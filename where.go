package sql

import (
	"fmt"
	"regexp"
	"strings"

	"gitlab.com/gorib/sql/types"

	"gitlab.com/gorib/criteria"
)

var (
	filterReg = regexp.MustCompile(`[^a-zA-Z0-9_.\s-]`)
	lowerReg  = regexp.MustCompile(`([A-Z])`)
)

func newExpressionBuilder(e criteria.Expression, builder Driver) *exprBuilder {
	return &exprBuilder{driver: builder, e: e}
}

type exprBuilder struct {
	e      criteria.Expression
	driver Driver
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
		if ex, ok := part.(criteria.Expression); ok {
			q, err = newExpressionBuilder(ex, e.driver).build(params)
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

			if f, ok := field.(types.SqlExpr); ok {
				field = f.String()
			} else if f, ok := field.(string); ok {
				f = filterReg.ReplaceAllString(f, "")
				field = lowerReg.ReplaceAllStringFunc(f, func(s string) string {
					return "_" + strings.ToLower(s)
				})
			} else {
				return "", fmt.Errorf("unsupported type for field '%s'", field)
			}

			if s, ok := value.(builder); ok {
				q, err = s.build(e.driver, params)
				q = field.(string) + " " + operation + " (" + q + ")"
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
