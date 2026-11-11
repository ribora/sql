package sqltest

import (
	"context"
	dbSql "database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var seededMysql = []string{
	`create table states (id int auto_increment primary key, name varchar(50) not null)`,
	`insert into states (name) values ('new'), ('active'), ('closed')`,
	`create table categories (
		id bigint auto_increment primary key,
		parent_id bigint,
		name varchar(50) not null,
		name_length int generated always as (char_length(name)) stored,
		created_at datetime(6) not null default current_timestamp(6),
		foreign key (parent_id) references categories(id)
	)`,
	`insert into categories (parent_id, name) values (null, 'Master')`,
	`insert into categories (parent_id, name) values (1, 'Child')`,
	`create table vendors (
		id int auto_increment primary key,
		state_id int not null,
		name varchar(50) not null,
		foreign key (state_id) references states(id) on delete restrict
	)`,
	`insert into vendors (state_id, name) values (1, 'R1')`,
	`create table audit (id int auto_increment primary key, what varchar(50) not null)`,
	`create trigger vendors_audited after insert on vendors for each row insert into audit (what) values (new.name)`,
	`create trigger vendors_forgotten after delete on vendors for each row insert into audit (what) values (concat('-', old.name))`,
	`create table orders (
		id int auto_increment primary key,
		vendor_id int not null,
		foreign key (vendor_id) references vendors(id)
	)`,
}

func TestMysqlResetPutsBackWhatTheMigrationsLeft(t *testing.T) {
	m := migratedMysql(t, "back", seededMysql...)
	execMysql(t, m, `insert into states (name) values ('extra')`)
	execMysql(t, m, `update vendors set name = 'renamed'`)
	execMysql(t, m, `delete from categories where parent_id is not null`)
	execMysql(t, m, `insert into orders (vendor_id) values (1)`)

	require.NoError(t, m.Reset(t.Context()))

	assert.Equal(t, []string{"new", "active", "closed"}, columnMysql(t, m, `select name from states order by id`))
	assert.Equal(t, []string{"Master", "Child"}, columnMysql(t, m, `select name from categories order by id`))
	assert.Equal(t, []string{"R1"}, columnMysql(t, m, `select name from vendors order by id`))
	assert.Empty(t, columnMysql(t, m, `select id from orders`))
}

func TestMysqlIdsContinueAfterTheRowsPutBack(t *testing.T) {
	m := migratedMysql(t, "ids", seededMysql...)
	execMysql(t, m, `insert into states (name) values ('extra')`)
	execMysql(t, m, `insert into orders (vendor_id) values (1)`)

	require.NoError(t, m.Reset(t.Context()))

	assert.Equal(t, int64(4), insertedMysql(t, m, `insert into states (name) values ('next')`))
	assert.Equal(t, int64(2), insertedMysql(t, m, `insert into vendors (state_id, name) values (1, 'R2')`))
	assert.Equal(t, int64(1), insertedMysql(t, m, `insert into orders (vendor_id) values (1)`))
}

func TestMysqlAGapTheMigrationsLeftIsKept(t *testing.T) {
	m := migratedMysql(t, "gap",
		`create table states (id int auto_increment primary key, name varchar(50) not null)`,
		`insert into states (name) values ('new'), ('active'), ('dropped')`,
		`delete from states where name = 'dropped'`)

	require.NoError(t, m.Reset(t.Context()))

	assert.Equal(t, int64(4), insertedMysql(t, m, `insert into states (name) values ('next')`))
}

func TestMysqlACounterMovedByARowSinceDeletedIsRewound(t *testing.T) {
	m := migratedMysql(t, "counter", seededMysql...)
	execMysql(t, m, `insert into states (name) values ('extra')`)
	execMysql(t, m, `delete from states where name = 'extra'`)

	require.NoError(t, m.Reset(t.Context()))

	assert.Equal(t, int64(4), insertedMysql(t, m, `insert into states (name) values ('next')`))
}

