package other_types

// A non-struct declaration in the same file crashed the parser: the type of a
// TypeSpec was asserted to be a struct without asking.
type Status int

type Base struct{ ID int }

type Alias = Base

type Item struct {
	ID     int
	Status Status
}

type StoredItem struct {
	_ *Item

	ID     int    `db:"id"`
	Status Status `db:"status"`
}
