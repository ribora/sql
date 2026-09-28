package sql

import (
	"fmt"
	"strings"
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

type resolver struct {
	mappings map[string]string
	view     *View
}

func newResolver(cfg *builderCfg, view *View) *resolver {
	return &resolver{mappings: cfg.mappings, view: view}
}

func (r *resolver) field(f string) (string, error) {
	column, err := sanitize(f, r.mappings)
	if err != nil || r.view == nil {
		return column, err
	}
	if _, mapped := r.mappings[f]; !mapped && strings.Contains(f, ".") {
		return "", fmt.Errorf("%q: a qualified name cannot reach past a view, declare it with Column", f)
	}
	return r.view.place(column), nil
}

func (r *resolver) projection(f string) (string, error) {
	column, err := sanitizeProjection(f, r.mappings)
	if err != nil || r.view == nil {
		return column, err
	}
	if expr, ok := r.view.rule(column); ok {
		return expr + " " + column, nil
	}
	return r.view.place(column), nil
}
