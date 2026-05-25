package driver

import (
	"strings"
)

func NewMysql() *driver {
	return NewDriver(&mysqlDriver{}, map[string][]string{
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

type mysqlDriver struct{}

func (m mysqlDriver) Returning() bool {
	return false
}

func (m mysqlDriver) Placeholder(_ int) string {
	return "?"
}

func (m mysqlDriver) Conflict(conflict string, updates []string) string {
	var sb strings.Builder
	sb.WriteString("as excluded on duplicate key update ")
	if len(updates) > 0 {
		parts := make([]string, 0, len(updates))
		for _, u := range updates {
			parts = append(parts, u+"=excluded."+u)
		}
		sb.WriteString(strings.Join(parts, ","))
	} else {
		key := strings.Split(conflict, ",")[0]
		sb.WriteString(key + "=excluded." + key)
	}
	return sb.String()
}

func (m mysqlDriver) UpdateAsJoin() bool {
	return true
}
