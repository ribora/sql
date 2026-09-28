package err_bad_sort

type StoredCode struct {
	_ any `store:"nonew,nounwrap,sort:code upwards"`

	Code  string `db:"code"`
	Title string `db:"title"`
}
