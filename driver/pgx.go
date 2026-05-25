package driver

import (
	"strconv"
	"strings"
)

func NewPostgres() *driver {
	return NewDriver(&pgxDriver{}, map[string][]string{
		"=":       {"=", "eq", "in"},
		"!=":      {"!=", "ne", "neq", "nin"},
		">":       {">", "gt"},
		">=":      {">=", "gte", "ge"},
		"<":       {"<", "lt"},
		"<=":      {"<=", "lte", "le"},
		"between": {"between"},
		"like":    {"like"},
		"ilike":   {"ilike"},
		"@>":      {"@>", "json_in"},
		"?":       {"?", "json_eq"},
	})
}

type pgxDriver struct{}

func (p pgxDriver) Returning() bool {
	return true
}

func (p pgxDriver) Placeholder(pos int) string {
	return "$" + strconv.Itoa(pos)
}

func (p pgxDriver) Conflict(conflict string, updates []string) string {
	if len(updates) > 0 {
		parts := make([]string, 0, len(updates))
		for _, u := range updates {
			parts = append(parts, u+"="+"excluded."+u)
		}
		return "on conflict (" + conflict + ") do update set " + strings.Join(parts, ", ")
	}
	return "on conflict (" + conflict + ") do nothing"
}

func (p pgxDriver) UpdateAsJoin() bool {
	return false
}
