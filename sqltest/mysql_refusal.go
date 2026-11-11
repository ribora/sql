package sqltest

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
)

const mysqlQuota = "sqltest_refusal"

func (m *Mysql) Refuse(ctx context.Context, table string, refused ...Write) error {
	return m.RefuseAfter(ctx, table, 0, refused...)
}

func (m *Mysql) RefuseAfter(ctx context.Context, table string, rows int, refused ...Write) error {
	events, err := triggerEvents(refused)
	if err != nil {
		return err
	}
	name := "sqltest_refuse_" + strings.Join(events, "_")

	if _, err = m.handle.ExecContext(ctx, `create table if not exists `+mysqlQuota+` (
		relation varchar(64) not null,
		name varchar(64) not null,
		remaining int not null,
		primary key (relation, name))`); err != nil {
		return fmt.Errorf("cannot prepare the refusal on %s: %w", m.where(), err)
	}
	if _, err = m.handle.ExecContext(ctx, `
		insert into `+mysqlQuota+` (relation, name, remaining) values (?, ?, ?) as kept
		on duplicate key update remaining = kept.remaining`,
		table, name, rows); err != nil {
		return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
	}
	quota := "relation = " + mysqlLiteral(table) + " and name = " + mysqlLiteral(name)
	for _, event := range events {
		trigger := backtick(mysqlTriggerName(name, event, table))
		if _, err = m.handle.ExecContext(ctx, "drop trigger if exists "+trigger); err != nil {
			return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
		}
		if _, err = m.handle.ExecContext(ctx, "create trigger "+trigger+" before "+event+" on "+backtick(table)+
			" for each row begin "+
			"update "+mysqlQuota+" set remaining = remaining - 1 where "+quota+"; "+
			"if (select remaining from "+mysqlQuota+" where "+quota+") < 0 then "+
			"signal sqlstate '45000' set message_text = "+mysqlLiteral("sqltest: "+event+" on "+table+" is refused")+"; "+
			"end if; end"); err != nil {
			return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
		}
	}
	return nil
}

func mysqlTriggerName(refusal, event, table string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(refusal + " " + event + " " + table))
	return fmt.Sprintf("sqltest_refuse_%016x", hash.Sum64())
}

func (m *Mysql) lift(ctx context.Context) error {
	rows, err := m.handle.QueryContext(ctx, `
		select trigger_name from information_schema.triggers
		where trigger_schema = database() and trigger_name like 'sqltest\_refuse\_%'`)
	if err != nil {
		return fmt.Errorf("cannot lift the refusals on %s: %w", m.where(), err)
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
		if _, err = m.handle.ExecContext(ctx, "drop trigger "+backtick(trigger)); err != nil {
			return fmt.Errorf("cannot lift the refusals on %s: %w", m.where(), err)
		}
	}
	// Asked first because the drop is DDL even when there is nothing to drop.
	var quotas int
	if err = m.handle.QueryRowContext(ctx, `
		select count(*) from information_schema.tables
		where table_schema = database() and table_name = '`+mysqlQuota+`'`).Scan(&quotas); err != nil || quotas == 0 {
		return err
	}
	if _, err = m.handle.ExecContext(ctx, "drop table "+mysqlQuota); err != nil {
		return fmt.Errorf("cannot lift the refusals on %s: %w", m.where(), err)
	}
	return nil
}
