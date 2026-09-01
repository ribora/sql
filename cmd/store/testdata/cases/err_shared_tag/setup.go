package err_shared_tag

type Doc struct {
	ID    int
	Left  int
	Right int
}

// Two names on one tag would map two fields to one column. The first name was
// taken and the second dropped in silence.
type StoredDoc struct {
	_ *Doc

	ID          int `db:"id"`
	Left, Right int `db:"side"`
}
