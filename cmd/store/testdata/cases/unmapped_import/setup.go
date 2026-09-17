package unmapped_import

import "gitlab.com/gorib/sql/cmd/store/testdata/cases/unmapped_import/model"

// A written-in field is spelled in this package's terms, so the import its type
// needs arrives with it — time here, while model is already in scope.
type StoredDoc struct {
	_ *model.Doc

	ID int `db:"id"`
}
