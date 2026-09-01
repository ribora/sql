package err_embedded_tag

type Audit struct {
	CreatedAt string
}

type Doc struct {
	ID int
	Audit
}

// An embedded field has no name to map, so the db tag on it went nowhere — and
// reading it crashed on an empty Names slice.
type StoredDoc struct {
	_ *Doc

	ID    int `db:"id"`
	Audit `db:"audit"`
}
