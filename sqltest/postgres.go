// Package sqltest runs a component test against a real postgres.
package sqltest

import (
	"context"
	dbSql "database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"gitlab.com/gorib/sql"
)

const startupTimeout = 60 * time.Second

// Taken as a function rather than driven from here, to keep goose out of the
// sql module's dependency graph, which every service carries.
type MigrateFunc func(ctx context.Context, handle *dbSql.DB) error

// Truncating one of these would leave a migrated database claiming to be empty.
// A domain table under one of these names has to be listed by Tables.
var bookkeeping = []string{"goose_db_version", "migrations", "schema_migrations"}

type Option func(*Postgres)

func Dsn(dsn string) Option {
	return func(p *Postgres) { p.dsn = dsn }
}

// First non-empty wins. There is no built-in default, because one carrying a
// password would put a credential in this repository.
func Env(keys ...string) Option {
	return func(p *Postgres) { p.envKeys = append(p.envKeys, keys...) }
}

func Migrations(migrate MigrateFunc) Option {
	return func(p *Postgres) { p.migrate = migrate }
}

// Without it the tables are discovered from the schema.
func Tables(names ...string) Option {
	return func(p *Postgres) { p.tables = append(p.tables, names...) }
}

// Only needed alongside Docker — a DSN already carries the name.
func Database(name string) Option {
	return func(p *Postgres) { p.database = name }
}

// The process id in the name is what lets `go test ./...` run two packages at
// once. So the name in a DSN is not the name used.
func Ephemeral() Option {
	return func(p *Postgres) { p.ephemeral = true }
}

// Appended to the name however it was given, DSN included — Database alone is
// ignored there, and a second instance would land in the first one's database.
func Suffix(suffix string) Option {
	return func(p *Postgres) { p.suffix = suffix }
}

