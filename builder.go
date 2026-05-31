package sql

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	filterRegex = regexp.MustCompile(`[^a-zA-Z0-9_.\s*-]`)
	lowerRegex  = regexp.MustCompile(`([A-Z])`)
)

func newBuilderCfg(opts []BuildOption) (*builderCfg, error) {
	cfg := &builderCfg{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

type builderCfg struct {
	mappings map[string]string
}

type BuildOption func(*builderCfg) error

func WithMappings(mappings map[string]string) BuildOption {
	return func(cfg *builderCfg) error {
		cfg.mappings = mappings
		return nil
	}
}

type builder interface {
	build(d Driver, cfg *builderCfg, p *[]any) (string, error)
}

type Builder interface {
	Build(driver Driver, opts ...BuildOption) (string, []any, error)
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
	if f, ok := field.(string); ok {
		f = filterRegex.ReplaceAllString(f, "")
		return lowerRegex.ReplaceAllStringFunc(f, func(s string) string {
			return "_" + strings.ToLower(s)
		}), nil
	}
	return "", fmt.Errorf("unsupported type for field '%s'", field)
}
