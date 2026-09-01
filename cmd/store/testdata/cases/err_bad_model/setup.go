package err_bad_model

type Doc struct {
	ID int
}

// The `_` field has to name the model type. Anything else produced a lexem with
// an empty Model and a generated file referring to a type called "".
type StoredDoc struct {
	_ map[string]Doc

	ID int `db:"id"`
}
