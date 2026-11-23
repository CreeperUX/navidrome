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

func initSonusSchema(ctx context.Context, db *sql.DB) {
	if _, err := db.ExecContext(ctx, SonusSchema); err != nil {
		log.Fatal(ctx, "Failed to create Sonus's tables", err)
	}
}
