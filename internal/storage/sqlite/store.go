package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nyan-go/internal/ledger"

	_ "modernc.org/sqlite"
)

const (
	driverName    = "sqlite"
	maxSearchRows = 20

	mutationCreate  MutationKind = "create"
	mutationUpdate  MutationKind = "update"
	mutationDelete  MutationKind = "delete"
	mutationRestore MutationKind = "restore"
)

// Store is the SQLite-backed ledger repository. It owns schema setup and keeps
// all persistence concerns below the application/Discord boundary.
type Store struct {
	db *sql.DB
}

// Open opens a SQLite database and applies all known migrations.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite path is required")
	}
	dsn := path
	if path == ":memory:" {
		dsn = "file:nyan-go-memory?mode=memory&cache=shared"
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	// A single connection avoids separate in-memory databases and serializes
	// writes without adding a connection-pool policy to the repository.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the underlying connection for narrowly scoped read-only checks.
// Callers must not mutate schema or bypass Store mutation methods.
func (s *Store) DB() *sql.DB { return s.db }

type MutationKind string

const (
	MutationCreate  MutationKind = mutationCreate
	MutationUpdate  MutationKind = mutationUpdate
	MutationDelete  MutationKind = mutationDelete
	MutationRestore MutationKind = mutationRestore
)

// Mutation is one ordered operation in an atomic source-message batch.
type Mutation struct {
	Kind  MutationKind
	ID    int64
	Input ledger.TransactionInput
}

// ApplyBatch validates and executes every mutation atomically. A source
// message is an idempotency key: a replay returns the original transaction
// set without applying any operation again.
func (s *Store) ApplyBatch(ctx context.Context, request ledger.RequestContext, mutations []Mutation) ([]ledger.Transaction, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if len(mutations) == 0 {
		return nil, errors.New("mutation batch cannot be empty")
	}
	for _, mutation := range mutations {
		if err := validateMutation(mutation); err != nil {
			return nil, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin mutation transaction: %w", err)
	}
	defer tx.Rollback()

	var resultJSON string
	err = tx.QueryRowContext(ctx, `SELECT result_json FROM mutation_batches WHERE source_message_id = ?`, request.SourceMessageID).Scan(&resultJSON)
	if err == nil {
		var ids []int64
		if err := json.Unmarshal([]byte(resultJSON), &ids); err != nil {
			return nil, fmt.Errorf("decode idempotency result: %w", err)
		}
		result, err := getTransactionsByIDs(ctx, tx, ids, true)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit idempotency read: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check mutation idempotency: %w", err)
	}

	ids := make([]int64, 0, len(mutations))
	for _, mutation := range mutations {
		id, err := applyMutation(ctx, tx, request, mutation)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, fmt.Errorf("encode mutation result: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mutation_batches(source_message_id, user_id, guild_id, channel_id, created_at, result_json) VALUES (?, ?, ?, ?, ?, ?)`, request.SourceMessageID, request.UserID, request.GuildID, request.ChannelID, formatTime(request.ReceivedAt), string(encoded)); err != nil {
		return nil, fmt.Errorf("record mutation idempotency: %w", err)
	}
	result, err := getTransactionsByIDs(ctx, tx, ids, true)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit mutation transaction: %w", err)
	}
	return result, nil
}

// CreateBatch implements app.LedgerRepository for the common create-only path.
func (s *Store) CreateBatch(ctx context.Context, request ledger.RequestContext, inputs []ledger.TransactionInput) ([]ledger.Transaction, error) {
	mutations := make([]Mutation, len(inputs))
	for i, input := range inputs {
		mutations[i] = Mutation{Kind: MutationCreate, Input: input}
	}
	return s.ApplyBatch(ctx, request, mutations)
}

func validateMutation(mutation Mutation) error {
	switch mutation.Kind {
	case MutationCreate:
		return mutation.Input.Validate()
	case MutationUpdate:
		if mutation.ID <= 0 {
			return errors.New("update transaction ID must be positive")
		}
		return mutation.Input.Validate()
	case MutationDelete, MutationRestore:
		if mutation.ID <= 0 {
			return fmt.Errorf("%s transaction ID must be positive", mutation.Kind)
		}
		return nil
	default:
		return fmt.Errorf("unsupported mutation kind %q", mutation.Kind)
	}
}

func applyMutation(ctx context.Context, tx *sql.Tx, request ledger.RequestContext, mutation Mutation) (int64, error) {
	now := request.ReceivedAt.UTC()
	switch mutation.Kind {
	case MutationCreate:
		result, err := tx.ExecContext(ctx, `INSERT INTO transactions(type, amount_vnd, category, note, occurred_at, creator_user_id, guild_id, channel_id, source_message_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, mutation.Input.Type, mutation.Input.AmountVND, mutation.Input.Category, mutation.Input.Note, formatTime(mutation.Input.OccurredAt), request.UserID, request.GuildID, request.ChannelID, request.SourceMessageID, formatTime(now), formatTime(now))
		if err != nil {
			return 0, fmt.Errorf("create transaction: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("read created transaction ID: %w", err)
		}
		if err := insertAudit(ctx, tx, request, id, "create", nil, mutation.Input); err != nil {
			return 0, err
		}
		return id, nil
	case MutationUpdate:
		before, err := getTransaction(ctx, tx, mutation.ID, true)
		if err != nil {
			return 0, err
		}
		if before.DeletedAt != nil {
			return 0, errors.New("cannot update a deleted transaction")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE transactions SET type = ?, amount_vnd = ?, category = ?, note = ?, occurred_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, mutation.Input.Type, mutation.Input.AmountVND, mutation.Input.Category, mutation.Input.Note, formatTime(mutation.Input.OccurredAt), formatTime(now), mutation.ID); err != nil {
			return 0, fmt.Errorf("update transaction: %w", err)
		}
		if err := insertAudit(ctx, tx, request, mutation.ID, "update", &before, mutation.Input); err != nil {
			return 0, err
		}
		return mutation.ID, nil
	case MutationDelete:
		before, err := getTransaction(ctx, tx, mutation.ID, true)
		if err != nil {
			return 0, err
		}
		if before.DeletedAt != nil {
			return 0, errors.New("transaction is already deleted")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE transactions SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, formatTime(now), formatTime(now), mutation.ID); err != nil {
			return 0, fmt.Errorf("delete transaction: %w", err)
		}
		if err := insertAudit(ctx, tx, request, mutation.ID, "delete", &before, nil); err != nil {
			return 0, err
		}
		return mutation.ID, nil
	case MutationRestore:
		before, err := getTransaction(ctx, tx, mutation.ID, true)
		if err != nil {
			return 0, err
		}
		if before.DeletedAt == nil {
			return 0, errors.New("transaction is not deleted")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE transactions SET deleted_at = NULL, updated_at = ? WHERE id = ?`, formatTime(now), mutation.ID); err != nil {
			return 0, fmt.Errorf("restore transaction: %w", err)
		}
		if err := insertAudit(ctx, tx, request, mutation.ID, "restore", &before, nil); err != nil {
			return 0, err
		}
		return mutation.ID, nil
	default:
		return 0, fmt.Errorf("unsupported mutation kind %q", mutation.Kind)
	}
}
