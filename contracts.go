package sql

import (
	"context"
	dbSql "database/sql"
)

type Row interface {
	Scan(dest ...any) error
}

type Rows interface {
	Close()
	Err() error
	Next() bool
	Scan(dest ...any) error
}

type Result interface {
	RowsAffected() int64
}

type Tx interface {
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
	Exec(ctx context.Context, query string, args ...any) (Result, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type Db interface {
	Connect(ctx context.Context) (Db, error)
	Begin(ctx context.Context) (Tx, error)
	Driver() Driver
	DbSql() (*dbSql.DB, error)
}
