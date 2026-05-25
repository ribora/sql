package sql

import (
	"gitlab.com/gorib/sql/types"
)

func Expr(expression string) *raw {
	return &raw{expression: expression}
}

type raw struct {
	types.SqlExpr
	expression string
}

func (r *raw) String() string {
	return r.expression
}