func New(opts ...Option) *Postgres {
	// Not an option: this type speaks postgres and nothing else.
	p := &Postgres{driver: "pgx"}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type Postgres struct {
	driver    string
	dsn       string
	envKeys   []string
	migrate   MigrateFunc
	tables    []string
	database  string
	ephemeral bool
	suffix    string
	suffixed  bool
	container *container
	// Not container != nil: with Env and Docker both set, CI resolves on the env
	// branch and nothing is coming up to wait for.
	started bool
	base    string // the name before the process id, for the sweep

	baseline baseline

	db     sql.Db
	handle *dbSql.DB
	fault  fault
}

func (p *Postgres) Name() string { return "postgres" }

func (p *Postgres) Db() sql.Db {
	if p.db == nil {
		return nil
	}
	return &faultyDb{inner: p.db, fault: &p.fault}
}

// Every Db handed out fails with err until Reset; nil reads as a refused connection.
func (p *Postgres) FailWith(err error) { p.fault.fail(err) }

func (p *Postgres) Handle() *dbSql.DB { return p.handle }

func (p *Postgres) Provision(ctx context.Context) error {
	if err := p.resolve(ctx); err != nil {
		return err
	}
	if err := p.await(ctx); err != nil {
		return err
	}
	if err := p.separate(); err != nil {
		return err
	}
	if err := p.personalize(); err != nil {
		return err
	}
	if err := p.ensureDatabase(ctx); err != nil {
		return err
	}
	if err := p.sweep(ctx); err != nil {
		return err
	}

	db, err := sql.NewDb(p.driver, p.dsn)
	if err != nil {
		return fmt.Errorf("cannot build a handle for %s: %w", address(p.dsn), err)
	}
	if _, err = db.Connect(ctx); err != nil {
		return unreachable(p.dsn, err)
	}
	handle, err := db.DbSql()
	if err != nil {
		return unreachable(p.dsn, err)
	}
	if err = handle.PingContext(ctx); err != nil {
		return unreachable(p.dsn, err)
	}

	p.db, p.handle = db, handle
	return nil
}

func (p *Postgres) Migrate(ctx context.Context) error {
	if p.migrate != nil {
		if err := p.migrate(ctx, p.handle); err != nil {
			return fmt.Errorf("migrations failed on %s: %w", address(p.dsn), err)
		}
	}
	if len(p.tables) == 0 {
		if err := p.discover(ctx); err != nil {
			return err
		}
	}
	if err := p.remember(ctx); err != nil {
		return fmt.Errorf("cannot keep what the migrations left in %s: %w", address(p.dsn), err)
	}
	// Once here, so a restore that cannot work fails the one Migrate, not every Reset.
	return p.Reset(ctx)
}

// Truncate rather than a held-open transaction: the scenario talks to the
// service over a real connection, and context values do not cross that.
func (p *Postgres) Reset(ctx context.Context) error {
	p.fault.heal()
	if len(p.tables) == 0 {
		return nil
	}
	// Down closes the handle but leaves the table list, so without this a Reset
	// after teardown dereferences nil instead of saying what happened.
	if p.handle == nil {
		return errors.New("sqltest: the database is not open — Provision has not run, or Down already closed it")
	}
	if err := p.restore(ctx); err != nil {
		return fmt.Errorf("cannot reset %s: %w", address(p.dsn), err)
	}
	return nil
}

// The container, if one was started, is left running: starting it is the only
// part that costs anything, and the next run reuses it.
func (p *Postgres) Down(ctx context.Context) error {
	if !p.ephemeral || p.dsn == "" {
		return nil
	}
	name, err := p.databaseName()
	if err != nil {
		return err
	}
	if closer, ok := p.db.(io.Closer); ok {
		_ = closer.Close()
		p.handle, p.db = nil, nil
	}
	return p.drop(ctx, name)
}

// Explicit, environment, container — the container last, so CI and anyone with
// their own server never start one.
func (p *Postgres) resolve(ctx context.Context) error {
	if p.dsn != "" {
		return nil
	}
	for _, key := range p.envKeys {
		if value := os.Getenv(key); value != "" {
			p.dsn = value
			return nil
		}
	}
	if p.container != nil {
		if p.database == "" {
			return errNoDatabase
		}
		if err := p.container.ensure(ctx); err != nil {
			return err
		}
		p.started = true
		p.dsn = p.container.dsn(p.database)
		return nil
	}
	if len(p.envKeys) == 0 {
		return errors.New("sqltest: no address given, pass sqltest.Dsn, sqltest.Env or sqltest.Docker")
	}
	return fmt.Errorf("sqltest: no address given, set %s", strings.Join(p.envKeys, " or "))
}

// Only for a container this run started. An address that was already there —
// CI, or a server of your own — should fail now rather than in a minute.
func (p *Postgres) await(ctx context.Context) error {
	if !p.started {
		return nil
	}
	deadline := time.Now().Add(startupTimeout)
	for {
		err := p.pingMaintenance(ctx)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the container %q did not accept connections within %s: %w",
				p.container.name, startupTimeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (p *Postgres) pingMaintenance(ctx context.Context) error {
	handle, err := p.maintenance()
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	return handle.PingContext(ctx)
}

// The server's own database: dropping the test one needs a connection outside it.
func (p *Postgres) maintenance() (*dbSql.DB, error) {
	dsn, err := withDatabase(p.dsn, "postgres")
	if err != nil {
		return nil, err
	}
	admin, err := sql.NewDb(p.driver, dsn)
	if err != nil {
		return nil, err
	}
	handle, err := admin.DbSql()
	if err != nil {
		return nil, unreachable(dsn, err)
	}
	return handle, nil
}

func (p *Postgres) databaseName() (string, error) {
	return databaseName(p.dsn)
}

func (p *Postgres) ensureDatabase(ctx context.Context) error {
	name, err := p.databaseName()
	if err != nil {
		return err
	}
	handle, err := p.maintenance()
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	if err = handle.PingContext(ctx); err != nil {
		return unreachable(p.dsn, err)
	}

	var exists bool
	row := handle.QueryRowContext(ctx, "select exists(select 1 from pg_database where datname = $1)", name)
	if err = row.Scan(&exists); err != nil {
		return fmt.Errorf("cannot tell whether database %q exists: %w", name, err)
	}
	if exists {
		return nil
	}
	if _, err = handle.ExecContext(ctx, "create database "+quoteIdent(name)); err != nil {
		return fmt.Errorf("cannot create database %q: %w", name, err)
	}
	// pg_database records no creation time, and a comment is the only place to
	// keep one that needs no table inside the database itself.
	stamp := createdMark + time.Now().UTC().Format(time.RFC3339)
	if _, err = handle.ExecContext(ctx, "comment on database "+quoteIdent(name)+" is '"+stamp+"'"); err != nil {
		return fmt.Errorf("cannot mark database %q: %w", name, err)
	}
	return nil
}

func (p *Postgres) discover(ctx context.Context) error {
	rows, err := p.handle.QueryContext(ctx, `
		select table_name from information_schema.tables
		where table_schema = current_schema() and table_type = 'BASE TABLE'`)
	if err != nil {
		return fmt.Errorf("cannot list the tables of %s: %w", address(p.dsn), err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return err
		}
		if !slices.Contains(bookkeeping, name) {
			p.tables = append(p.tables, name)
		}
	}
	return rows.Err()
}
