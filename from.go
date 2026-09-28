package sql

type fromMain interface {
	As(alias string) *from
	from() *from
}

type fromSource interface {
	source() ([]*from, *View)
}

type fromTarget interface {
	target() *from
}

type fromTable interface {
	fromMain
	fromSource
	fromTarget
	_table()
}

type fromQuery interface {
	fromMain
	fromSource
	_query()
}

type fromOptions func() *from

func Table(table string) fromTable {
	return &from{
		table: table,
	}
}

func Query(q SelectQuery, alias string) fromQuery {
	return &from{
		query: q,
		alias: alias,
	}
}

func LeftJoin(f fromMain, condition string) fromOptions {
	return joined(f, "left join", condition)
}

func RightJoin(f fromMain, condition string) fromOptions {
	return joined(f, "right join", condition)
}

func Join(f fromMain, condition string) fromOptions {
	return joined(f, "join", condition)
}

func joined(f fromMain, kind string, condition string) fromOptions {
	return func() *from {
		j := *f.from()
		j.condition = condition
		j.join = kind
		return &j
	}
}

type from struct {
	table     string
	query     SelectQuery
	condition string
	join      string
	alias     string
}

func (f *from) As(alias string) *from {
	if f == nil {
		return nil
	}
	n := *f
	n.alias = alias
	return &n
}

func (f *from) from() *from {
	return f
}

func (f *from) source() ([]*from, *View) {
	c := *f
	return []*from{&c}, nil
}

func (f *from) target() *from {
	c := *f
	return &c
}

func (f *from) _table() {}

func (f *from) _query() {}
