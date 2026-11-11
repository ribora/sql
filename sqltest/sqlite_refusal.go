package sqltest

import (
	"context"
	"fmt"
	"strings"
)

// In main beside the tables, because a trigger reads only its own schema.
const sqliteQuota = "sqltest_refusal"

func (s *Sqlite) Refuse(ctx context.Context, table string, refused ...Write) error {
	return s.RefuseAfter(ctx, table, 0, refused...)
}

func (s *Sqlite) RefuseAfter(ctx context.Context, table string, rows int, refused ...Write) error {
	events, err := triggerEvents(refused)
	if err != nil {
		return err
	}
	name := "sqltest_refuse_" + strings.Join(events, "_")

	tx, err := s.handle.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, `create table if not exists `+sqliteQuota+` (
		relation text,
		name text,
		remaining int not null,
		primary key (relation, name))`); err != nil {
		return fmt.Errorf("cannot prepare the refusal on %s: %w", s.file(), err)
	}
	if _, err = tx.ExecContext(ctx, `
		insert into `+sqliteQuota+` (relation, name, remaining) values (?, ?, ?)
		on conflict (relation, name) do update set remaining = excluded.remaining`,
		table, name, rows); err != nil {
		return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
	}
	quota := "relation = " + quoteLiteral(table) + " and name = " + quoteLiteral(name)
	for _, event := range events {
		trigger := quoteIdent(name + "_" + event + "_on_" + table)
		if _, err = tx.ExecContext(ctx, "drop trigger if exists "+trigger); err != nil {
			return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
		}
		if _, err = tx.ExecContext(ctx, "create trigger "+trigger+" before "+event+" on "+quoteIdent(table)+" begin "+
			"update "+sqliteQuota+" set remaining = remaining - 1 where "+quota+"; "+
			"select raise(abort, "+quoteLiteral("sqltest: "+event+" on "+table+" is refused")+") "+
			"where (select remaining from "+sqliteQuota+" where "+quota+") < 0; "+
			"end"); err != nil {
			return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
		}
	}
	return tx.Commit()
}

func (s *Sqlite) lift(ctx context.Context) error {
	tx, err := s.handle.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `select name from sqlite_schema where type = 'trigger' and name like 'sqltest\_refuse\_%' escape '\'`)
	if err != nil {
		return fmt.Errorf("cannot lift the refusals on %s: %w", s.file(), err)
	}
	var triggers []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		triggers = append(triggers, name)
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}

	for _, trigger := range triggers {
		if _, err = tx.ExecContext(ctx, "drop trigger "+quoteIdent(trigger)); err != nil {
			return fmt.Errorf("cannot lift the refusals on %s: %w", s.file(), err)
		}
	}
	if _, err = tx.ExecContext(ctx, "drop table if exists "+sqliteQuota); err != nil {
		return fmt.Errorf("cannot lift the refusals on %s: %w", s.file(), err)
	}
	return tx.Commit()
}
