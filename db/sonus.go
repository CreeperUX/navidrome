package db

import (
	"context"
	"database/sql"

	"github.com/navidrome/navidrome/log"
)

// SonusSchema is this fork's own tables (core/twofactor: two-step verification and app passwords). They are created
// after upstream's migrations, outside them, so those stay as upstream has them and upstream Navidrome, switched back
// to, leaves these tables alone. Every statement is idempotent.
const SonusSchema = `
CREATE TABLE IF NOT EXISTS sonus_two_factor (
	user_id TEXT PRIMARY KEY REFERENCES user(id) ON DELETE CASCADE,
	secret TEXT NOT NULL DEFAULT '',
	pending_secret TEXT NOT NULL DEFAULT '',
	confirmed_at DATETIME,
	last_step INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sonus_recovery_code (
	user_id TEXT NOT NULL REFERENCES user(id) ON DELETE CASCADE,
	code_hash TEXT NOT NULL,
	used_at DATETIME,
	PRIMARY KEY (user_id, code_hash)
);
CREATE TABLE IF NOT EXISTS sonus_app_password (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES user(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	secret TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	last_used_at DATETIME,
	last_used_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS sonus_app_password_user ON sonus_app_password(user_id);
`

// SonusUpgrade brings tables made by an earlier build up to date: each step runs once, when its column is missing.
// App passwords gained secret_hash (sha256 of the password), to find the user an API key belongs to; filled in by
// core/twofactor, which can open the sealed passwords.
func SonusUpgrade(ctx context.Context, db *sql.DB) error {
	has, err := hasColumn(ctx, db, "sonus_app_password", "secret_hash")
	if err != nil {
		return err
	}
	if !has {
		if _, err := db.ExecContext(ctx, `ALTER TABLE sonus_app_password ADD COLUMN secret_hash TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS sonus_app_password_hash ON sonus_app_password(secret_hash)`)
	return err
}

func hasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func initSonusSchema(ctx context.Context, db *sql.DB) {
	if _, err := db.ExecContext(ctx, SonusSchema); err != nil {
		log.Fatal(ctx, "Failed to create Sonus's tables", err)
	}
	if err := SonusUpgrade(ctx, db); err != nil {
		log.Fatal(ctx, "Failed to upgrade Sonus's tables", err)
	}
}
