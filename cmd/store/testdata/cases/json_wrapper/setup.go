package json_wrapper

import "github.com/ribora/sql"

type Payload struct {
	Kind string
}

type Doc struct {
	ID   int
	Meta Payload
}

// A wrapped field is one column: the generated file imports the sql package
// under a name nothing can collide with.
type StoredDoc struct {
	_ *Doc

	ID   int                `db:"id"`
	Meta *sql.Json[Payload] `db:"meta"`
}
