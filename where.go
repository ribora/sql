package sql

import (
	"fmt"
	"regexp"
	"strings"

	"gitlab.com/gorib/criteria"
	"gitlab.com/gorib/sql/driver"
)

var (
	filterReg = regexp.MustCompile(`[^a-zA-Z0-9_.\s*-]`)
	lowerReg  = regexp.MustCompile(`([A-Z])`)
)

func newExpressionBuilder(e criteria.Expression, builder Driver, cfg *builderCfg) *exprBuilder {
	return &exprBuilder{driver: builder, e: e, cfg: cfg}
}

type Field string

type exprBuilder struct {
	e      criteria.Expression
	driver Driver
	cfg    *builderCfg
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
			q, err = newExpressionBuilder(node.Nested, e.driver, e.cfg).build(params)
			if err != nil {
				return "", err
			}
			if q != "" {
				query = append(query, q)
			}
			continue
		}

		var field string
		field, err = sanitize(node.Condition.Field, e.cfg.mappings)
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
			m, err = sanitize(string(value), e.cfg.mappings)
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
	if r, ok := m[f]; ok {
		f = r
	} else {
		// protect against injections
		if filterReg.MatchString(f) {
			return "", fmt.Errorf("invalid characters in field name: %q", f)
		}
		f = lowerReg.ReplaceAllStringFunc(f, func(s string) string {
			return "_" + strings.ToLower(s)
		})
	}
	return f, nil
}

func sanitizeOrder(item string, m map[string]string) (string, error) {
	parts := strings.Fields(item)
	if len(parts) == 0 {
		return "", nil
	}
	field, err := sanitize(parts[0], m)
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
