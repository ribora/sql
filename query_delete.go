package sql

import (
	"fmt"
	"strings"

	"gitlab.com/gorib/criteria"
)

type DeleteQuery interface {
	DeleteBuilder
	From(main fromTable, opts ...fromOptions) DeleteQuery
	Where(query criteria.Expression) DeleteQuery
}

func Delete() *deleteQuery {
	return &deleteQuery{}
}

type deleteQuery struct {
	DeleteBuilder
	from  []*from
	where criteria.Expression
}

func (q *deleteQuery) From(main fromTable, opts ...fromOptions) DeleteQuery {
	main.from().join = "from"
	q.from = []*from{main.from()}
	for _, opt := range opts {
		q.from = append(q.from, opt())
	}
	return q
}

func (q *deleteQuery) Where(e criteria.Expression) DeleteQuery {
	q.where = e
	return q
}

func (q *deleteQuery) Build(d Driver) (string, []any, error) {
	var params []any
	s, err := q.build(d, &params)
	if err != nil {
		return "", nil, err
	}
	return s, params, nil
}

func (q *deleteQuery) build(d Driver, p *[]any) (string, error) {
	var sb strings.Builder
	sb.WriteString("delete ")
	if len(q.from) == 0 {
		return "", fmt.Errorf("no table for delete")
	}
	for _, f := range q.from {
		sb.WriteString(" ")
		sb.WriteString(f.join)
		sb.WriteString(" ")
		if qq, ok := f.query.(*selectQuery); ok {
			s, err := qq.build(d, p)
			if err != nil {
				return "", err
			}
			sb.WriteString("(")
			sb.WriteString(s)
			sb.WriteString(") " + f.alias)
		} else if f.table != "" {
			sb.WriteString(f.table)
			if f.alias != "" {
				sb.WriteString(" " + f.alias)
			}
		}
		if f.condition != "" {
			sb.WriteString(" on ")
			sb.WriteString(f.condition)
		}
	}

	if q.where != nil {
		w, err := newExpressionBuilder(q.where, d).build(p)
		if err != nil {
			return "", err
		}
		if w != "" {
			sb.WriteString(" where ")
			sb.WriteString(w)
		}
	}

	return sb.String(), nil
}
