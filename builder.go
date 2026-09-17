package sql

import (
	"fmt"
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

func filter(field any, m map[string]string) (string, error) {
	f, ok := field.(string)
	if !ok {
		return "", fmt.Errorf("unsupported type for field '%s'", field)
	}
	return sanitizeProjection(f, m)
}

func filterAll(fields []string, m map[string]string) ([]string, error) {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		f, err := filter(field, m)
		if err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, nil
}
