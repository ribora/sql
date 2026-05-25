package sql

type fromMain interface {
	As(alias string) *from
	from() *from
}

type fromTable interface {
	fromMain
	_table()
}

type fromQuery interface {
	fromMain
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
	return func() *from {
		j := f.from()
		j.condition = condition
		j.join = "left join"
		return j
	}
}

func RightJoin(f fromMain, condition string) fromOptions {
	return func() *from {
		j := f.from()
		j.condition = condition
		j.join = "right join"
		return j
	}
}

func Join(f fromMain, condition string) fromOptions {
	return func() *from {
		j := f.from()
		j.condition = condition
		j.join = "join"
		return j
	}
}

type from struct {
	fromTable
	fromQuery
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
