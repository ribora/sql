package sqltest

import (
	"context"
	dbSql "database/sql"
	"fmt"
	"slices"
	"strings"
)

type mysqlBaseline struct {
	inserts    map[string]string
	checksums  map[string]int64
	increments map[string]int64
	triggers   []mysqlTrigger
}

type mysqlTrigger struct {
	name      string
	table     string
	event     string
	sqlMode   string
	statement string
}

func (m *Mysql) remember(ctx context.Context) error {
	m.baseline = mysqlBaseline{inserts: map[string]string{}}
	return m.throwaway(ctx, func(conn *dbSql.Conn) error {
		if err := m.settle(ctx, conn); err != nil {
			return err
		}
		var filled []string
		for i, table := range m.tables {
			kept, err := m.rememberRows(ctx, conn, i, table)
			if err != nil {
				return err
			}
			if kept {
				filled = append(filled, table)
			}
		}
		var err error
		if m.baseline.checksums, err = m.checksums(ctx, conn); err != nil {
			return err
		}
		if m.baseline.increments, err = m.increments(ctx, conn); err != nil {
			return err
		}
		return m.rememberTriggers(ctx, conn, filled)
	})
}

func (m *Mysql) rememberRows(ctx context.Context, conn *dbSql.Conn, i int, table string) (bool, error) {
	var filled bool
	if err := conn.QueryRowContext(ctx, "select exists(select 1 from "+backtick(table)+")").Scan(&filled); err != nil || !filled {
		return false, err
	}
	columns, err := m.insertable(ctx, conn, table)
	if err != nil {
		return false, err
	}
	kept := backtick(fmt.Sprintf("sqltest_kept_%d", i))
	if _, err = conn.ExecContext(ctx, "drop table if exists "+kept); err != nil {
		return false, err
	}
	if _, err = conn.ExecContext(ctx, "create table "+kept+" as select "+columns+" from "+backtick(table)); err != nil {
		return false, err
	}
	m.baseline.inserts[table] = "insert into " + backtick(table) + " (" + columns + ") select " + columns + " from " + kept
	return true, nil
}

// One statement for every table, a row each in the order they are named.
func (m *Mysql) checksums(ctx context.Context, conn *dbSql.Conn) (map[string]int64, error) {
	quoted := make([]string, 0, len(m.tables))
	for _, table := range m.tables {
		quoted = append(quoted, backtick(table))
	}
	rows, err := conn.QueryContext(ctx, "checksum table "+strings.Join(quoted, ", "))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	checksums := map[string]int64{}
	for i := 0; rows.Next() && i < len(m.tables); i++ {
		var name string
		var checksum dbSql.NullInt64
		if err = rows.Scan(&name, &checksum); err != nil {
			return nil, err
		}
		checksums[m.tables[i]] = checksum.Int64
	}
	return checksums, rows.Err()
}

