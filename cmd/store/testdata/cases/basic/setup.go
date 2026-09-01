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

	// Neither of these is a column: one opts out, the other has no db tag.
	Internal string `db:"internal" store:"-"`
	Scratch  string
}
