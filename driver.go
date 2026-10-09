package sql

type Driver interface {
	Field(field string, operation string, value any, params *[]any) (string, *[]any, error)
	Returning() bool
	Placeholder(pos int) string
	Conflict(conflict string, updates []string) string
	UpdateAsJoin() bool
	Dialect() string
}
