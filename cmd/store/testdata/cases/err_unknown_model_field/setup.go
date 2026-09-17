package err_unknown_model_field

type Doc struct {
	ID int
}

// store:"Title" names a model field that does not exist. Without type checking
// this reached the compiler as `m.Title undefined` in the generated file.
type StoredDoc struct {
	_ *Doc

	ID    int    `db:"id"`
	Title string `db:"title" store:"Title"`
}
