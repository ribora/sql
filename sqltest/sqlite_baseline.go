package sqltest

import (
	"context"
	dbSql "database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type sqliteBaseline struct {
	inserts   []string
	sequences []sqliteSequence // nil without an AUTOINCREMENT table: sqlite_sequence does not exist
}

type sqliteSequence struct {
	name string
	seq  int64
}

func (s *Sqlite) remember(ctx context.Context) error {
	s.baseline = sqliteBaseline{}
	if err := os.Remove(s.kept()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return s.attached(ctx, func(conn *dbSql.Conn) error {
		for _, table := range s.tables {
			if err := s.rememberRows(ctx, conn, table); err != nil {
				return err
			}
		}
		return s.rememberSequences(ctx, conn)
	})
}

func (s *Sqlite) rememberRows(ctx context.Context, conn *dbSql.Conn, table string) error {
	var filled bool
	if err := conn.QueryRowContext(ctx, "select exists(select 1 from main."+quoteIdent(table)+")").Scan(&filled); err != nil || !filled {
		return err
	}
	columns, err := s.insertable(ctx, conn, table)
	if err != nil {
		return err
	}
	kept := baselineSchema + "." + quoteIdent(table)
	if _, err = conn.ExecContext(ctx, "create table "+kept+" as select "+columns+" from main."+quoteIdent(table)); err != nil {
		return err
	}
	s.baseline.inserts = append(s.baseline.inserts,
		"insert into main."+quoteIdent(table)+" ("+columns+") select "+columns+" from "+kept)
	return nil
}

func (s *Sqlite) insertable(ctx context.Context, conn *dbSql.Conn, table string) (string, error) {
	rows, err := conn.QueryContext(ctx, `select name from pragma_table_xinfo(?, 'main') where hidden = 0 order by cid`, table)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	var quoted []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return "", err
		}
		quoted = append(quoted, quoteIdent(name))
	}
	return strings.Join(quoted, ", "), rows.Err()
}

func (s *Sqlite) rememberSequences(ctx context.Context, conn *dbSql.Conn) error {
	var exists bool
	row := conn.QueryRowContext(ctx, "select exists(select 1 from main.sqlite_schema where name = 'sqlite_sequence')")
	if err := row.Scan(&exists); err != nil || !exists {
		return err
	}
	rows, err := conn.QueryContext(ctx, "select name, seq from main.sqlite_sequence")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	s.baseline.sequences = []sqliteSequence{}
	for rows.Next() {
		var kept sqliteSequence
		if err = rows.Scan(&kept.name, &kept.seq); err != nil {
			return err
		}
		s.baseline.sequences = append(s.baseline.sequences, kept)
	}
	return rows.Err()
}

func (s *Sqlite) restore(ctx context.Context) error {
	return s.attached(ctx, func(conn *dbSql.Conn) error {
		if _, err := conn.ExecContext(ctx, "pragma foreign_keys = off"); err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		triggers, err := sqliteTriggers(ctx, tx)
		if err != nil {
			return err
		}
		for _, trigger := range triggers {
			if _, err = tx.ExecContext(ctx, "drop trigger main."+quoteIdent(trigger.name)); err != nil {
				return err
			}
		}
		for _, table := range s.tables {
			if _, err = tx.ExecContext(ctx, "delete from main."+quoteIdent(table)); err != nil {
				return err
			}
		}
		for _, insert := range s.baseline.inserts {
			if _, err = tx.ExecContext(ctx, insert); err != nil {
				return err
			}
		}
		if s.baseline.sequences != nil {
			if _, err = tx.ExecContext(ctx, "delete from main.sqlite_sequence"); err != nil {
				return err
			}
			for _, kept := range s.baseline.sequences {
				if _, err = tx.ExecContext(ctx, "insert into main.sqlite_sequence (name, seq) values (?, ?)", kept.name, kept.seq); err != nil {
					return err
				}
			}
		}
		for _, trigger := range triggers {
			if _, err = tx.ExecContext(ctx, trigger.sql); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

type sqliteTrigger struct {
	name string
	sql  string
}

func sqliteTriggers(ctx context.Context, tx *dbSql.Tx) ([]sqliteTrigger, error) {
	rows, err := tx.QueryContext(ctx, "select name, sql from main.sqlite_schema where type = 'trigger'")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []sqliteTrigger
	for rows.Next() {
		var trigger sqliteTrigger
		if err = rows.Scan(&trigger.name, &trigger.sql); err != nil {
			return nil, err
		}
		out = append(out, trigger)
	}
	return out, rows.Err()
}

func (s *Sqlite) attached(ctx context.Context, do func(conn *dbSql.Conn) error) error {
	return s.throwaway(ctx, func(conn *dbSql.Conn) error {
		if _, err := conn.ExecContext(ctx, "attach database ? as "+baselineSchema, s.kept()); err != nil {
			return err
		}
		return do(conn)
	})
}

func (s *Sqlite) kept() string { return filepath.Join(s.dir, "baseline.db") }
