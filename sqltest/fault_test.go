package sqltest

import (
	"context"
	dbSql "database/sql"
	"errors"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/gorib/sql"
)

type stubDb struct{ tx *stubTx }

func (d *stubDb) Connect(context.Context) (sql.Db, error) { return d, nil }
func (d *stubDb) Begin(context.Context) (sql.Tx, error)   { return d.tx, nil }
func (d *stubDb) Driver() sql.Driver                      { return nil }
func (d *stubDb) DbSql() (*dbSql.DB, error)               { return nil, nil }

type stubTx struct{ committed, rolledBack bool }

func (t *stubTx) Query(context.Context, string, ...any) (sql.Rows, error)  { return nil, nil }
func (t *stubTx) QueryRow(context.Context, string, ...any) sql.Row         { return failedRow{} }
func (t *stubTx) Exec(context.Context, string, ...any) (sql.Result, error) { return nil, nil }
func (t *stubTx) Commit(context.Context) error                             { t.committed = true; return nil }
func (t *stubTx) Rollback(context.Context) error                           { t.rolledBack = true; return nil }

func provisioned() (*Postgres, *stubTx) {
	tx := &stubTx{}
	p := New()
	p.db = &stubDb{tx: tx}
	return p, tx
}

func TestFailWithNilRefusesTheConnection(t *testing.T) {
	p, _ := provisioned()
	db := p.Db()

	p.FailWith(nil)

	_, err := db.Begin(t.Context())
	assert.ErrorIs(t, err, syscall.ECONNREFUSED)
}

func TestFailWithReachesADbHandedOutBefore(t *testing.T) {
	p, _ := provisioned()
	db := p.Db()
	gone := errors.New("gone")

	p.FailWith(gone)

	_, err := db.Connect(t.Context())
	assert.ErrorIs(t, err, gone)
	_, err = db.DbSql()
	assert.ErrorIs(t, err, gone)
}

func TestAnOpenTransactionFailsAndGivesItsConnectionBack(t *testing.T) {
	p, inner := provisioned()
	tx, err := p.Db().Begin(t.Context())
	require.NoError(t, err)
	gone := errors.New("gone")

	p.FailWith(gone)

	_, err = tx.Exec(t.Context(), "update t set a = 1")
	assert.ErrorIs(t, err, gone)
	assert.ErrorIs(t, tx.QueryRow(t.Context(), "select 1").Scan(), gone)
	assert.ErrorIs(t, tx.Commit(t.Context()), gone)
	assert.False(t, inner.committed)
	assert.True(t, inner.rolledBack)
}

func TestResetHeals(t *testing.T) {
	p, inner := provisioned()
	p.FailWith(nil)

	require.NoError(t, p.Reset(t.Context()))

	tx, err := p.Db().Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	assert.True(t, inner.committed)
}

func TestConnectKeepsTheFault(t *testing.T) {
	p, _ := provisioned()
	connected, err := p.Db().Connect(t.Context())
	require.NoError(t, err)

	p.FailWith(nil)

	_, err = connected.Begin(t.Context())
	assert.ErrorIs(t, err, syscall.ECONNREFUSED)
}
