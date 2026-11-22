package sqltest

import (
	"context"
	dbSql "database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ribora/sql"
)

const startupTimeout = 60 * time.Second

// Taken as a function rather than driven from here, to keep goose out of the
// sql module's dependency graph, which every service carries.
type MigrateFunc func(ctx context.Context, handle *dbSql.DB) error

// Truncating one of these would leave a migrated database claiming to be empty.
// A domain table under one of these names has to be listed by Tables.
var bookkeeping = []string{"goose_db_version", "migrations", "schema_migrations"}

type server struct {
	engine
	host      host
	dsn       string
	envKeys   []string
	database  string
	ephemeral bool
	suffix    string
	suffixed  bool
	container *container
	// Not container != nil: with Env and Docker both set, CI resolves on the env
	// branch and nothing is coming up to wait for.
	started bool
	base    string // the name before the process id, for the sweep
}

type host interface {
	driver() string
	databaseName(dsn string) (string, error)
	withDatabase(dsn, name string) (string, error)
	address(dsn string) string
	maintenance(dsn string) (string, error)
	kind() containerKind

	exists(ctx context.Context, admin *dbSql.DB, name string) (bool, error)
	create(ctx context.Context, admin *dbSql.DB, name, mark string) error
	marks(ctx context.Context, admin *dbSql.DB) (map[string]string, error) // database to its mark, "" for none
	connected(ctx context.Context, admin *dbSql.DB, name string) (bool, error)
	drop(ctx context.Context, admin *dbSql.DB, name string) error
}

type Option func(*server)

func Dsn(dsn string) Option {
	return func(s *server) { s.dsn = dsn }
}

// First non-empty wins. There is no built-in default, because one carrying a
// password would put a credential in this repository.
func Env(keys ...string) Option {
	return func(s *server) { s.envKeys = append(s.envKeys, keys...) }
}

func Migrations(migrate MigrateFunc) Option {
	return func(s *server) { s.migrate = migrate }
}

// Without it the tables are discovered from the schema.
func Tables(names ...string) Option {
	return func(s *server) { s.tables = append(s.tables, names...) }
}

// Only needed alongside Docker — a DSN already carries the name.
func Database(name string) Option {
	return func(s *server) { s.database = name }
}

// The process id in the name is what lets `go test ./...` run two packages at
// once. So the name in a DSN is not the name used.
func Ephemeral() Option {
	return func(s *server) { s.ephemeral = true }
}

// Appended to the name however it was given, DSN included — Database alone is
// ignored there, and a second instance would land in the first one's database.
func Suffix(suffix string) Option {
	return func(s *server) { s.suffix = suffix }
}

func (s *server) where() string { return s.host.address(s.dsn) }

func (s *server) unreachable(dsn string, err error) error {
	return fmt.Errorf("no test database at %s: %w", s.host.address(dsn), err)
}

func (s *server) Provision(ctx context.Context) error {
	if err := s.resolve(ctx); err != nil {
		return err
	}
	if err := s.await(ctx); err != nil {
		return err
	}
	if err := s.separate(); err != nil {
		return err
	}
	if err := s.personalize(); err != nil {
		return err
	}
	if err := s.ensureDatabase(ctx); err != nil {
		return err
	}
	if err := s.sweep(ctx); err != nil {
		return err
	}
	return s.open(ctx, s.host.driver(), s.dsn)
}

// The container, if one was started, is left running: starting it is the only
// part that costs anything, and the next run reuses it.
func (s *server) Down(ctx context.Context) error {
	if !s.ephemeral || s.dsn == "" {
		return nil
	}
	name, err := s.databaseName()
	if err != nil {
		return err
	}
	s.close()
	admin, err := s.maintenance()
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close() }()
	if err = s.host.drop(ctx, admin, name); err != nil {
		return fmt.Errorf("cannot drop database %q: %w", name, err)
	}
	return nil
}

// Explicit, environment, container — the container last, so CI and anyone with
// their own server never start one.
func (s *server) resolve(ctx context.Context) error {
	if s.dsn != "" {
		return nil
	}
	for _, key := range s.envKeys {
		if value := os.Getenv(key); value != "" {
			s.dsn = value
			return nil
		}
	}
	if s.container != nil {
		if s.database == "" {
			return errNoDatabase
		}
		if err := s.container.ensure(ctx); err != nil {
			return err
		}
		s.started = true
		s.dsn = s.container.dsn(s.database)
		return nil
	}
	if len(s.envKeys) == 0 {
		return errors.New("sqltest: no address given, pass sqltest.Dsn, sqltest.Env or sqltest.Docker")
	}
	return fmt.Errorf("sqltest: no address given, set %s", strings.Join(s.envKeys, " or "))
}

// Only for a container this run started. An address that was already there —
// CI, or a server of your own — should fail now rather than in a minute.
func (s *server) await(ctx context.Context) error {
	if !s.started {
		return nil
	}
	deadline := time.Now().Add(startupTimeout)
	for {
		err := s.pingMaintenance(ctx)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the container %q did not accept connections within %s: %w",
				s.container.name, startupTimeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *server) pingMaintenance(ctx context.Context) error {
	admin, err := s.maintenance()
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close() }()
	return admin.PingContext(ctx)
}

// Creating and dropping the test database needs a connection outside it.
func (s *server) maintenance() (*dbSql.DB, error) {
	dsn, err := s.host.maintenance(s.dsn)
	if err != nil {
		return nil, err
	}
	admin, err := sql.NewDb(s.host.driver(), dsn)
	if err != nil {
		return nil, err
	}
	handle, err := admin.DbSql()
	if err != nil {
		return nil, s.unreachable(dsn, err)
	}
	return handle, nil
}

func (s *server) databaseName() (string, error) {
	return s.host.databaseName(s.dsn)
}

func (s *server) ensureDatabase(ctx context.Context) error {
	name, err := s.databaseName()
	if err != nil {
		return err
	}
	admin, err := s.maintenance()
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close() }()
	if err = admin.PingContext(ctx); err != nil {
		return s.unreachable(s.dsn, err)
	}

	exists, err := s.host.exists(ctx, admin, name)
	if err != nil {
		return fmt.Errorf("cannot tell whether database %q exists: %w", name, err)
	}
	if exists {
		return nil
	}
	return s.host.create(ctx, admin, name, createdMark+time.Now().UTC().Format(time.RFC3339))
}
