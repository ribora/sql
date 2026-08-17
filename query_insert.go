package sql

import (
	"errors"
	"fmt"
	"strings"
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
	q.conflictKey = conflict
	q.conflicts = q.conflicts[:0]
	for _, exclude := range excluded {
		q.conflicts = append(q.conflicts, exclude)
	}
	return q
}

func (q *insertQuery) Returning(fields ...string) InsertQuery {
	q.returning = q.returning[:0]
	for _, field := range fields {
		q.returning = append(q.returning, field)
	}
	return q
}

func (q *insertQuery) Build(d Driver, opts ...BuildOption) (string, []any, error) {
	cfg, err := newBuilderCfg(opts)
	if err != nil {
		return "", nil, err
	}
	var params []any
	s, err := q.build(d, cfg, &params)
	if err != nil {
		return "", nil, err
	}
	return s, params, nil
}

func (q *insertQuery) build(d Driver, cfg *builderCfg, p *[]any) (string, error) {
	filtered := func(field any) (string, error) {
		if b, ok := field.(builder); ok {
			return b.build(d, cfg, p)
		}
		return filter(field, cfg.mappings)
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
	returning := make([]string, 0, len(q.returning))
	for _, field := range q.returning {
		f, err := filtered(field)
		if err != nil {
			return "", err
		}
		returning = append(returning, f)
	}
	fields := append([]string{}, q.fields...)
	if len(fields) == 0 {
		if len(returning) > 0 {
			fields = returning
		}
	}
	if len(fields) > 0 {
		sb.WriteString(" (")
		sb.WriteString(strings.Join(fields, ","))
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
			s, err := qq.build(d, cfg, p)
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
			if b, ok := value.(builder); ok {
				sub, err := b.build(d, cfg, p)
				if err != nil {
					return "", err
				}
				v = append(v, sub)
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
		keys := strings.Split(q.conflictKey, ",")
		for idx, key := range keys {
			f, err := filtered(key)
			if err != nil {
				return "", err
			}
			keys[idx] = f
		}
		conflicts := make([]string, 0, len(q.conflicts))
		for _, key := range q.conflicts {
			f, err := filtered(key)
			if err != nil {
				return "", err
			}
			conflicts = append(conflicts, f)
		}
		sb.WriteString(d.Conflict(strings.Join(keys, ","), q.conflicts))
	}

	if len(returning) > 0 {
		sb.WriteString(" returning ")
		sb.WriteString(strings.Join(returning, ","))
	}

	return sb.String(), nil
}
