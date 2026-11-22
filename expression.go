package sql

import (
	"github.com/ribora/criteria"

	"github.com/ribora/sql/op"
)

func Eq(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.Eq), value)
}

func Ne(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.Ne), value)
}

func Gt(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.Gt), value)
}

func Ge(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.Ge), value)
}

func Lt(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.Lt), value)
}

func Le(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.Le), value)
}

func Between(field string, from, to any) criteria.Condition {
	return criteria.Cond(field, string(op.Between), []any{from, to})
}

func Like(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.Like), value)
}

func ILike(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.ILike), value)
}

func JsonIn(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.JsonIn), value)
}

func JsonEq(field string, value any) criteria.Condition {
	return criteria.Cond(field, string(op.JsonEq), value)
}
