package grouped

type A struct{ ID int }

type B struct{ ID int }

// A grouped declaration used to generate for the first struct and drop the rest
// without a word, because only Specs[0] was read.
type (
	StoredA struct {
		_  *A
		ID int `db:"id"`
	}

	StoredB struct {
		_  *B
		ID int `db:"id"`
	}
)
