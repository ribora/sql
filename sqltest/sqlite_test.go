package sqltest

import (
	"context"
	dbSql "database/sql"
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const seededSqlite = `
create table states (id integer primary key autoincrement, name text not null);
insert into states (name) values ('new'), ('active'), ('closed');

create table categories (
	id integer primary key,
	parent_id integer references categories(id),
	name text not null,
	name_length int generated always as (length(name)) stored
);
insert into categories (parent_id, name) values (null, 'Master');
insert into categories (parent_id, name) values (1, 'Child');

create table vendors (id integer primary key, state_id int not null references states(id) on delete restrict, name text not null);
insert into vendors (state_id, name) values (1, 'R1');

create table audit (id integer primary key, what text not null);
create trigger vendors_audited after insert on vendors begin
	insert into audit (what) values (new.name);
end;

create table orders (id integer primary key, vendor_id int not null references vendors(id));
`

func TestSqliteResetPutsBackWhatTheMigrationsLeft(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	execSqlite(t, s, `insert into states (name) values ('extra')`)
	execSqlite(t, s, `update vendors set name = 'renamed'`)
	execSqlite(t, s, `delete from categories where parent_id is not null`)
	execSqlite(t, s, `insert into orders (vendor_id) values (1)`)

	require.NoError(t, s.Reset(t.Context()))

	assert.Equal(t, []string{"new", "active", "closed"}, columnSqlite(t, s, `select name from states order by id`))
	assert.Equal(t, []string{"Master", "Child"}, columnSqlite(t, s, `select name from categories order by id`))
	assert.Equal(t, []string{"R1"}, columnSqlite(t, s, `select name from vendors order by id`))
	assert.Empty(t, columnSqlite(t, s, `select id from orders`))
}

func TestSqliteIdsContinueAfterTheRowsPutBack(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	execSqlite(t, s, `insert into states (name) values ('extra')`)
	execSqlite(t, s, `insert into orders (vendor_id) values (1)`)

	require.NoError(t, s.Reset(t.Context()))

	assert.Equal(t, []string{"4"}, columnSqlite(t, s, `insert into states (name) values ('next') returning id`))
	assert.Equal(t, []string{"2"}, columnSqlite(t, s, `insert into vendors (state_id, name) values (1, 'R2') returning id`))
	assert.Equal(t, []string{"1"}, columnSqlite(t, s, `insert into orders (vendor_id) values (1) returning id`))
}

func TestSqliteResetFiresNoTriggerAndKeepsThem(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)

	require.NoError(t, s.Reset(t.Context()))
	assert.Empty(t, columnSqlite(t, s, `select what from audit`))

	execSqlite(t, s, `insert into vendors (state_id, name) values (1, 'R2')`)
	assert.Equal(t, []string{"R2"}, columnSqlite(t, s, `select what from audit`))
}

func TestSqliteForeignKeysAreCheckedAfterAReset(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)

	require.NoError(t, s.Reset(t.Context()))

	assert.ErrorContains(t, attemptSqlite(t, s, `insert into orders (vendor_id) values (99)`), "FOREIGN KEY")
}

func TestSqliteGeneratedColumnsAreLeftToSqlite(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	execSqlite(t, s, `update categories set name = 'Renamed' where id = 1`)

	require.NoError(t, s.Reset(t.Context()))

	assert.Equal(t, []string{"6", "5"}, columnSqlite(t, s, `select name_length from categories order by id`))
}

func TestSqliteBookkeepingSurvivesAReset(t *testing.T) {
	s := migratedSqlite(t, `
create table migrations (id integer primary key autoincrement, version_id integer not null);
insert into migrations (version_id) values (1);
create table orders (id integer primary key);`)
	execSqlite(t, s, `insert into orders default values`)

	require.NoError(t, s.Reset(t.Context()))

	assert.Equal(t, []string{"1"}, columnSqlite(t, s, `select version_id from migrations`))
	assert.Empty(t, columnSqlite(t, s, `select id from orders`))
}

func TestSqliteMigrationsThatCreateNoTablesLeaveNothingToReset(t *testing.T) {
	s := migratedSqlite(t, `select 1`)

	require.NoError(t, s.Reset(t.Context()))
}

func TestSqliteMigrationsThatFailAreReported(t *testing.T) {
	s := NewSqlite(func(context.Context, *dbSql.DB) error { return errors.New("broken") })
	require.NoError(t, s.Provision(t.Context()))
	t.Cleanup(func() { _ = s.Down(context.Background()) })

	assert.ErrorContains(t, s.Migrate(t.Context()), "broken")
}

