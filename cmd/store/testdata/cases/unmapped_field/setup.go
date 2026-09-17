package unmapped_field

type Doc struct {
	ID    int
	Lines []string
}

// Lines is on the model and nowhere here, so Unwrap() would leave it zero. It is
// written in without tags, and the answer is the author's to give.
type StoredDoc struct {
	_ *Doc

	ID int `db:"id"`
}