func TestMysqlResetFiresNoTriggerAndKeepsThem(t *testing.T) {
	m := migratedMysql(t, "trigger", seededMysql...)
	execMysql(t, m, `update vendors set name = 'renamed'`)

	require.NoError(t, m.Reset(t.Context()))
	assert.Empty(t, columnMysql(t, m, `select what from audit`))

	execMysql(t, m, `insert into vendors (state_id, name) values (1, 'R2')`)
	assert.Equal(t, []string{"R2"}, columnMysql(t, m, `select what from audit`))
}

func TestMysqlATriggerAnEarlierResetLostComesBack(t *testing.T) {
	m := migratedMysql(t, "lost", seededMysql...)
	execMysql(t, m, `drop trigger vendors_audited`)

	require.NoError(t, m.Reset(t.Context()))

	execMysql(t, m, `insert into vendors (state_id, name) values (1, 'R2')`)
	assert.Equal(t, []string{"R2"}, columnMysql(t, m, `select what from audit`))
}

func TestMysqlTriggersComeBackInTheirOrder(t *testing.T) {
	m := migratedMysql(t, "order",
		`create table items (id int auto_increment primary key, name varchar(50) not null)`,
		`create table log (id int auto_increment primary key, what varchar(50) not null)`,
		`create trigger b_first after insert on items for each row insert into log (what) values ('first')`,
		`create trigger a_second after insert on items for each row follows b_first insert into log (what) values ('second')`,
		`insert into items (name) values ('seeded')`)
	execMysql(t, m, `insert into items (name) values ('extra')`)

	require.NoError(t, m.Reset(t.Context()))

	execMysql(t, m, `insert into items (name) values ('next')`)
	assert.Equal(t, []string{"first", "second", "first", "second"}, columnMysql(t, m, `select what from log order by id`))
}

func TestMysqlARowTheMigrationsMovedToIdZeroKeepsIt(t *testing.T) {
	m := migratedMysql(t, "zero",
		`create table states (id int auto_increment primary key, name varchar(50) not null)`,
		`insert into states (name) values ('none'), ('new')`,
		`update states set id = 0 where name = 'none'`)
	execMysql(t, m, `insert into states (name) values ('extra')`)

	require.NoError(t, m.Reset(t.Context()))

	assert.Equal(t, []string{"0 none", "2 new"}, columnMysql(t, m, `select concat(id, ' ', name) from states order by id`))
}

func TestMysqlForeignKeysAreCheckedAfterAReset(t *testing.T) {
	m := migratedMysql(t, "keys", seededMysql...)

	require.NoError(t, m.Reset(t.Context()))

	assert.ErrorContains(t, attemptMysql(t, m, `insert into orders (vendor_id) values (99)`), "foreign key constraint fails")
}

func TestMysqlColumnsTheServerFillsComeBackAsTheyWere(t *testing.T) {
	m := migratedMysql(t, "generated", seededMysql...)
	created := columnMysql(t, m, `select created_at from categories order by id`)
	execMysql(t, m, `update categories set name = 'Renamed' where id = 1`)

	require.NoError(t, m.Reset(t.Context()))

	assert.Equal(t, []string{"6", "5"}, columnMysql(t, m, `select name_length from categories order by id`))
	assert.Equal(t, created, columnMysql(t, m, `select created_at from categories order by id`))
}

func TestMysqlBookkeepingSurvivesAReset(t *testing.T) {
	m := migratedMysql(t, "bookkeeping",
		`create table migrations (id int auto_increment primary key, version_id bigint not null)`,
		`insert into migrations (version_id) values (1)`,
		`create table orders (id int auto_increment primary key)`)
	execMysql(t, m, `insert into orders values ()`)

	require.NoError(t, m.Reset(t.Context()))

	assert.Equal(t, []string{"1"}, columnMysql(t, m, `select version_id from migrations`))
	assert.Empty(t, columnMysql(t, m, `select id from orders`))
}

func TestMysqlFailWithFailsTheDbUntilReset(t *testing.T) {
	m := migratedMysql(t, "fault", seededMysql...)
	m.FailWith(nil)

	_, err := m.Db().Begin(t.Context())
	assert.ErrorIs(t, err, errRefused)

	require.NoError(t, m.Reset(t.Context()))
	tx, err := m.Db().Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
}

