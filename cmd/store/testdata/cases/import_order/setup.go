package import_order

type Doc struct {
	ID   int
	Tags []string
}

// The convert:csv field needs an import registered on the lexem that the `_`
// field creates. Read in source order, this dereferenced a nil pointer, and the
// documented workaround was "put `_` first".
type StoredDoc struct {
	Tags *string `db:"tags" store:"Tags,convert:csv"`

	_ *Doc

	ID int `db:"id"`
}
