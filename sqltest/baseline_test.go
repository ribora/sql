package sqltest

import (
	"context"
	dbSql "database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const seeded = `
create table states (id int generated always as identity primary key, name text not null);
insert into states (name) values ('new'), ('active'), ('closed');

create table categories (
	id bigserial primary key,
	parent_id bigint references categories(id),
	name text not null,
	name_length int generated always as (length(name)) stored
);
insert into categories (parent_id, name) values (null, 'Master');
insert into categories (parent_id, name) values (1, 'Child');

create table vendors (id serial primary key, state_id int not null references states(id), name text not null);
insert into vendors (state_id, name) values (1, 'R1');

create table audit (id serial primary key, what text not null);
create function audited() returns trigger language plpgsql as $$
begin
	insert into audit (what) values (new.name);
	return new;
end $$;
create trigger vendors_audited after insert on vendors for each row execute function audited();

create table orders (id serial primary key, vendor_id int not null references vendors(id));
`

func TestResetPutsBackWhatTheMigrationsLeft(t *testing.T) {
	p := migrated(t, "back", seeded)
	apply(t, p, `insert into states (name) values ('extra')`)
	apply(t, p, `update vendors set name = 'renamed'`)
	apply(t, p, `delete from categories where parent_id is not null`)
	apply(t, p, `insert into orders (vendor_id) values (1)`)

	require.NoError(t, p.Reset(t.Context()))

	assert.Equal(t, []string{"new", "active", "closed"}, column(t, p, `select name from states order by id`))
	assert.Equal(t, []string{"Master", "Child"}, column(t, p, `select name from categories order by id`))
	assert.Equal(t, []string{"R1"}, column(t, p, `select name from vendors order by id`))
	assert.Empty(t, column(t, p, `select id::text from orders`))
}

func TestIdsContinueAfterTheRowsPutBack(t *testing.T) {
	p := migrated(t, "ids", seeded)
	apply(t, p, `insert into states (name) values ('extra')`)
	apply(t, p, `insert into orders (vendor_id) values (1)`)

	require.NoError(t, p.Reset(t.Context()))

	assert.Equal(t, []string{"4"}, column(t, p, `insert into states (name) values ('next') returning id`))
	assert.Equal(t, []string{"2"}, column(t, p, `insert into vendors (state_id, name) values (1, 'R2') returning id`))
	assert.Equal(t, []string{"1"}, column(t, p, `insert into orders (vendor_id) values (1) returning id`))
}

func TestResetPutsTablesBackInAnyOrder(t *testing.T) {
	p := migrated(t, "order", seeded, Tables("orders", "vendors", "audit", "categories", "states"))

	require.NoError(t, p.Reset(t.Context()))

	assert.Equal(t, []string{"R1"}, column(t, p, `select name from vendors`))
}

func TestResetFiresNoTrigger(t *testing.T) {
	p := migrated(t, "trigger", seeded)

	require.NoError(t, p.Reset(t.Context()))

	assert.Empty(t, column(t, p, `select what from audit`))
}

func TestGeneratedColumnsAreLeftToPostgres(t *testing.T) {
	p := migrated(t, "generated", seeded)
	apply(t, p, `update categories set name = 'Renamed' where id = 1`)

	require.NoError(t, p.Reset(t.Context()))

	assert.Equal(t, []string{"6", "5"}, column(t, p, `select name_length::text from categories order by id`))
}

func TestNothingIsKeptWhenTheMigrationsLeaveNoRows(t *testing.T) {
	p := migrated(t, "empty", `create table orders (id serial primary key)`)
	apply(t, p, `insert into orders default values`)

	require.NoError(t, p.Reset(t.Context()))

	assert.Equal(t, []string{"1"}, column(t, p, `insert into orders default values returning id`))
	assert.Equal(t, []string{"false"}, column(t, p,
		`select exists(select 1 from information_schema.schemata where schema_name = 'sqltest_baseline')::text`))
}

func TestMigrationsThatCreateNoTablesLeaveNothingToReset(t *testing.T) {
	p := migrated(t, "none", `select 1`)

	require.NoError(t, p.Reset(t.Context()))
}

func migrated(t *testing.T, suffix, schema string, opts ...Option) *Postgres {
	t.Helper()
	p := New(append([]Option{
		Env("SQLTEST_TEST_DSN"),
		Docker("postgres:18-alpine"),
		Database("sqltest_test"),
		Suffix(suffix),
		Ephemeral(),
		Migrations(func(ctx context.Context, handle *dbSql.DB) error {
			_, err := handle.ExecContext(ctx, schema)
			return err
		}),
	}, opts...)...)
	require.NoError(t, p.Provision(t.Context()))
	t.Cleanup(func() { _ = p.Down(context.Background()) })
	require.NoError(t, p.Migrate(t.Context()))
	return p
}

func apply(t *testing.T, p *Postgres, statement string) {
	t.Helper()
	_, err := p.Handle().ExecContext(t.Context(), statement)
	require.NoError(t, err)
}

func column(t *testing.T, p *Postgres, query string) []string {
	t.Helper()
	rows, err := p.Handle().QueryContext(t.Context(), query)
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
