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

type Valueable interface {
	values() []any
}

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
	expression, ok := e.e.(criteria.Builder)
	if !ok {
		return "", fmt.Errorf("need a criteria here")
	}
	strategy, err := expression.Strategy()
	if err != nil {
		return "", err
	}
	parts, err := expression.Params()
	if err != nil {
		return "", err
	}

	var query []string
	var q string
	for _, part := range parts {
		if v, ok := part.(Valueable); ok {
			part = v.values()
		}
		if ex, ok := part.(criteria.Expression); ok {
			q, err = newExpressionBuilder(ex, e.driver, e.cfg).build(params)
			if err != nil {
				return "", err
			}
			if q != "" {
				query = append(query, q)
			}
		} else {
			v := part.([]any)
			field := v[0]
			operation := v[1].(string)
			value := v[2]

			if f, ok := field.(string); ok {
				field, err = sanitize(f, e.cfg.mappings)
				if err != nil {
					return "", err
				}
			} else {
				return "", fmt.Errorf("unsupported type for field '%s'", field)
			}

			if s, ok := value.(builder); ok {
				sub, err := s.build(e.driver, e.cfg, params)
				if err != nil {
					return "", err
				}
				q, params, err = e.driver.Field(field.(string), operation, driver.Raw(sub), params)
			} else if r, ok := value.(Field); ok {
				var m string
				m, err = sanitize(string(r), e.cfg.mappings)
				if err != nil {
					return "", err
				}
				q, params, err = e.driver.Field(field.(string), operation, driver.Raw(m), params)
			} else {
				q, params, err = e.driver.Field(field.(string), operation, value, params)
			}

			if err != nil {
				return "", err
			}
			if q != "" {
				query = append(query, q)
			}
		}
	}

	if strategy == "" {
		strategy = criteria.StrategyAnd
	}
	if strategy != criteria.StrategyAnd && strategy != criteria.StrategyOr {
		return "", fmt.Errorf("%w: '%s'", ErrUnsupportedQueryMode, strategy)
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
