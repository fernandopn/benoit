package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
)

func ConfigureDB(ctx context.Context, dbPath string) (*bun.DB, func() error, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, nil, nil
	}
	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		return nil, nil, err
	}
	return db, db.Close, nil
}

func OpenDB(ctx context.Context, dbPath string) (*bun.DB, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, errors.New("db path is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sqlDB, err := sql.Open(sqliteshim.ShimName, dbPath)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	db := bun.NewDB(sqlDB, sqlitedialect.New())
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
