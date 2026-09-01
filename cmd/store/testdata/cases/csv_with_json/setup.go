package csv_with_json

import "gitlab.com/gorib/sql"

type Payload struct {
	Kind string
}

type Doc struct {
	ID   int
	Tags []string
	Meta Payload
}

// Both conversions reach for the same package, and it has to arrive as one import
// line: two aliases for one path do not build.
type StoredDoc struct {
	_ *Doc

	ID   int                `db:"id"`
	Tags *string            `db:"tags" store:"Tags,convert:csv"`
	Meta *sql.Json[Payload] `db:"meta"`
}
