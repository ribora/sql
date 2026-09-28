package sql

import (
	"maps"
	"strings"
)

type View struct {
	base    *from
	joins   []*from
	columns map[string]string
}

func NewView(base fromTable, joins ...fromOptions) *View {
	b := *base.from()
	v := &View{base: &b, columns: make(map[string]string)}
	for _, join := range joins {
		j := *join()
		v.joins = append(v.joins, &j)
	}
	return v
}

func (v *View) Column(name string, expr string) *View {
	n := *v
	n.columns = maps.Clone(v.columns)
	n.columns[name] = expr
	return &n
}

func (v *View) source() ([]*from, *View) {
	base := *v.base
	items := []*from{&base}
	for _, j := range v.joins {
		c := *j
		items = append(items, &c)
	}
	return items, v
}

func (v *View) target() *from {
	t := *v.base
	t.alias = ""
	t.join = ""
	return &t
}

func (v *View) qualifier() string {
	if v.base.alias != "" {
		return v.base.alias
	}
	return v.base.table
}

func (v *View) rule(column string) (string, bool) {
	expr, ok := v.columns[column]
	if ok && !identReg.MatchString(expr) {
		expr = "(" + expr + ")"
	}
	return expr, ok
}

func (v *View) place(column string) string {
	if expr, ok := v.rule(column); ok {
		return expr
	}
	if strings.Contains(column, ".") || len(v.joins) == 0 {
		return column
	}
	return v.qualifier() + "." + column
}
