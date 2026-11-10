package sqltest

import (
	"context"
	"fmt"
	"strings"
)

// Outside current_schema(), so discover never lists it and truncate never reaches it.
const baselineSchema = "sqltest_baseline"

type baseline struct {
	inserts    []string
	sequences  []string
	lastValues []int64
	isCalled   []bool
}

func (p *Postgres) remember(ctx context.Context) error {
	p.baseline = baseline{}
	if _, err := p.handle.ExecContext(ctx, "drop schema if exists "+baselineSchema+" cascade"); err != nil {
		return err
	}
	for _, table := range p.tables {
		if err := p.rememberSequences(ctx, table); err != nil {
			return err
		}
		if err := p.rememberRows(ctx, table); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) rememberRows(ctx context.Context, table string) error {
	var filled bool
	row := p.handle.QueryRowContext(ctx,
		"select not relispartition and exists(select 1 from "+quoteIdent(table)+") from pg_class where oid = $1::regclass",
		quoteIdent(table))
	if err := row.Scan(&filled); err != nil || !filled {
		return err
	}
	columns, err := p.insertable(ctx, table)
	if err != nil {
		return err
	}
	if _, err = p.handle.ExecContext(ctx, "create schema if not exists "+baselineSchema); err != nil {
		return err
	}
	kept := baselineSchema + "." + quoteIdent(table)
	if _, err = p.handle.ExecContext(ctx, "create table "+kept+" as select "+columns+" from "+quoteIdent(table)); err != nil {
		return err
	}
	p.baseline.inserts = append(p.baseline.inserts,
		"insert into "+quoteIdent(table)+" ("+columns+") overriding system value select "+columns+" from "+kept)
	return nil
}

func (p *Postgres) insertable(ctx context.Context, table string) (string, error) {
	rows, err := p.handle.QueryContext(ctx, `
		select column_name from information_schema.columns
		where table_schema = current_schema() and table_name = $1 and is_generated = 'NEVER'
		order by ordinal_position`, table)
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

func (p *Postgres) rememberSequences(ctx context.Context, table string) error {
	rows, err := p.handle.QueryContext(ctx, `
		select owned from (
			select pg_get_serial_sequence(quote_ident(table_name), column_name) as owned
			from information_schema.columns
			where table_schema = current_schema() and table_name = $1
		) columns where owned is not null`, table)
	if err != nil {
		return err
	}
	var owned []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		owned = append(owned, name)
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}

	for _, name := range owned {
		var lastValue int64
		var isCalled bool
		if err = p.handle.QueryRowContext(ctx, "select last_value, is_called from "+name).Scan(&lastValue, &isCalled); err != nil {
			return err
		}
		p.baseline.sequences = append(p.baseline.sequences, name)
		p.baseline.lastValues = append(p.baseline.lastValues, lastValue)
		p.baseline.isCalled = append(p.baseline.isCalled, isCalled)
	}
	return nil
}

func (p *Postgres) restore(ctx context.Context) error {
	tx, err := p.handle.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if len(p.baseline.inserts) > 0 {
		if _, err = tx.ExecContext(ctx, "set local session_replication_role = replica"); err != nil {
			return fmt.Errorf("putting back the rows the migrations left needs a superuser: %w", err)
		}
	}
	quoted := make([]string, 0, len(p.tables))
	for _, table := range p.tables {
		quoted = append(quoted, quoteIdent(table))
	}
	if _, err = tx.ExecContext(ctx, "truncate "+strings.Join(quoted, ", ")+" cascade"); err != nil {
		return err
	}
	for _, insert := range p.baseline.inserts {
		if _, err = tx.ExecContext(ctx, insert); err != nil {
			return err
		}
	}
	if len(p.baseline.sequences) > 0 {
		_, err = tx.ExecContext(ctx, `
			select setval(name::regclass, last_value, is_called)
			from unnest($1::text[], $2::bigint[], $3::bool[]) as kept(name, last_value, is_called)`,
			p.baseline.sequences, p.baseline.lastValues, p.baseline.isCalled)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
