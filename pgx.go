package sql

import (
	"context"
	dbSql "database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"gitlab.com/gorib/sql/driver"
)

func newPgx(dsn string) Db {
	return &pgxDb{
		dsn:    dsn,
		driver: driver.NewPostgres(),
	}
}

type pgxDb struct {
	dsn    string
	db     *pgxpool.Pool
	driver Driver
}

func (s *pgxDb) Connect(ctx context.Context) (Db, error) {
	if s.db == nil {
		conn, err := pgxpool.New(ctx, s.dsn)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCannotConnectDb, err)
		}
		s.db = conn
	}
	return s, nil
}

func (s *pgxDb) Begin(ctx context.Context) (Tx, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgxAdapter{Tx: tx}, nil
}

func (s *pgxDb) Driver() Driver {
	return s.driver
}

func (s *pgxDb) DbSql() (*dbSql.DB, error) {
	stdlib.GetDefaultDriver()
	return dbSql.Open("pgx", s.dsn)
}

type pgxAdapter struct {
	pgx.Tx
}

func (a *pgxAdapter) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	return a.Tx.Query(ctx, query, args...)
}

func (a *pgxAdapter) QueryRow(ctx context.Context, query string, args ...any) Row {
	return a.Tx.QueryRow(ctx, query, args...)
}

func (a *pgxAdapter) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return a.Tx.Exec(ctx, query, args...)
}