func TestMysqlARefusedTableRefusesEveryWrite(t *testing.T) {
	m := migratedMysql(t, "refused", seededMysql...)

	require.NoError(t, m.Refuse(t.Context(), "vendors"))

	assert.ErrorContains(t, attemptMysql(t, m, `insert into vendors (state_id, name) values (1, 'R2')`), "insert on vendors is refused")
	assert.ErrorContains(t, attemptMysql(t, m, `update vendors set name = 'renamed'`), "update on vendors is refused")
	assert.ErrorContains(t, attemptMysql(t, m, `delete from vendors`), "delete on vendors is refused")
	assert.Equal(t, []string{"R1"}, columnMysql(t, m, `select name from vendors`))
}

func TestMysqlARefusalLeavesTheOtherTablesAndWritesAlone(t *testing.T) {
	m := migratedMysql(t, "elsewhere", seededMysql...)

	require.NoError(t, m.Refuse(t.Context(), "vendors", Delete))

	assert.NoError(t, attemptMysql(t, m, `insert into states (name) values ('extra')`))
	assert.NoError(t, attemptMysql(t, m, `update vendors set name = 'renamed'`))
	assert.ErrorContains(t, attemptMysql(t, m, `delete from vendors`), "refused")
}

func TestMysqlAQuotaIsSharedByTheWritesItNames(t *testing.T) {
	m := migratedMysql(t, "quota", seededMysql...)

	require.NoError(t, m.RefuseAfter(t.Context(), "states", 1))

	assert.NoError(t, attemptMysql(t, m, `update states set name = 'renamed' where id = 1`))
	assert.ErrorContains(t, attemptMysql(t, m, `insert into states (name) values ('second')`), "refused")
}

func TestMysqlARolledBackRowGivesItsPlaceBack(t *testing.T) {
	m := migratedMysql(t, "rolledback", seededMysql...)
	require.NoError(t, m.RefuseAfter(t.Context(), "states", 1))

	tx, err := m.Handle().BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `insert into states (name) values ('undone')`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	assert.NoError(t, attemptMysql(t, m, `insert into states (name) values ('kept')`))
}

func TestMysqlRefusingAgainReplacesTheQuota(t *testing.T) {
	m := migratedMysql(t, "again", seededMysql...)
	require.NoError(t, m.Refuse(t.Context(), "states"))

	require.NoError(t, m.RefuseAfter(t.Context(), "states", 1))

	assert.NoError(t, attemptMysql(t, m, `insert into states (name) values ('first')`))
}

func TestMysqlATableWithTheLongestNameCanBeRefused(t *testing.T) {
	table := strings.Repeat("t", 64)
	m := migratedMysql(t, "long", `create table `+table+` (id int auto_increment primary key)`)

	require.NoError(t, m.Refuse(t.Context(), table))

	assert.ErrorContains(t, attemptMysql(t, m, `insert into `+table+` values ()`), "refused")
}

func TestMysqlHealLiftsTheRefusalsAndKeepsTheRows(t *testing.T) {
	m := migratedMysql(t, "healed", seededMysql...)
	execMysql(t, m, `insert into states (name) values ('extra')`)
	require.NoError(t, m.Refuse(t.Context(), "vendors"))
	m.FailWith(nil)

	require.NoError(t, m.Heal(t.Context()))

	assert.NoError(t, attemptMysql(t, m, `insert into vendors (state_id, name) values (1, 'R2')`))
	tx, err := m.Db().Begin(t.Context())
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
	assert.Equal(t, []string{"new", "active", "closed", "extra"}, columnMysql(t, m, `select name from states order by id`))
}

func TestMysqlResetLiftsTheRefusals(t *testing.T) {
	m := migratedMysql(t, "lifted", seededMysql...)
	require.NoError(t, m.Refuse(t.Context(), "vendors"))

	require.NoError(t, m.Reset(t.Context()))

	assert.NoError(t, attemptMysql(t, m, `insert into vendors (state_id, name) values (1, 'R2')`))
}

