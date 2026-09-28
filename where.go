package sql

import (
	"fmt"
	"regexp"
	"strings"

	"gitlab.com/gorib/criteria"
	"gitlab.com/gorib/sql/driver"
)

var (
	identReg      = regexp.MustCompile(`^\w+(\.\w+)*$`)
	projectionReg = regexp.MustCompile(`^(\*|\w+(\.\w+)*(\.\*)?)$`)
	lowerReg      = regexp.MustCompile(`([A-Z])`)
)

func newExpressionBuilder(e criteria.Expression, builder Driver, cfg *builderCfg, names *resolver) *exprBuilder {
	return &exprBuilder{driver: builder, e: e, cfg: cfg, names: names}
}

type Field string

type exprBuilder struct {
	e      criteria.Expression
	driver Driver
	cfg    *builderCfg
	names  *resolver
}

func (e *exprBuilder) build(params *[]any) (string, error) {
	if e == nil || e.e == nil {
		return "", nil
	}
	if e.driver == nil {
		return "", ErrExprBuilderNotImplemented
	}
	strategy, nodes, err := criteria.Unpack(e.e)
	if err != nil {
		return "", err
	}

	var query []string
	for _, node := range nodes {
		var q string
		if node.Nested != nil {
			q, err = newExpressionBuilder(node.Nested, e.driver, e.cfg, e.names).build(params)
			if err != nil {
				return "", err
			}
			if q != "" {
				query = append(query, q)
			}
			continue
		}

		var field string
		field, err = e.names.field(node.Condition.Field)
		if err != nil {
			return "", err
		}

		switch value := node.Condition.Value.(type) {
		case builder:
			var sub string
			sub, err = value.build(e.driver, e.cfg, params)
			if err != nil {
				return "", err
			}
			q, params, err = e.driver.Field(field, node.Condition.Operation, driver.Raw(sub), params)
		case Field:
			var m string
			m, err = e.names.field(string(value))
			if err != nil {
				return "", err
			}
			q, params, err = e.driver.Field(field, node.Condition.Operation, driver.Raw(m), params)
		default:
			q, params, err = e.driver.Field(field, node.Condition.Operation, value, params)
		}

		if err != nil {
			return "", err
		}
		if q != "" {
			query = append(query, q)
		}
	}

	if len(query) == 0 {
		return "", nil
	}
	var sb strings.Builder
	sb.WriteString("(")
	sb.WriteString(strings.Join(query, ") "+string(strategy)+" ("))
	sb.WriteString(")")
	return sb.String(), nil
}

func sanitize(f string, m map[string]string) (string, error) {
	return sanitizeAs(f, m, identReg)
}

func sanitizeProjection(f string, m map[string]string) (string, error) {
	return sanitizeAs(f, m, projectionReg)
}

func sanitizeAs(f string, m map[string]string, allowed *regexp.Regexp) (string, error) {
	if r, ok := m[f]; ok {
		return r, nil
	}
	if !allowed.MatchString(f) {
		return "", fmt.Errorf("invalid characters in field name: %q", f)
	}
	return lowerReg.ReplaceAllStringFunc(f, func(s string) string {
		return "_" + strings.ToLower(s)
	}), nil
}

func sanitizeOrder(item string, names *resolver) (string, error) {
	parts := strings.Fields(item)
	if len(parts) == 0 {
		return "", nil
	}
	field, err := names.field(parts[0])
	if err != nil {
		return "", err
	}
	tail := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		tail = append(tail, strings.ToLower(part))
	}
	clause := []string{field}
	if len(tail) > 0 && (tail[0] == "asc" || tail[0] == "desc") {
		clause = append(clause, tail[0])
		tail = tail[1:]
	}
	if len(tail) > 0 {
		if len(tail) != 2 || tail[0] != "nulls" || (tail[1] != "first" && tail[1] != "last") {
			return "", fmt.Errorf("unsupported sort clause: %q", item)
		}
		clause = append(clause, tail...)
	}
	return strings.Join(clause, " "), nil
}
