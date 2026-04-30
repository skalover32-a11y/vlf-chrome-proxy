package sqliteutil

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed 001_init.sql
var initSQL string

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, stmt := range pragmas {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply pragma: %w", err)
		}
	}

	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, initSQL); err != nil {
		return err
	}
	accessLinkRequired, err := columnNotNull(ctx, db, "browser_sessions", "access_link_id")
	if err != nil {
		return err
	}
	if accessLinkRequired {
		if err := rebuildBrowserSessions(ctx, db); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, initSQL); err != nil {
			return err
		}
	}

	columns := map[string]string{
		"source_type":              "TEXT NOT NULL DEFAULT 'local_access_link'",
		"source_ref":               "TEXT",
		"external_subscription_id": "TEXT",
	}
	for name, definition := range columns {
		exists, err := columnExists(ctx, db, "browser_sessions", name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE browser_sessions ADD COLUMN %s %s", name, definition)); err != nil {
			return err
		}
	}
	return nil
}

func rebuildBrowserSessions(ctx context.Context, db *sql.DB) error {
	statements := []string{
		"PRAGMA foreign_keys = OFF",
		`CREATE TABLE IF NOT EXISTS browser_sessions_new (
			id TEXT PRIMARY KEY,
			access_link_id TEXT,
			source_type TEXT NOT NULL DEFAULT 'local_access_link',
			source_ref TEXT,
			external_subscription_id TEXT,
			session_token_hash TEXT NOT NULL UNIQUE,
			selected_node_id TEXT,
			default_node_id TEXT,
			available_node_ids TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'active',
			expires_at TEXT NOT NULL,
			last_seen_at TEXT,
			revoked_at TEXT,
			client_ip TEXT,
			user_agent TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (access_link_id) REFERENCES access_links(id) ON DELETE CASCADE
		)`,
		`INSERT INTO browser_sessions_new (
			id, access_link_id, source_type, source_ref, external_subscription_id,
			session_token_hash, selected_node_id, default_node_id, available_node_ids,
			status, expires_at, last_seen_at, revoked_at, client_ip, user_agent, created_at, updated_at
		)
		SELECT
			id, access_link_id, 'local_access_link', access_link_id, NULL,
			session_token_hash, selected_node_id, default_node_id, available_node_ids,
			status, expires_at, last_seen_at, revoked_at, client_ip, user_agent, created_at, updated_at
		FROM browser_sessions`,
		"DROP TABLE browser_sessions",
		"ALTER TABLE browser_sessions_new RENAME TO browser_sessions",
		"PRAGMA foreign_keys = ON",
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func columnNotNull(ctx context.Context, db *sql.DB, table string, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return notNull == 1, nil
		}
	}
	return false, rows.Err()
}

func columnExists(ctx context.Context, db *sql.DB, table string, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
