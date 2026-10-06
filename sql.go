package sql

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"gitlab.com/gorib/sql/driver"
)

func newSql(driverName, dsn string) (closableDb, error) {
	var drv Driver
	switch driverName {
	case "sqlite3", "sqlite":
		driverName = "sqlite"
		drv = driver.NewSqlite()
	case "mysql":
		drv = driver.NewMysql()
	default:
		return nil, fmt.Errorf("unsupported driver: %s", driverName)
	}
	return &sqlDb{
		dsn:    dsn,
		driver: driverName,
		drv:    drv,
	}, nil
}

type sqlDb struct {
	dsn    string
	driver string
	drv    Driver

	once sync.Once
	db   *sql.DB
	err  error
}

func (s *sqlDb) Connect(_ context.Context) (Db, error) {
	if _, err := s.DbSql(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *sqlDb) Begin(ctx context.Context) (Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqlAdapter{tx}, nil
}

func (s *sqlDb) Driver() Driver {
	return s.drv
}

func (s *sqlDb) DbSql() (*sql.DB, error) {
	s.once.Do(func() {
		conn, err := sql.Open(s.driver, s.dsn)
		if err != nil {
			s.err = fmt.Errorf("%w: %v", ErrCannotConnectDb, err)
			return
		}
		s.db = conn
	})
	return s.db, s.err
}

func (s *sqlDb) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

type sqlAdapter struct {
	*sql.Tx
}

func (a *sqlAdapter) Query(ctx context.Context, q string, args ...any) (Rows, error) {
	rows, err := a.QueryContext(ctx, q, args...)
	return &sqlRows{Rows: rows}, err
}

func (a *sqlAdapter) QueryRow(ctx context.Context, q string, args ...any) Row {
	return a.QueryRowContext(ctx, q, args...)
}

func (a *sqlAdapter) Exec(ctx context.Context, q string, args ...any) (Result, error) {
	result, err := a.ExecContext(ctx, q, args...)
	return &sqlResult{res: result}, err
}

func (a *sqlAdapter) Commit(_ context.Context) error {
	return a.Tx.Commit()
}

func (a *sqlAdapter) Rollback(_ context.Context) error {
	return a.Tx.Rollback()
}

type sqlRows struct {
	*sql.Rows
}

func (r sqlRows) Close() {
	_ = r.Rows.Close()
}

type sqlResult struct {
	res sql.Result
}

func (r sqlResult) RowsAffected() int64 {
	count, _ := r.res.RowsAffected()
	return count
}
