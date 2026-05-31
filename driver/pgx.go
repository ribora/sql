package driver

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func NewPostgres() *pgxDriver {
	return &pgxDriver{
		driver: newDriver(map[string][]string{
			"=":       {"=", "eq", "in"},
			"!=":      {"!=", "ne", "neq", "nin"},
			">":       {">", "gt"},
			">=":      {">=", "gte", "ge"},
			"<":       {"<", "lt"},
			"<=":      {"<=", "lte", "le"},
			"between": {"between"},
			"like":    {"like"},
			"ilike":   {"ilike"},
			"@>":      {"@>", "json_in"},
			"?":       {"?", "json_eq"},
		}),
	}
}

type pgxDriver struct {
	*driver
}

func (p pgxDriver) Field(field string, operation string, value any, params *[]any) (string, *[]any, error) {
	o, found := p.operation(operation)
	if !found {
		return "", nil, fmt.Errorf("unsupported sql operation: '%s'", operation)
	}
	operation = o

	if value == nil {
		op := operation
		operation = map[string]string{
			"=":  "is null",
			"!=": "is not null",
		}[operation]
		if operation == "" {
			return "", params, fmt.Errorf("unknown operation %q for nil value", op)
		}
		return field + " " + operation, params, nil
	}

	if vv, ok := value.(Raw); ok {
		switch operation {
		case "=", "in":
			operation = "in"
		case "!=", "not in":
			operation = "not in"
		}
		return field + " " + operation + " (" + string(vv) + ")", params, nil
	}

	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Slice {
		switch operation {
		case "=", "in":
			if v.Len() == 0 {
				return "false", params, nil
			}
			operation = "="
			*params = append(*params, value)
			return field + " = ANY(" + p.Placeholder(len(*params)) + ")", params, nil
		case "!=", "not in":
			if v.Len() == 0 {
				return "", params, nil
			}
			*params = append(*params, value)
			return field + " <> ALL(" + p.Placeholder(len(*params)) + ")", params, nil
		case "?", "?|", "?&":
			if v.Len() == 0 {
				return "false", params, nil
			}
			*params = append(*params, value)
			return field + " " + operation + " " + p.Placeholder(len(*params)), params, nil
		case "between":
			if v.Len() != 2 {
				return "", nil, fmt.Errorf("between requires 2 args, got %d", v.Len())
			}
			*params = append(*params, v.Index(0).Interface())
			ph1 := p.Placeholder(len(*params))
			*params = append(*params, v.Index(1).Interface())
			ph2 := p.Placeholder(len(*params))
			return field + " between " + ph1 + " and " + ph2, params, nil

		default:
			return "", params, fmt.Errorf("unknown operation '%v' for slice", operation)
		}
	}

	*params = append(*params, value)
	return field + " " + operation + " " + p.Placeholder(len(*params)), params, nil
}

func (p pgxDriver) Returning() bool {
	return true
}

func (p pgxDriver) Placeholder(pos int) string {
	return "$" + strconv.Itoa(pos)
}

func (p pgxDriver) Conflict(conflict string, updates []string) string {
	if len(updates) > 0 {
		parts := make([]string, 0, len(updates))
		for _, u := range updates {
			parts = append(parts, u+"="+"excluded."+u)
		}
		return "on conflict (" + conflict + ") do update set " + strings.Join(parts, ", ")
	}
	return "on conflict (" + conflict + ") do nothing"
}

func (p pgxDriver) UpdateAsJoin() bool {
	return false
}
