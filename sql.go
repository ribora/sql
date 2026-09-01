package sql

import (
	"context"
	"database/sql"
	"fmt"

	"gitlab.com/gorib/sql/driver"
)

func newSql(driverName, dsn string) (Db, error) {
	var drv Driver
	switch driverName {
	case "sqlite3", "sqlite":
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
	db     *sql.DB
}

func (s *sqlDb) Connect(_ context.Context) (Db, error) {
	if s.db == nil || s.db.Ping() != nil {
		conn, err := sql.Open(s.driver, s.dsn)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCannotConnectDb, err)
		}
		s.db = conn
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
	return s.db, nil
}

func (s *sqlDb) poolConns() (idle, used int64, ok bool) {
	if s.db == nil {
		return 0, 0, false
	}
	stats := s.db.Stats()
	return int64(stats.Idle), int64(stats.InUse), true
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
