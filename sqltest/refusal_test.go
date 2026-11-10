package sqltest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARefusedTableRefusesEveryWrite(t *testing.T) {
	p := migrated(t, "refused", seeded)

	require.NoError(t, p.Refuse(t.Context(), "vendors"))

	assert.ErrorContains(t, attempt(t, p, `insert into vendors (state_id, name) values (1, 'R2')`), "insert on vendors is refused")
	assert.ErrorContains(t, attempt(t, p, `update vendors set name = 'renamed'`), "update on vendors is refused")
	assert.ErrorContains(t, attempt(t, p, `delete from vendors`), "delete on vendors is refused")
	assert.Equal(t, []string{"R1"}, column(t, p, `select name from vendors`))
}

func TestAWriteThatReturnsRowsIsRefusedToo(t *testing.T) {
	p := migrated(t, "returning", seeded)
	require.NoError(t, p.Refuse(t.Context(), "states"))

	tx, err := p.Db().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	var id int
	err = tx.QueryRow(t.Context(), `insert into states (name) values ('extra') returning id`).Scan(&id)

	assert.ErrorContains(t, err, "refused")
}

func TestARefusalLeavesTheOtherTablesAlone(t *testing.T) {
	p := migrated(t, "elsewhere", seeded)

	require.NoError(t, p.Refuse(t.Context(), "vendors"))

	assert.NoError(t, attempt(t, p, `insert into states (name) values ('extra')`))
}

func TestARefusalLeavesTheWritesItDoesNotNameAlone(t *testing.T) {
	p := migrated(t, "named", seeded)

	require.NoError(t, p.Refuse(t.Context(), "vendors", Delete))

	assert.NoError(t, attempt(t, p, `update vendors set name = 'renamed'`))
	assert.ErrorContains(t, attempt(t, p, `delete from vendors`), "refused")
}

func TestAQuotaLetsThatManyRowsThrough(t *testing.T) {
	p := migrated(t, "quota", seeded)

	require.NoError(t, p.RefuseAfter(t.Context(), "states", 1))

	assert.NoError(t, attempt(t, p, `insert into states (name) values ('first')`))
	assert.ErrorContains(t, attempt(t, p, `insert into states (name) values ('second')`), "refused")
}

func TestADeleteWithinTheQuotaDeletes(t *testing.T) {
	p := migrated(t, "deleted", seeded)

	require.NoError(t, p.RefuseAfter(t.Context(), "vendors", 1, Delete))

	require.NoError(t, attempt(t, p, `delete from vendors`))
	assert.Empty(t, column(t, p, `select name from vendors`))
}

func TestARolledBackRowGivesItsPlaceBack(t *testing.T) {
	p := migrated(t, "rolledback", seeded)
	require.NoError(t, p.RefuseAfter(t.Context(), "states", 1))

	tx, err := p.Handle().BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `insert into states (name) values ('undone')`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	assert.NoError(t, attempt(t, p, `insert into states (name) values ('kept')`))
}

func TestRefusingAgainReplacesTheQuota(t *testing.T) {
	p := migrated(t, "again", seeded)
	require.NoError(t, p.Refuse(t.Context(), "states"))

	require.NoError(t, p.RefuseAfter(t.Context(), "states", 1))

	assert.NoError(t, attempt(t, p, `insert into states (name) values ('first')`))
}

func TestACommitRefusalLetsTheStatementsThroughAndFailsTheCommit(t *testing.T) {
	p := migrated(t, "commit", seeded)
	require.NoError(t, p.RefuseCommit(t.Context(), "states"))

	tx, err := p.Handle().BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `insert into states (name) values ('extra')`)
	require.NoError(t, err)

	assert.ErrorContains(t, tx.Commit(), "insert on states is refused")
	assert.Equal(t, []string{"new", "active", "closed"}, column(t, p, `select name from states order by id`))
}

func TestACommitRefusalLeavesATransactionThatDidNotWriteTheTableAlone(t *testing.T) {
	p := migrated(t, "elsewherecommit", seeded)
	require.NoError(t, p.RefuseCommit(t.Context(), "states"))

	tx, err := p.Handle().BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `select name from states`)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `insert into vendors (state_id, name) values (1, 'R2')`)
	require.NoError(t, err)

	assert.NoError(t, tx.Commit())
}

func TestHealLiftsTheRefusalsAndKeepsTheRows(t *testing.T) {
	p := migrated(t, "healed", seeded)
	apply(t, p, `insert into states (name) values ('extra')`)
	require.NoError(t, p.Refuse(t.Context(), "vendors"))
	p.FailWith(nil)

	require.NoError(t, p.Heal(t.Context()))

	assert.NoError(t, attempt(t, p, `insert into vendors (state_id, name) values (1, 'R2')`))
	tx, err := p.Db().Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
	assert.Equal(t, []string{"new", "active", "closed", "extra"}, column(t, p, `select name from states order by id`))
}

func TestResetLiftsTheRefusals(t *testing.T) {
	p := migrated(t, "lifted", seeded)
	require.NoError(t, p.Refuse(t.Context(), "vendors"))

	require.NoError(t, p.Reset(t.Context()))

	assert.NoError(t, attempt(t, p, `insert into vendors (state_id, name) values (1, 'R2')`))
}

func TestOnlyAWriteCanBeRefused(t *testing.T) {
	err := New().Refuse(t.Context(), "vendors", Write("truncate"))

	assert.ErrorContains(t, err, `"truncate" is not a write`)
}

func attempt(t *testing.T, p *Postgres, statement string) error {
	t.Helper()
	_, err := p.Handle().ExecContext(t.Context(), statement)
	return err
}
