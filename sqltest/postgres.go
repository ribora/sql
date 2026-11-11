// Package sqltest runs a component test against a real postgres, mysql or sqlite.
package sqltest

import (
	"context"
	dbSql "database/sql"
	"fmt"
)

func New(opts ...Option) *Postgres {
	p := &Postgres{}
	p.host, p.dialect = postgresHost{}, p
	for _, opt := range opts {
		opt(&p.server)
	}
	return p
}

type Postgres struct {
	server
	baseline baseline
}

func (p *Postgres) Name() string { return "postgres" }

func (p *Postgres) discover(ctx context.Context) ([]string, error) {
	rows, err := p.handle.QueryContext(ctx, `
		select table_name from information_schema.tables
		where table_schema = current_schema() and table_type = 'BASE TABLE'`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

type postgresHost struct{}

var postgresContainer = containerKind{
	name:       "gorib-harness-pg",
	port:       55432,
	serverPort: 5432,
	noPassword: "POSTGRES_HOST_AUTH_METHOD=trust",
	dsn: func(port int, database string) string {
		return fmt.Sprintf("postgres://postgres@127.0.0.1:%d/%s?sslmode=disable", port, database)
	},
}

func (postgresHost) driver() string                                { return "pgx" }
func (postgresHost) databaseName(dsn string) (string, error)       { return databaseName(dsn) }
func (postgresHost) withDatabase(dsn, name string) (string, error) { return withDatabase(dsn, name) }
func (postgresHost) address(dsn string) string                     { return address(dsn) }
func (postgresHost) kind() containerKind                           { return postgresContainer }

func (postgresHost) maintenance(dsn string) (string, error) {
	return withDatabase(dsn, "postgres")
}

func (postgresHost) exists(ctx context.Context, admin *dbSql.DB, name string) (bool, error) {
	var exists bool
	err := admin.QueryRowContext(ctx, "select exists(select 1 from pg_database where datname = $1)", name).Scan(&exists)
	return exists, err
}

// pg_database records no creation time, and a comment is the only place to keep
// one that needs no table inside the database itself.
func (postgresHost) create(ctx context.Context, admin *dbSql.DB, name, mark string) error {
	if _, err := admin.ExecContext(ctx, "create database "+quoteIdent(name)); err != nil {
		return fmt.Errorf("cannot create database %q: %w", name, err)
	}
	if _, err := admin.ExecContext(ctx, "comment on database "+quoteIdent(name)+" is "+quoteLiteral(mark)); err != nil {
		return fmt.Errorf("cannot mark database %q: %w", name, err)
	}
	return nil
}

func (postgresHost) marks(ctx context.Context, admin *dbSql.DB) (map[string]string, error) {
	rows, err := admin.QueryContext(ctx, `
		select d.datname, coalesce(shobj_description(d.oid, 'pg_database'), '')
		from pg_database d where d.datistemplate = false`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	marks := map[string]string{}
	for rows.Next() {
		var name, comment string
		if err = rows.Scan(&name, &comment); err != nil {
			return nil, err
		}
		marks[name] = comment
	}
	return marks, rows.Err()
}

func (postgresHost) connected(ctx context.Context, admin *dbSql.DB, name string) (bool, error) {
	var count int
	err := admin.QueryRowContext(ctx, "select count(*) from pg_stat_activity where datname = $1", name).Scan(&count)
	return count > 0, err
}

// `with (force)` — postgres 13 and later: our own pool is not the only possible
// connection, and a leaked one would otherwise leave the schema behind.
func (postgresHost) drop(ctx context.Context, admin *dbSql.DB, name string) error {
	_, err := admin.ExecContext(ctx, "drop database if exists "+quoteIdent(name)+" with (force)")
	return err
}
