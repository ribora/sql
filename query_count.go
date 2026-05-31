package sql

import (
	"gitlab.com/gorib/criteria"
)

type CountQuery interface {
	CountBuilder
	From(main fromMain, opts ...fromOptions) CountQuery
	Where(query criteria.Expression) CountQuery
	Returning(fields ...string) CountQuery
}

func Count() CountQuery {
	return &countQuery{q: Select("count(*) cnt").Sort("cnt")}
}

type countQuery struct {
	CountBuilder
	q SelectQuery
}

func (c *countQuery) Build(driver Driver, opts ...BuildOption) (string, []any, error) {
	return c.q.Build(driver, opts...)
}

func (c *countQuery) From(main fromMain, opts ...fromOptions) CountQuery {
	c.q.From(main, opts...)
	return c
}

func (c *countQuery) Where(query criteria.Expression) CountQuery {
	c.q.Where(query)
	return c
}

func (c *countQuery) Returning(fields ...string) CountQuery {
	c.q.Returning(fields...)
	return c
}
