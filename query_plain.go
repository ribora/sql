package sql

import (
	"fmt"
	"strings"
)

var Expr = Plain

func Plain(query string, params ...any) *plainBuilder {
	return &plainBuilder{
		query: query,
		args:  params,
	}
}

type plainBuilder struct {
	DeleteBuilder
	CountBuilder
	query string
	args  []any
}

func (b *plainBuilder) Build(driver Driver, _ ...BuildOption) (string, []any, error) {
	var p []any
	q, err := b.build(driver, nil, &p)
	if err != nil {
		return "", nil, err
	}
	return q, p, nil
}

func (b *plainBuilder) build(driver Driver, _ *builderCfg, p *[]any) (string, error) {
	var sb strings.Builder
	sb.Grow(len(b.query))

	placeholderCount := len(*p)
	for i := 0; i < len(b.query); i++ {
		if b.query[i] == '$' && i+1 < len(b.query) && b.query[i+1] == '?' {
			placeholderCount++
			sb.WriteString(driver.Placeholder(placeholderCount))
			i++
		} else {
			sb.WriteByte(b.query[i])
		}
	}

	*p = append(*p, b.args...)
	if placeholderCount > 0 && placeholderCount != len(*p) {
		return "", fmt.Errorf("%w: expected %d, got %d", ErrArgumentsMismatch, placeholderCount, len(*p))
	}

	return sb.String(), nil
}
