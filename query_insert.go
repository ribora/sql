package sql

import (
	"errors"
	"fmt"
	"strings"

	"gitlab.com/gorib/sql/types"
)

type InsertQuery interface {
	Builder
	Into(table fromTable) InsertQuery
	Values(values ...any) InsertQuery
	From(main fromMain, opts ...fromOptions) InsertQuery
	Conflict(conflict string, excluded ...string) InsertQuery
	Returning(fields ...string) InsertQuery
}

func Insert(fields ...string) *insertQuery {
	return &insertQuery{
		fields: fields,
	}
}

type insertQuery struct {
	table       *from
	fields      []string
	from        []*from
	values      [][]any
	conflictKey string
	conflicts   []string
	returning   []string
	fail        error
}

func (q *insertQuery) Into(table fromTable) InsertQuery {
	q.table = table.from()
	return q
}

func (q *insertQuery) From(main fromMain, opts ...fromOptions) InsertQuery {
	main.from().join = "select"
	q.from = []*from{main.from()}
	for _, opt := range opts {
		q.from = append(q.from, opt())
	}
	return q
}

func (q *insertQuery) Values(values ...any) InsertQuery {
	q.values = append(q.values, values)
	return q
}

func (q *insertQuery) Conflict(conflict string, excluded ...string) InsertQuery {
	keys := strings.Split(conflict, ",")
	for idx, key := range keys {
		keys[idx] = q.filter(key)
	}
	q.conflictKey = strings.Join(keys, ",")
	q.conflicts = q.conflicts[:0]
	for _, exclude := range excluded {
		q.conflicts = append(q.conflicts, q.filter(exclude))
	}
	return q
}

func (q *insertQuery) Returning(fields ...string) InsertQuery {
	q.returning = q.returning[:0]
	for _, field := range fields {
		q.returning = append(q.returning, q.filter(field))
	}
	return q
}

func (q *insertQuery) filter(field any) string {
	v, err := filter(field)
	if err != nil {
		q.fail = err
		return ""
	}
	return v
}

func (q *insertQuery) Build(d Driver) (string, []any, error) {
	var params []any
	s, err := q.build(d, &params)
	if err != nil {
		return "", nil, err
	}
	return s, params, nil
}

func (q *insertQuery) build(d Driver, p *[]any) (string, error) {
	if q.fail != nil {
		return "", q.fail
	}
	var sb strings.Builder
	sb.WriteString("insert into ")
	if q.table == nil {
		return "", errors.New("no table for insert query")
	}
	sb.WriteString(q.table.table)
	if q.table.alias != "" {
		sb.WriteString(" " + q.table.alias)
	}
	if len(q.fields) == 0 {
		if len(q.returning) > 0 {
			q.fields = q.returning
		}
	}
	if len(q.fields) > 0 {
		sb.WriteString(" (")
		sb.WriteString(strings.Join(q.fields, ","))
		sb.WriteString(")")
	}
	if len(q.from) > 0 && len(q.values) > 0 {
		return "", fmt.Errorf("cannot insert both values and subqueries")
	}
	if len(q.from) == 0 && len(q.values) == 0 {
		return "", fmt.Errorf("no values found in insert query")
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

	var parts []string
	for _, values := range q.values {
		var v []string
		for _, value := range values {
			if vv, ok := value.(types.SqlExpr); ok {
				v = append(v, vv.String())
			} else {
				*p = append(*p, value)
				v = append(v, d.Placeholder(len(*p)))
			}
		}
		parts = append(parts, strings.Join(v, ","))
	}
	if len(parts) > 0 {
		sb.WriteString(" values (")
		sb.WriteString(strings.Join(parts, "), ("))
		sb.WriteString(")")
	}

	if q.conflictKey != "" {
		sb.WriteString(" ")
		sb.WriteString(d.Conflict(q.conflictKey, q.conflicts))
	}

	if len(q.returning) > 0 {
		sb.WriteString(" returning ")
		sb.WriteString(strings.Join(q.returning, ","))
	}

	return sb.String(), nil
}
