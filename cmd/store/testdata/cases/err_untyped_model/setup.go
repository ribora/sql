package err_untyped_model

// `_ any` names no model, so both constructors would map fields off a type that
// has none. It is allowed only when neither is generated.
type StoredDoc struct {
	_ any

	ID int `db:"id"`
}
