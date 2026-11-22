package major_version

import "github.com/ribora/sql/cmd/store/testdata/cases/major_version/payload/v2"

// The last element of the path is the version. Reading only that put the import
// under "v2", nothing under "payload", and the generated file carried an import
// line with an empty path. Resolved from the path alone — no go tool involved.
type StoredDoc struct {
	_ *payload.Doc

	ID int `db:"id"`
}
