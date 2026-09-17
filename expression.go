package sql

import (
	"gitlab.com/gorib/criteria"
)

func Eq(field string, value any) criteria.Condition {
	return criteria.Cond(field, "eq", value)
}

func Ne(field string, value any) criteria.Condition {
	return criteria.Cond(field, "ne", value)
}

func Gt(field string, value any) criteria.Condition {
	return criteria.Cond(field, "gt", value)
}

func Ge(field string, value any) criteria.Condition {
	return criteria.Cond(field, "ge", value)
}

func Lt(field string, value any) criteria.Condition {
	return criteria.Cond(field, "lt", value)
}

func Le(field string, value any) criteria.Condition {
	return criteria.Cond(field, "le", value)
}

func Between(field string, from, to any) criteria.Condition {
	return criteria.Cond(field, "between", []any{from, to})
}

func Like(field string, value any) criteria.Condition {
	return criteria.Cond(field, "like", value)
}

func ILike(field string, value any) criteria.Condition {
	return criteria.Cond(field, "ilike", value)
}

func JsonIn(field string, value any) criteria.Condition {
	return criteria.Cond(field, "json_in", value)
}

func JsonEq(field string, value any) criteria.Condition {
	return criteria.Cond(field, "json_eq", value)
}
