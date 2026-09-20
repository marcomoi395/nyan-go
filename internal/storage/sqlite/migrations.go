package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 1

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}
	var version int
	err := db.QueryRowContext(ctx, `SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("read schema version: %w", err)
	}
	if err == sql.ErrNoRows {
		version = 0
	}
	if version > schemaVersion {
		return fmt.Errorf("unsupported sqlite schema version %d", version)
	}
	if version < 1 {
		if err := migrateV1(ctx, db); err != nil {
			return err
		}
	}
	return nil
}

func migrateV1(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL CHECK (type IN ('income', 'expense')),
			amount_vnd INTEGER NOT NULL CHECK (amount_vnd > 0),
			category TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			occurred_at TEXT NOT NULL,
			creator_user_id TEXT NOT NULL,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			source_message_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			deleted_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_scope_time ON transactions(creator_user_id, guild_id, channel_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_source_message ON transactions(source_message_id)`,
		`CREATE TABLE IF NOT EXISTS mutation_batches (
			source_message_id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			result_json TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			transaction_id INTEGER NOT NULL REFERENCES transactions(id),
			user_id TEXT NOT NULL,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			source_message_id TEXT NOT NULL,
			action TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			before_json TEXT,
			after_json TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_transaction ON audit_events(transaction_id, id)`,
		`CREATE TABLE IF NOT EXISTS reminder_state (
			user_id TEXT PRIMARY KEY,
			last_delivery_date TEXT,
			last_delivery_at TEXT
		)`,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version(version) VALUES (1)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply schema migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}
