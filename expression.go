package sql

import (
	"encoding/json"

	"gitlab.com/gorib/criteria"
)

func Eq(field string, value any) Valueable {
	return &expression{v: []any{field, "eq", value}}
}

func Ne(field string, value any) Valueable {
	return &expression{v: []any{field, "ne", value}}
}

func Gt(field string, value any) Valueable {
	return &expression{v: []any{field, "gt", value}}
}

func Ge(field string, value any) Valueable {
	return &expression{v: []any{field, "ge", value}}
}

func Lt(field string, value any) Valueable {
	return &expression{v: []any{field, "lt", value}}
}

func Le(field string, value any) Valueable {
	return &expression{v: []any{field, "le", value}}
}

func Between(field string, from, to any) Valueable {
	return &expression{v: []any{field, "between", []any{from, to}}}
}

func Like(field string, value any) Valueable {
	return &expression{v: []any{field, "like", value}}
}

func ILike(field string, value any) Valueable {
	return &expression{v: []any{field, "ilike", value}}
}

func JsonIn(field string, value any) Valueable {
	return &expression{v: []any{field, "json_in", value}}
}

func JsonEq(field string, value any) Valueable {
	return &expression{v: []any{field, "json_eq", value}}
}

type expression struct {
	criteria.Expression
	v []any
}

func (e *expression) values() []any {
	return e.v
}

func (e *expression) UnmarshalJSON(bytes []byte) error {
	var v []any
	if err := json.Unmarshal(bytes, &v); err != nil {
		return err
	}
	e.v = v
	return nil
}

func (e *expression) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.v)
}
