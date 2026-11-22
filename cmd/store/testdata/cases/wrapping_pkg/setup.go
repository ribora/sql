package wrapping_pkg

import "github.com/ribora/sql/cmd/store/testdata/cases/wrapping_pkg/wrapsql"

type Payload struct {
	Kind string
}

type Doc struct {
	ID   int
	Tags []string
	Meta Payload
}

// The csv field would reach for github.com/ribora/sql by default, and the wrapped
// field names another package. One import line has to come out, and it has to be
// the one the author wrote — two aliases for one name do not build, and the
// default would be a package this file never mentions. csv is declared after the
// wrapped field on purpose: whichever is read last must not decide.
type StoredDoc struct {
	_ *Doc

	ID   int                    `db:"id"`
	Meta *wrapsql.Json[Payload] `db:"meta"`
	Tags *string                `db:"tags" store:"Tags,convert:csv"`
}
