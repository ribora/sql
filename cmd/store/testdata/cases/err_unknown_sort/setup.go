package err_unknown_sort

type StoredCode struct {
	_ any `store:"nonew,nounwrap,sort:code"`

	Code  string `db:"channel_code"`
	Title string `db:"title"`
}
