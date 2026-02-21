package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	_ "modernc.org/sqlite"
)

func OpenSQLiteDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, errors.New("db path is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}

	return db, nil
}