func TestMysqlDownDropsTheDatabase(t *testing.T) {
	m := migratedMysql(t, "down", seededMysql...)
	name, err := m.databaseName()
	require.NoError(t, err)

	require.NoError(t, m.Down(t.Context()))

	admin, err := m.maintenance()
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()
	exists, err := mysqlHost{}.exists(t.Context(), admin, name)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestMysqlProvisionSweepsWhatAKilledRunLeft(t *testing.T) {
	m := mysqlServer("sweep")
	require.NoError(t, m.resolve(t.Context()))
	require.NoError(t, m.separate())
	base, err := m.databaseName()
	require.NoError(t, err)
	admin, err := m.maintenance()
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()
	leftover := base + "_1"
	require.NoError(t, mysqlHost{}.drop(t.Context(), admin, leftover))
	require.NoError(t, mysqlHost{}.create(t.Context(), admin, leftover, createdMark+time.Now().Add(-2*sweepAfter).UTC().Format(time.RFC3339)))

	require.NoError(t, m.Provision(t.Context()))
	t.Cleanup(func() { _ = m.Down(context.Background()) })

	exists, err := mysqlHost{}.exists(t.Context(), admin, leftover)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestMysqlAddressDropsTheCredentials(t *testing.T) {
	reported := mysqlHost{}.address("root:fake-pw@tcp(127.0.0.1:53306)/bastion_test?parseTime=true")

	assert.Equal(t, "127.0.0.1:53306/bastion_test", reported)
}

func TestMysqlWithDatabaseKeepsTheRestOfTheAddress(t *testing.T) {
	dsn, err := mysqlHost{}.withDatabase("app:pw@tcp(db:3306)/shop?parseTime=true&loc=Local&multiStatements=true", "shop_test_1")

	require.NoError(t, err)
	assert.Contains(t, dsn, "app:pw@tcp(db:3306)/shop_test_1?")
	for _, param := range []string{"parseTime=true", "loc=Local", "multiStatements=true"} {
		assert.Contains(t, dsn, param)
	}
}

func TestMysqlAddressWithoutADatabaseIsRefused(t *testing.T) {
	_, err := mysqlHost{}.databaseName("root@tcp(127.0.0.1:3306)/")

	assert.ErrorContains(t, err, "names no database")
}

func TestMysqlContainerAddressCarriesNoCredential(t *testing.T) {
	c := &container{kind: mysqlContainer, port: 53306}

	assert.Equal(t, "root@tcp(127.0.0.1:53306)/bastion_test?parseTime=true", c.dsn("bastion_test"))
}

func mysqlServer(suffix string, opts ...Option) *Mysql {
	return NewMysql(append([]Option{
		Env("SQLTEST_MYSQL_DSN"),
		Docker("mysql:8"),
		Database("sqltest_test"),
		Suffix(suffix),
		Ephemeral(),
	}, opts...)...)
}

func migratedMysql(t *testing.T, suffix string, schema ...string) *Mysql {
	t.Helper()
	m := mysqlServer(suffix, Migrations(func(ctx context.Context, handle *dbSql.DB) error {
		for _, statement := range schema {
			if _, err := handle.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, m.Provision(t.Context()))
	t.Cleanup(func() { _ = m.Down(context.Background()) })
	require.NoError(t, m.Migrate(t.Context()))
	return m
}

func execMysql(t *testing.T, m *Mysql, statement string) {
	t.Helper()
	require.NoError(t, attemptMysql(t, m, statement))
}

func attemptMysql(t *testing.T, m *Mysql, statement string) error {
	t.Helper()
	_, err := m.Handle().ExecContext(t.Context(), statement)
	return err
}

func insertedMysql(t *testing.T, m *Mysql, statement string) int64 {
	t.Helper()
	result, err := m.Handle().ExecContext(t.Context(), statement)
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

func columnMysql(t *testing.T, m *Mysql, query string) []string {
	t.Helper()
	rows, err := m.Handle().QueryContext(t.Context(), query)
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
