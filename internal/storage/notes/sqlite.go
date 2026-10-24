package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

const (
	driverName  = "sqlite"
	MaxRunes    = 10000
	MaxPageSize = 100
)

var (
	ErrNotFound = errors.New("note not found")
	ErrInvalid  = errors.New("invalid note")
)

type Note struct {
	ID        int64     `json:"id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("notes sqlite path is required")
	}
	path = strings.TrimSpace(path)
	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("open notes sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure notes sqlite busy timeout: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure notes sqlite journal: %w", err)
	}
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(ctx context.Context, content string) (Note, error) {
	if err := validateContent(content); err != nil {
		return Note{}, err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO notes(content, created_at, updated_at) VALUES (?, ?, ?)`, content, formatTime(now), formatTime(now))
	if err != nil {
		return Note{}, fmt.Errorf("create note: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Note{}, fmt.Errorf("read created note ID: %w", err)
	}
	return Note{ID: id, Content: content, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) List(ctx context.Context, offset, limit int) ([]Note, error) {
	if offset < 0 || limit < 1 || limit > MaxPageSize+1 {
		return nil, fmt.Errorf("invalid note page: %w", ErrInvalid)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, content, created_at, updated_at FROM notes ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()
	notes := make([]Note, 0, limit)
	for rows.Next() {
		var note Note
		var createdAt, updatedAt string
		if err := rows.Scan(&note.ID, &note.Content, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		if note.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, fmt.Errorf("parse note creation time: %w", err)
		}
		if note.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
			return nil, fmt.Errorf("parse note update time: %w", err)
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notes: %w", err)
	}
	return notes, nil
}

func (s *Store) Update(ctx context.Context, id int64, content string) (Note, error) {
	if id <= 0 {
		return Note{}, fmt.Errorf("note ID must be positive: %w", ErrInvalid)
	}
	if err := validateContent(content); err != nil {
		return Note{}, err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE notes SET content = ?, updated_at = ? WHERE id = ?`, content, formatTime(now), id)
	if err != nil {
		return Note{}, fmt.Errorf("update note: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Note{}, fmt.Errorf("check updated note: %w", err)
	}
	if changed == 0 {
		return Note{}, ErrNotFound
	}
	var note Note
	var createdAt, updatedAt string
	if err := s.db.QueryRowContext(ctx, `SELECT id, content, created_at, updated_at FROM notes WHERE id = ?`, id).Scan(&note.ID, &note.Content, &createdAt, &updatedAt); err != nil {
		return Note{}, fmt.Errorf("read updated note: %w", err)
	}
	note.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Note{}, fmt.Errorf("parse note creation time: %w", err)
	}
	note.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Note{}, fmt.Errorf("parse note update time: %w", err)
	}
	return note, nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("note ID must be positive: %w", ErrInvalid)
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted note: %w", err)
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read notes schema version: %w", err)
	}
	if version > 1 {
		return fmt.Errorf("unsupported notes schema version %d", version)
	}
	if version == 1 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin notes schema migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE notes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		content TEXT NOT NULL CHECK(length(content) BETWEEN 1 AND 10000),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create notes table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX idx_notes_updated ON notes(updated_at DESC, id DESC)`); err != nil {
		return fmt.Errorf("create notes index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 1`); err != nil {
		return fmt.Errorf("set notes schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notes schema migration: %w", err)
	}
	return nil
}

func validateContent(content string) error {
	if !utf8.ValidString(content) || strings.TrimSpace(content) == "" || utf8.RuneCountInString(content) > MaxRunes {
		return ErrInvalid
	}
	return nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
