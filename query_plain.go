package sql

import (
	"fmt"
	"strings"
)

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

func (p *plainBuilder) Build(driver Driver) (string, []any, error) {
	var sb strings.Builder
	sb.Grow(len(p.query))

	placeholderCount := 0
	for i := 0; i < len(p.query); i++ {
		if p.query[i] == '$' && i+1 < len(p.query) && p.query[i+1] == '?' {
			placeholderCount++
			sb.WriteString(driver.Placeholder(placeholderCount))
			i++
		} else {
			sb.WriteByte(p.query[i])
		}
	}

	if placeholderCount > 0 && placeholderCount != len(p.args) {
		return "", nil, fmt.Errorf("%w: expected %d, got %d", ErrArgumentsMismatch, placeholderCount, len(p.args))
	}

	return sb.String(), p.args, nil
}
