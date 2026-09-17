package err_broken_model

import "gitlab.com/gorib/sql/cmd/store/testdata/cases/err_broken_model/model"

// The model package does not compile, so Title has no type to line up with. Type
// errors are ignored wholesale at load, so this one has to be caught at the field.
type StoredDoc struct {
	_ *model.Doc

	ID    int    `db:"id"`
	Title string `db:"title"`
}
