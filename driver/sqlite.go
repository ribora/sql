package driver

import (
	"strings"
)

func NewSqlite() *driver {
	return NewDriver(&sqliteDriver{}, map[string][]string{
		"=":       {"=", "eq", "in"},
		"!=":      {"!=", "ne", "neq", "nin"},
		">":       {">", "gt"},
		">=":      {">=", "gte", "ge"},
		"<":       {"<", "lt"},
		"<=":      {"<=", "lte", "le"},
		"between": {"between"},
		"like":    {"like", "ilike"},
	})
}

type sqliteDriver struct{}

func (p sqliteDriver) Returning() bool {
	return true
}

func (p sqliteDriver) Placeholder(_ int) string {
	return "?"
}

func (p sqliteDriver) Conflict(conflict string, updates []string) string {
	if len(updates) > 0 {
		parts := make([]string, 0, len(updates))
		for _, u := range updates {
			parts = append(parts, u+"="+"excluded."+u)
		}
		return "on conflict (" + conflict + ") do update set " + strings.Join(parts, ", ")
	}
	return "on conflict (" + conflict + ") do nothing"
}

func (p sqliteDriver) UpdateAsJoin() bool {
	return false
}
