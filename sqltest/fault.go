package sqltest

import (
	"context"
	dbSql "database/sql"
	"net"
	"os"
	"sync/atomic"
	"syscall"

	"gitlab.com/gorib/sql"
)

// What a server that went away looks like from the client.
var errRefused error = &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}

type fault struct {
	err atomic.Pointer[error]
}

func (f *fault) fail(err error) {
	if err == nil {
		err = errRefused
	}
	f.err.Store(&err)
}

func (f *fault) heal() { f.err.Store(nil) }

func (f *fault) current() error {
	if err := f.err.Load(); err != nil {
		return *err
	}
	return nil
}

type faultyDb struct {
	inner sql.Db
	fault *fault
}

func (d *faultyDb) Connect(ctx context.Context) (sql.Db, error) {
	if err := d.fault.current(); err != nil {
		return nil, err
	}
	if _, err := d.inner.Connect(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *faultyDb) Begin(ctx context.Context) (sql.Tx, error) {
	if err := d.fault.current(); err != nil {
		return nil, err
	}
	tx, err := d.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &faultyTx{inner: tx, fault: d.fault}, nil
}

func (d *faultyDb) Driver() sql.Driver { return d.inner.Driver() }

func (d *faultyDb) DbSql() (*dbSql.DB, error) {
	if err := d.fault.current(); err != nil {
		return nil, err
	}
	return d.inner.DbSql()
}

type faultyTx struct {
	inner sql.Tx
	fault *fault
}

func (t *faultyTx) Query(ctx context.Context, query string, args ...any) (sql.Rows, error) {
	if err := t.fault.current(); err != nil {
		return nil, err
	}
	return t.inner.Query(ctx, query, args...)
}

func (t *faultyTx) QueryRow(ctx context.Context, query string, args ...any) sql.Row {
	if err := t.fault.current(); err != nil {
		return failedRow{err}
	}
	return t.inner.QueryRow(ctx, query, args...)
}

func (t *faultyTx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := t.fault.current(); err != nil {
		return nil, err
	}
	return t.inner.Exec(ctx, query, args...)
}

// The real transaction is rolled back either way: a connection left inside one
// would stay out of the pool for every scenario after this.
func (t *faultyTx) Commit(ctx context.Context) error {
	if err := t.fault.current(); err != nil {
		_ = t.inner.Rollback(ctx)
		return err
	}
	return t.inner.Commit(ctx)
}

func (t *faultyTx) Rollback(ctx context.Context) error {
	err := t.inner.Rollback(ctx)
	if failed := t.fault.current(); failed != nil {
		return failed
	}
	return err
}

type failedRow struct{ err error }

func (r failedRow) Scan(...any) error { return r.err }
