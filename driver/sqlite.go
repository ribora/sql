package driver

import (
	"strings"

	"gitlab.com/gorib/sql/op"
)

func NewSqlite() *sqliteDriver {
	return &sqliteDriver{
		driver: newDriver(map[string][]op.Operation{
			"=":       {"=", op.Eq, op.In},
			"!=":      {"!=", op.Ne, op.Neq, op.Nin},
			">":       {">", op.Gt},
			">=":      {">=", op.Gte, op.Ge},
			"<":       {"<", op.Lt},
			"<=":      {"<=", op.Lte, op.Le},
			"between": {op.Between},
			// No case-insensitive match: ILike collapses to Like here.
			"like": {op.Like, op.ILike},
		}),
	}
}

type sqliteDriver struct {
	*driver
}

func (p sqliteDriver) Field(field string, operation string, value any, params *[]any) (string, *[]any, error) {
	return p.field(p, field, operation, value, params)
}

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

func (p sqliteDriver) Dialect() string {
	return "sqlite"
}
