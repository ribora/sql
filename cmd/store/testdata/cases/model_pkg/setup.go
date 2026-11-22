package model_pkg

import "github.com/ribora/sql/cmd/store/testdata/cases/model_pkg/model"

// The model lives elsewhere, so the generated file has to carry an import line
// for it — built from the qualifier the field is written with.
type StoredOrder struct {
	_ *model.Order

	ID    int `db:"id"`
	Total int `db:"total"`
}
