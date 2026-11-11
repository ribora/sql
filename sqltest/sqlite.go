package sqltest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// As a service's own DSN would set them: foreign keys checked, a lock waited for.
const sqlitePragmas = "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

func NewSqlite(migrate MigrateFunc) *Sqlite {
	s := &Sqlite{}
	s.migrate, s.dialect = migrate, s
	return s
}

type Sqlite struct {
	engine
	dir      string
	baseline sqliteBaseline
}

func (s *Sqlite) Name() string { return "sqlite" }

func (s *Sqlite) Provision(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "sqltest-")
	if err != nil {
		return fmt.Errorf("cannot make a directory for the database: %w", err)
	}
	s.dir = dir
	return s.open(ctx, "sqlite", s.file()+sqlitePragmas)
}

func (s *Sqlite) Down(context.Context) error {
	s.close()
	if s.dir == "" {
		return nil
	}
	return os.RemoveAll(s.dir)
}

func (s *Sqlite) where() string { return s.file() }

func (s *Sqlite) file() string { return filepath.Join(s.dir, "test.db") }

func (s *Sqlite) discover(ctx context.Context) ([]string, error) {
	rows, err := s.handle.QueryContext(ctx, `
		select name from pragma_table_list
		where schema = 'main' and type = 'table' and name not like 'sqlite\_%' escape '\'`)
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
