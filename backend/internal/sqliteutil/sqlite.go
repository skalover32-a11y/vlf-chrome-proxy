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
	_, err := db.ExecContext(ctx, initSQL)
	return err
}
