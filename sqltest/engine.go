package sqltest

import (
	"context"
	dbSql "database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"slices"

	"gitlab.com/gorib/sql"
)

type engine struct {
	dialect dialect
	migrate MigrateFunc
	tables  []string

	db     sql.Db
	handle *dbSql.DB
	fault  fault
}

type dialect interface {
	where() string // in errors: never a credential
	discover(ctx context.Context) ([]string, error)
	remember(ctx context.Context) error
	restore(ctx context.Context) error
	lift(ctx context.Context) error
}

func (e *engine) Db() sql.Db {
	if e.db == nil {
		return nil
	}
	return &faultyDb{inner: e.db, fault: &e.fault}
}

// Every Db handed out fails with err until Heal or Reset; nil reads as a refused connection.
func (e *engine) FailWith(err error) { e.fault.fail(err) }

func (e *engine) Heal(ctx context.Context) error {
	e.fault.heal()
	return e.dialect.lift(ctx)
}

func (e *engine) Handle() *dbSql.DB { return e.handle }

func (e *engine) open(ctx context.Context, driverName, dsn string) error {
	db, err := sql.NewDb(driverName, dsn)
	if err != nil {
		return fmt.Errorf("cannot build a handle for %s: %w", e.dialect.where(), err)
	}
	if _, err = db.Connect(ctx); err != nil {
		return e.unreachable(err)
	}
	handle, err := db.DbSql()
	if err != nil {
		return e.unreachable(err)
	}
	if err = handle.PingContext(ctx); err != nil {
		return e.unreachable(err)
	}
	e.db, e.handle = db, handle
	return nil
}

func (e *engine) unreachable(err error) error {
	return fmt.Errorf("no test database at %s: %w", e.dialect.where(), err)
}

func (e *engine) close() {
	if closer, ok := e.db.(io.Closer); ok {
		_ = closer.Close()
	}
	e.handle, e.db = nil, nil
}

func (e *engine) Migrate(ctx context.Context) error {
	if e.migrate != nil {
		if err := e.migrate(ctx, e.handle); err != nil {
			return fmt.Errorf("migrations failed on %s: %w", e.dialect.where(), err)
		}
	}
	if len(e.tables) == 0 {
		tables, err := e.dialect.discover(ctx)
		if err != nil {
			return fmt.Errorf("cannot list the tables of %s: %w", e.dialect.where(), err)
		}
		e.tables = slices.DeleteFunc(tables, func(name string) bool { return slices.Contains(bookkeeping, name) })
	}
	if err := e.dialect.remember(ctx); err != nil {
		return fmt.Errorf("cannot keep what the migrations left in %s: %w", e.dialect.where(), err)
	}
	// Once here, so a restore that cannot work fails the one Migrate, not every Reset.
	return e.Reset(ctx)
}

// Emptying the tables rather than a held-open transaction: the scenario talks to
// the service over a real connection, and context values do not cross that.
func (e *engine) Reset(ctx context.Context) error {
	e.fault.heal()
	if len(e.tables) == 0 {
		return nil
	}
	// Down closes the handle but leaves the table list, so without this a Reset
	// after teardown dereferences nil instead of saying what happened.
	if e.handle == nil {
		return errors.New("sqltest: the database is not open — Provision has not run, or Down already closed it")
	}
	if err := e.dialect.lift(ctx); err != nil {
		return err
	}
	if err := e.dialect.restore(ctx); err != nil {
		return fmt.Errorf("cannot reset %s: %w", e.dialect.where(), err)
	}
	return nil
}

func (e *engine) throwaway(ctx context.Context, do func(conn *dbSql.Conn) error) error {
	conn, err := e.handle.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }()
	return do(conn)
}