func (m *Mysql) insertable(ctx context.Context, conn *dbSql.Conn, table string) (string, error) {
	rows, err := conn.QueryContext(ctx, `
		select column_name from information_schema.columns
		where table_schema = database() and table_name = ? and generation_expression = ''
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
		quoted = append(quoted, backtick(name))
	}
	return strings.Join(quoted, ", "), rows.Err()
}

func (m *Mysql) increments(ctx context.Context, conn *dbSql.Conn) (map[string]int64, error) {
	rows, err := conn.QueryContext(ctx, `
		select table_name, auto_increment from information_schema.tables
		where table_schema = database() and auto_increment is not null`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	increments := map[string]int64{}
	for rows.Next() {
		var table string
		var next int64
		if err = rows.Scan(&table, &next); err != nil {
			return nil, err
		}
		if slices.Contains(m.tables, table) {
			increments[table] = next
		}
	}
	return increments, rows.Err()
}

func (m *Mysql) rememberTriggers(ctx context.Context, conn *dbSql.Conn, filled []string) error {
	rows, err := conn.QueryContext(ctx, `
		select trigger_name, event_object_table, event_manipulation from information_schema.triggers
		where trigger_schema = database() and event_manipulation in ('INSERT', 'DELETE')
		order by event_object_table, action_order`)
	if err != nil {
		return err
	}
	var triggers []mysqlTrigger
	for rows.Next() {
		var trigger mysqlTrigger
		if err = rows.Scan(&trigger.name, &trigger.table, &trigger.event); err != nil {
			_ = rows.Close()
			return err
		}
		if trigger.event == "DELETE" && slices.Contains(m.tables, trigger.table) || slices.Contains(filled, trigger.table) {
			triggers = append(triggers, trigger)
		}
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}

	for _, trigger := range triggers {
		var name, charset, collation, databaseCollation, created any
		if err = conn.QueryRowContext(ctx, "show create trigger "+backtick(trigger.name)).Scan(
			&name, &trigger.sqlMode, &trigger.statement, &charset, &collation, &databaseCollation, &created); err != nil {
			return err
		}
		m.baseline.triggers = append(m.baseline.triggers, trigger)
	}
	return nil
}

// DELETE, not TRUNCATE, and only where a scenario changed something: truncating,
// a trigger and a counter are DDL in MySQL, at 7 to 60 ms a statement.
func (m *Mysql) restore(ctx context.Context) error {
	return m.throwaway(ctx, func(conn *dbSql.Conn) error {
		if err := m.settle(ctx, conn); err != nil {
			return err
		}
		checksums, err := m.checksums(ctx, conn)
		if err != nil {
			return err
		}
		var changed []string
		for _, table := range m.tables {
			if checksums[table] != m.baseline.checksums[table] {
				changed = append(changed, table)
			}
		}
		present, err := m.presentTriggers(ctx, conn)
		if err != nil {
			return err
		}
		var firing, recreated []mysqlTrigger
		for _, trigger := range m.baseline.triggers {
			switch {
			case !present[trigger.name]:
				recreated = append(recreated, trigger)
			// A delete trigger fires only on rows there are to delete.
			case slices.Contains(changed, trigger.table) && (trigger.event == "INSERT" || checksums[trigger.table] != 0):
				firing = append(firing, trigger)
				recreated = append(recreated, trigger)
			}
		}
		for _, trigger := range firing {
			if _, err = conn.ExecContext(ctx, "drop trigger "+backtick(trigger.name)); err != nil {
				return err
			}
		}
		if err = m.refill(ctx, conn, changed); err != nil {
			return err
		}
		if err = m.rewind(ctx, conn); err != nil {
			return err
		}
		for _, trigger := range recreated {
			if _, err = conn.ExecContext(ctx, "set session sql_mode = "+mysqlLiteral(trigger.sqlMode)); err != nil {
				return err
			}
			if _, err = conn.ExecContext(ctx, trigger.statement); err != nil {
				return err
			}
		}
		return nil
	})
}

// information_schema otherwise answers AUTO_INCREMENT from a day-old cache.
func (m *Mysql) settle(ctx context.Context, conn *dbSql.Conn) error {
	for _, setting := range []string{
		"set session foreign_key_checks = 0",
		"set session information_schema_stats_expiry = 0",
		"set session sql_mode = concat_ws(',', nullif(@@session.sql_mode, ''), 'NO_AUTO_VALUE_ON_ZERO')",
	} {
		if _, err := conn.ExecContext(ctx, setting); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mysql) presentTriggers(ctx context.Context, conn *dbSql.Conn) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, "select trigger_name from information_schema.triggers where trigger_schema = database()")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	present := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		present[name] = true
	}
	return present, rows.Err()
}

func (m *Mysql) refill(ctx context.Context, conn *dbSql.Conn, tables []string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range tables {
		if _, err = tx.ExecContext(ctx, "delete from "+backtick(table)); err != nil {
			return err
		}
	}
	for _, table := range tables {
		if insert, kept := m.baseline.inserts[table]; kept {
			if _, err = tx.ExecContext(ctx, insert); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (m *Mysql) rewind(ctx context.Context, conn *dbSql.Conn) error {
	current, err := m.increments(ctx, conn)
	if err != nil {
		return err
	}
	for table, next := range m.baseline.increments {
		if current[table] == next {
			continue
		}
		if _, err = conn.ExecContext(ctx, fmt.Sprintf("alter table %s auto_increment = %d", backtick(table), next)); err != nil {
			return err
		}
	}
	return nil
}