func TestSqliteDownRemovesTheDatabase(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	file := s.file()

	require.NoError(t, s.Down(t.Context()))

	_, err := os.Stat(file)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestSqliteFailWithFailsTheDbUntilReset(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	s.FailWith(nil)

	_, err := s.Db().Begin(t.Context())
	assert.ErrorIs(t, err, errRefused)

	require.NoError(t, s.Reset(t.Context()))
	tx, err := s.Db().Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
}

func TestSqliteARefusedTableRefusesEveryWrite(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)

	require.NoError(t, s.Refuse(t.Context(), "vendors"))

	assert.ErrorContains(t, attemptSqlite(t, s, `insert into vendors (state_id, name) values (1, 'R2')`), "insert on vendors is refused")
	assert.ErrorContains(t, attemptSqlite(t, s, `update vendors set name = 'renamed'`), "update on vendors is refused")
	assert.ErrorContains(t, attemptSqlite(t, s, `delete from vendors`), "delete on vendors is refused")
	assert.Equal(t, []string{"R1"}, columnSqlite(t, s, `select name from vendors`))
}

func TestSqliteAWriteThatReturnsRowsIsRefusedToo(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	require.NoError(t, s.Refuse(t.Context(), "states"))

	tx, err := s.Db().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	var id int
	err = tx.QueryRow(t.Context(), `insert into states (name) values ('extra') returning id`).Scan(&id)

	assert.ErrorContains(t, err, "refused")
}

func TestSqliteARefusalLeavesTheOtherTablesAndWritesAlone(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)

	require.NoError(t, s.Refuse(t.Context(), "vendors", Delete))

	assert.NoError(t, attemptSqlite(t, s, `insert into states (name) values ('extra')`))
	assert.NoError(t, attemptSqlite(t, s, `update vendors set name = 'renamed'`))
	assert.ErrorContains(t, attemptSqlite(t, s, `delete from vendors`), "refused")
}

func TestSqliteAQuotaIsSharedByTheWritesItNames(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)

	require.NoError(t, s.RefuseAfter(t.Context(), "states", 1))

	assert.NoError(t, attemptSqlite(t, s, `update states set name = 'renamed' where id = 1`))
	assert.ErrorContains(t, attemptSqlite(t, s, `insert into states (name) values ('second')`), "refused")
}

func TestSqliteARolledBackRowGivesItsPlaceBack(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	require.NoError(t, s.RefuseAfter(t.Context(), "states", 1))

	tx, err := s.Handle().BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `insert into states (name) values ('undone')`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	assert.NoError(t, attemptSqlite(t, s, `insert into states (name) values ('kept')`))
}

func TestSqliteRefusingAgainReplacesTheQuota(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	require.NoError(t, s.Refuse(t.Context(), "states"))

	require.NoError(t, s.RefuseAfter(t.Context(), "states", 1))

	assert.NoError(t, attemptSqlite(t, s, `insert into states (name) values ('first')`))
}

func TestSqliteHealLiftsTheRefusalsAndKeepsTheRows(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	execSqlite(t, s, `insert into states (name) values ('extra')`)
	require.NoError(t, s.Refuse(t.Context(), "vendors"))
	s.FailWith(nil)

	require.NoError(t, s.Heal(t.Context()))

	assert.NoError(t, attemptSqlite(t, s, `insert into vendors (state_id, name) values (1, 'R2')`))
	tx, err := s.Db().Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
	assert.Equal(t, []string{"new", "active", "closed", "extra"}, columnSqlite(t, s, `select name from states order by id`))
}

func TestSqliteResetLiftsTheRefusals(t *testing.T) {
	s := migratedSqlite(t, seededSqlite)
	require.NoError(t, s.Refuse(t.Context(), "vendors"))

	require.NoError(t, s.Reset(t.Context()))

	assert.NoError(t, attemptSqlite(t, s, `insert into vendors (state_id, name) values (1, 'R2')`))
}

func migratedSqlite(t *testing.T, schema string) *Sqlite {
	t.Helper()
	s := NewSqlite(func(ctx context.Context, handle *dbSql.DB) error {
		_, err := handle.ExecContext(ctx, schema)
		return err
	})
	require.NoError(t, s.Provision(t.Context()))
	t.Cleanup(func() { _ = s.Down(context.Background()) })
	require.NoError(t, s.Migrate(t.Context()))
	return s
}

func execSqlite(t *testing.T, s *Sqlite, statement string) {
	t.Helper()
	require.NoError(t, attemptSqlite(t, s, statement))
}

func attemptSqlite(t *testing.T, s *Sqlite, statement string) error {
	t.Helper()
	_, err := s.Handle().ExecContext(t.Context(), statement)
	return err
}

func columnSqlite(t *testing.T, s *Sqlite, query string) []string {
	t.Helper()
	rows, err := s.Handle().QueryContext(t.Context(), query)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var value string
		require.NoError(t, rows.Scan(&value))
		out = append(out, value)
	}
	require.NoError(t, rows.Err())
	return out
}
