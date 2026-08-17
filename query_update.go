package sql

import (
	"errors"
	"strings"

	"gitlab.com/gorib/criteria"
)

type UpdateQuery interface {
	Builder
	Table(table fromTable) UpdateQuery
	From(opts ...fromOptions) UpdateQuery
	Where(query criteria.Expression) UpdateQuery
	Returning(fields ...string) UpdateQuery
}

func Update(values map[string]any) *updateQuery {
	return &updateQuery{
		values: values,
	}
}

type updateQuery struct {
	values    map[string]any
	table     *from
	from      []*from
	where     criteria.Expression
	returning []string
}

func (q *updateQuery) Table(main fromTable) UpdateQuery {
	q.table = main.from()
	return q
}

func (q *updateQuery) From(opts ...fromOptions) UpdateQuery {
	for _, opt := range opts {
		q.from = append(q.from, opt())
	}
	return q
}

func (q *updateQuery) Where(e criteria.Expression) UpdateQuery {
	q.where = e
	return q
}

func (q *updateQuery) Returning(fields ...string) UpdateQuery {
	q.returning = append(q.returning[:0], fields...)
	return q
}

func (q *updateQuery) Build(d Driver, opts ...BuildOption) (string, []any, error) {
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

func (q *updateQuery) build(d Driver, cfg *builderCfg, p *[]any) (string, error) {
	if q.table == nil {
		return "", errors.New("table is required")
	}
	returning, err := filterAll(q.returning, cfg.mappings)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("update ")
	sb.WriteString(q.table.table)
	if q.table.alias != "" {
		sb.WriteString(" " + q.table.alias)
	}

	if d.UpdateAsJoin() {
		s, _, err := q.fromJoin(d, cfg, p)
		if err != nil {
			return "", err
		}
		sb.WriteString(" ")
		sb.WriteString(s)
	}

	sb.WriteString(" set ")

	var parts []string
	for field, value := range q.values {
		if b, ok := value.(builder); ok {
			sub, err := b.build(d, cfg, p)
			if err != nil {
				return "", err
			}
			parts = append(parts, field+"="+sub)
		} else {
			*p = append(*p, value)
			parts = append(parts, field+"="+d.Placeholder(len(*p)))
		}
	}
	sb.WriteString(strings.Join(parts, ", "))

	var on string
	if !d.UpdateAsJoin() {
		if len(q.from) > 0 {
			q.from[0].join = "from"
		}
		var s string
		var err error
		s, on, err = q.fromJoin(d, cfg, p)
		if err != nil {
			return "", err
		}
		sb.WriteString(" ")
		sb.WriteString(s)
	}

	if q.where != nil || on != "" {
		var wh []string
		if on != "" {
			wh = append(wh, on)
		}
		w, err := newExpressionBuilder(q.where, d, cfg).build(p)
		if err != nil {
			return "", err
		}
		if w != "" || on != "" {
			wh = append(wh, w)
		}
		if len(wh) > 0 {
			sb.WriteString(" where (")
			sb.WriteString(strings.Join(wh, ") and ("))
			sb.WriteString(")")
		}
	}

	if len(returning) > 0 {
		sb.WriteString(" returning ")
		sb.WriteString(strings.Join(returning, ","))
	}

	return sb.String(), nil
}

func (q *updateQuery) fromJoin(d Driver, cfg *builderCfg, p *[]any) (string, string, error) {
	var sb strings.Builder
	var on string
	for idx, f := range q.from {
		sb.WriteString(f.join)
		sb.WriteString(" ")
		if qq, ok := f.query.(*selectQuery); ok {
			s, err := qq.build(d, cfg, p)
			if err != nil {
				return "", "", err
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
			if !d.UpdateAsJoin() && idx == 0 {
				on += f.condition
			} else {
				sb.WriteString(" on ")
				sb.WriteString(f.condition)
			}
		}
	}

	return sb.String(), on, nil
}
