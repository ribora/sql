package err_untagged_field

type Doc struct {
	ID    int
	Cache string
}

// A field with no db tag used to be dropped in silence, and the column went
// missing from Scan() and Inserts() with nothing to read about it.
type StoredDoc struct {
	_ *Doc

	ID    int `db:"id"`
	Cache string
}
