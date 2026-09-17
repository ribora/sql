package basic

type User struct {
	ID    int
	Email string
	Name  string
	Notes []string
}

type StoredUser struct {
	_ *User

	// readonly stays out of Updates and Inserts, and store:"Name" renames the
	// model field this column comes from.
	ID       int    `db:"id" store:",readonly"`
	Email    string `db:"email"`
	FullName string `db:"full_name" store:"Name"`

	// Declined, db tag or not: a model field this struct does not carry, a column
	// it opts out of, and one the model knows nothing about.
	Notes    []string `store:"-"`
	Internal string   `db:"internal" store:"-"`
	Scratch  string   `store:"-"`
}
