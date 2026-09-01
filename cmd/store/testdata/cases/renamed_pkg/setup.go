package renamed_pkg

import "gitlab.com/gorib/sql/cmd/store/testdata/cases/renamed_pkg/pkgdir"

// The qualifier is the package clause, not the last element of the path. Reading
// it off the path put nothing under "payload", and the generated file carried an
// import line with an empty path.
type StoredDoc struct {
	_ *payload.Doc

	ID int `db:"id"`
}
