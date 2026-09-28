package sorted

type Guard struct {
	Facility string
	Name     string
	Pin      string
}

type StoredGuard struct {
	_ *Guard `store:"sort:facility|name desc nulls last"`

	Facility string `db:"facility"`
	Name     string `db:"name"`
	Pin      string `db:"pin"`
}

type Details struct {
	Address string
}

// Sorted by a column the struct declines: it is still in the table.
type StoredDetails struct {
	_ *Details `store:"sort:id"`

	Id      int    `db:"id" store:"-"`
	Address string `db:"address"`
}

// A column named key, and so a field named Key: the generated method must not
// take that name.
type StoredEnvironment struct {
	_ any `store:"nonew,nounwrap,sort:key"`

	Key   string `db:"key"`
	Value string `db:"value"`
}
