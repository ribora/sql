package sql

import (
	"fmt"
	"regexp"
	"strings"

	"gitlab.com/gorib/sql/types"
)

var (
	filterRegex = regexp.MustCompile(`[^a-zA-Z0-9_.\s*-]`)
	lowerRegex  = regexp.MustCompile(`([A-Z])`)
)

type builder interface {
	build(d Driver, p *[]any) (string, error)
}

type Builder interface {
	Build(driver Driver) (string, []any, error)
}

type DeleteBuilder interface {
	Builder
	delete()
}

type CountBuilder interface {
	Builder
	count()
}

func filter(field any) (string, error) {
	if f, ok := field.(types.SqlExpr); ok {
		return f.String(), nil
	} else if f, ok := field.(string); ok {
		f = filterRegex.ReplaceAllString(f, "")
		return lowerRegex.ReplaceAllStringFunc(f, func(s string) string {
			return "_" + strings.ToLower(s)
		}), nil
	}
	return "", fmt.Errorf("unsupported type for field '%s'", field)
}
