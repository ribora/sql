package sqltest

import (
	"context"
	dbSql "database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
)

func NewMysql(opts ...Option) *Mysql {
	m := &Mysql{}
	m.host, m.dialect = mysqlHost{}, m
	for _, opt := range opts {
		opt(&m.server)
	}
	return m
}

type Mysql struct {
	server
	baseline mysqlBaseline
}

func (m *Mysql) Name() string { return "mysql" }

// A schema is a database here, so sqltest's own tables sit beside the service's.
func (m *Mysql) discover(ctx context.Context) ([]string, error) {
	rows, err := m.handle.QueryContext(ctx, `
		select table_name from information_schema.tables
		where table_schema = database() and table_type = 'BASE TABLE' and table_name not like 'sqltest\_%'`)
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

type mysqlHost struct{}

// parseTime: goose reads its bookkeeping into time.Time.
var mysqlContainer = containerKind{
	name:       "ribora-harness-mysql",
	port:       53306,
	serverPort: 3306,
	noPassword: "MYSQL_ALLOW_EMPTY_PASSWORD=yes",
	dsn: func(port int, database string) string {
		return fmt.Sprintf("root@tcp(127.0.0.1:%d)/%s?parseTime=true", port, database)
	},
}

const mysqlMark = "sqltest_mark"

func parseMysql(dsn string) (*mysql.Config, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, errors.New("sqltest: the address is not a valid mysql DSN")
	}
	return cfg, nil
}

func (mysqlHost) driver() string      { return "mysql" }
func (mysqlHost) kind() containerKind { return mysqlContainer }

func (mysqlHost) databaseName(dsn string) (string, error) {
	cfg, err := parseMysql(dsn)
	if err != nil {
		return "", err
	}
	if cfg.DBName == "" {
		return "", errors.New("sqltest: the address names no database")
	}
	return cfg.DBName, nil
}

func (mysqlHost) withDatabase(dsn, name string) (string, error) {
	cfg, err := parseMysql(dsn)
	if err != nil {
		return "", err
	}
	cfg.DBName = name
	return cfg.FormatDSN(), nil
}

func (mysqlHost) address(dsn string) string {
	cfg, err := parseMysql(dsn)
	if err != nil {
		return "the configured address"
	}
	return cfg.Addr + "/" + cfg.DBName
}

func (h mysqlHost) maintenance(dsn string) (string, error) {
	return h.withDatabase(dsn, "")
}

func (mysqlHost) exists(ctx context.Context, admin *dbSql.DB, name string) (bool, error) {
	var count int
	err := admin.QueryRowContext(ctx, "select count(*) from information_schema.schemata where schema_name = ?", name).Scan(&count)
	return count > 0, err
}

// MySQL keeps no comment on a database, so the mark is a table of its own in it.
func (mysqlHost) create(ctx context.Context, admin *dbSql.DB, name, mark string) error {
	if _, err := admin.ExecContext(ctx, "create database "+backtick(name)); err != nil {
		return fmt.Errorf("cannot create database %q: %w", name, err)
	}
	if _, err := admin.ExecContext(ctx, "create table "+backtick(name)+"."+mysqlMark+" (id int) comment "+mysqlLiteral(mark)); err != nil {
		return fmt.Errorf("cannot mark database %q: %w", name, err)
	}
	return nil
}

func (mysqlHost) marks(ctx context.Context, admin *dbSql.DB) (map[string]string, error) {
	rows, err := admin.QueryContext(ctx, `
		select s.schema_name, coalesce(t.table_comment, '')
		from information_schema.schemata s
		left join information_schema.tables t on t.table_schema = s.schema_name and t.table_name = '`+mysqlMark+`'`)
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

func (mysqlHost) connected(ctx context.Context, admin *dbSql.DB, name string) (bool, error) {
	var count int
	err := admin.QueryRowContext(ctx, "select count(*) from information_schema.processlist where db = ?", name).Scan(&count)
	return count > 0, err
}

// There is no `with (force)`: a transaction left open holds a metadata lock
// that the drop would wait on for good.
func (mysqlHost) drop(ctx context.Context, admin *dbSql.DB, name string) error {
	rows, err := admin.QueryContext(ctx, "select id from information_schema.processlist where db = ?", name)
	if err != nil {
		return err
	}
	var ids []uint64
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		// Best effort: the connection may have gone on its own.
		_, _ = admin.ExecContext(ctx, "kill "+strconv.FormatUint(id, 10))
	}
	_, err = admin.ExecContext(ctx, "drop database if exists "+backtick(name))
	return err
}

func backtick(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// A backslash escapes in a MySQL literal, unlike in the standard one.
func mysqlLiteral(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(value) + "'"
}
