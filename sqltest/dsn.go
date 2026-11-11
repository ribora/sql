package sqltest

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgx and not net/url: net/url parses the keyword form (`host=... password=...`)
// without complaint and puts the whole string in Path, password included.
func parse(dsn string) (*pgconn.Config, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		// Deliberately not wrapped: the message pgx produces quotes the DSN.
		return nil, errors.New("sqltest: the address is not a valid postgres DSN")
	}
	return cfg, nil
}

func databaseName(dsn string) (string, error) {
	cfg, err := parse(dsn)
	if err != nil {
		return "", err
	}
	if cfg.Database == "" {
		return "", errors.New("sqltest: the address names no database")
	}
	return cfg.Database, nil
}

// Host, port and database only: errors reach logs and CI output.
func address(dsn string) string {
	cfg, err := parse(dsn)
	if err != nil {
		return "the configured address"
	}
	return fmt.Sprintf("%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)
}

// Edited in place, not rebuilt from the parsed config: rebuilding drops sslmode,
// timeouts and search_path.
func withDatabase(dsn, name string) (string, error) {
	if isURL(dsn) {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", errors.New("sqltest: the address is not a valid postgres DSN")
		}
		parsed.Path = "/" + name
		return parsed.String(), nil
	}
	if _, err := parse(dsn); err != nil {
		return "", err
	}
	return replaceKeyword(dsn, "dbname", name), nil
}

func isURL(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

func replaceKeyword(dsn, key, value string) string {
	kept := make([]string, 0, 8)
	for _, field := range splitKeywords(dsn) {
		if name, _, found := strings.Cut(field, "="); !found || strings.TrimSpace(name) != key {
			kept = append(kept, field)
		}
	}
	return strings.Join(append(kept, key+"="+quoteKeyword(value)), " ")
}

func splitKeywords(dsn string) []string {
	var fields []string
	var current strings.Builder
	var quoted bool
	for _, r := range dsn {
		switch {
		case r == '\'':
			quoted = !quoted
			current.WriteRune(r)
		case !quoted && (r == ' ' || r == '\t'):
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}

// information_schema returns the real case, and unquoted `truncate userProfile`
// is folded to lower case and finds nothing.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// libpq quoting: single quotes, backslash-escaped inside.
func quoteKeyword(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t'\\") {
		return value
	}
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}
