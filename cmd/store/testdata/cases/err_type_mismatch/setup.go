package err_type_mismatch

type Doc struct {
	ID    int
	Count int
}

// The column is a string and the model field is an int. Without type checking
// this reached the compiler as `cannot use m.Count` in the generated file.
type StoredDoc struct {
	_ *Doc

	ID    int    `db:"id"`
	Count string `db:"count"`
}
