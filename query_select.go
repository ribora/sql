package sql

import (
	"fmt"
	"strconv"
	"strings"

	"gitlab.com/gorib/criteria"
)

type SelectQuery interface {
	Builder
	From(main fromMain, opts ...fromOptions) SelectQuery
	Where(query criteria.Expression) SelectQuery
	Page(page int, count int) SelectQuery
	Sort(sort ...string) SelectQuery
	Window(window string) SelectQuery
	Returning(fields ...string) SelectQuery
	Group(fields ...string) SelectQuery
}

func Select(fields ...string) *selectQuery {
	return &selectQuery{
		fields: fields,
	}
}

type selectQuery struct {
	fields    []string
	from      []*from
	where     criteria.Expression
	sort      []string
	group     []string
	returning []string
	window    string
	offset    uint
	limit     uint
	fail      error
}

func (q *selectQuery) From(main fromMain, opts ...fromOptions) SelectQuery {
	q.from = []*from{main.from()}
	for _, opt := range opts {
		q.from = append(q.from, opt())
	}
	return q
}

func (q *selectQuery) Page(page int, count int) SelectQuery {
	if page <= 0 {
		page = 0
	}
	if count < 0 {
		count = 1
	}
	q.limit = uint(count)
	q.offset = uint(page) * uint(count)
	return q
}

func (q *selectQuery) Sort(sort ...string) SelectQuery {
	q.sort = q.sort[:0]
	for _, s := range sort {
		q.sort = append(q.sort, q.filter(s))
	}
	return q
}

func (q *selectQuery) Returning(fields ...string) SelectQuery {
	q.returning = q.returning[:0]
	for _, field := range fields {
		q.returning = append(q.returning, q.filter(field))
	}
	return q
}

func (q *selectQuery) Where(e criteria.Expression) SelectQuery {
	q.where = e
	return q
}

func (q *selectQuery) Window(window string) SelectQuery {
	q.window = window
	return q
}

func (q *selectQuery) Group(fields ...string) SelectQuery {
	q.group = q.group[:0]
	for _, field := range fields {
		q.group = append(q.group, q.filter(field))
	}
	return q
}

func (q *selectQuery) filter(field any) string {
	v, err := filter(field)
	if err != nil {
		q.fail = err
		return ""
	}
	return v
}

func (q *selectQuery) Build(d Driver) (string, []any, error) {
	var params []any
	s, err := q.build(d, &params)
	if err != nil {
		return "", nil, err
	}
	return s, params, nil
}

func (q *selectQuery) build(d Driver, p *[]any) (string, error) {
	if q.fail != nil {
		return "", q.fail
	}
	var sb strings.Builder
	sb.WriteString("select ")
	if len(q.fields) == 0 {
		if len(q.returning) > 0 {
			q.fields = q.returning
		} else {
			q.fields = []string{"*"}
		}
	}
	sb.WriteString(strings.Join(q.fields, ", "))
	if len(q.from) == 0 {
		return "", fmt.Errorf("no table specified for select")
	}
	q.from[0].join = "from"
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

	if q.window != "" {
		sb.WriteString(" window ")
		sb.WriteString(q.window)
	}

	if len(q.group) > 0 {
		sb.WriteString(" group by ")
		sb.WriteString(strings.Join(q.group, ","))
	}

	if len(q.sort) == 0 {
		q.sort = []string{"id asc"}
	}
	sb.WriteString(" order by ")
	sb.WriteString(strings.Join(q.sort, ","))

	if q.offset != 0 {
		sb.WriteString(" offset ")
		sb.WriteString(strconv.Itoa(int(q.offset)))
	}
	if q.limit != 0 {
		sb.WriteString(" limit ")
		sb.WriteString(strconv.Itoa(int(q.limit)))
	}

	return sb.String(), nil
}
