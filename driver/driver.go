package driver

import (
	"fmt"
	"reflect"
	"strings"
)

type Raw string

type Driver interface {
	Returning() bool
	Placeholder(pos int) string
	Conflict(conflict string, updates []string) string
	UpdateAsJoin() bool
}

func newDriver(operations map[string][]string) *driver {
	o := make(map[string]string, len(operations))
	for k, v := range operations {
		for _, op := range v {
			o[op] = k
		}
	}
	return &driver{op: o}
}

type driver struct {
	op map[string]string
}

func (p driver) field(d Driver, field string, operation string, value any, params *[]any) (string, *[]any, error) {
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
		var prefix, delimiter, suffix string
		switch operation {
		case "=", "in":
			if v.Len() == 0 {
				return "0=1", params, nil
			}
			operation = "in"
			prefix, delimiter, suffix = " (", ", ", ") "
		case "!=", "not in":
			if v.Len() == 0 {
				return "", params, nil
			}
			operation = "not in"
			prefix, delimiter, suffix = " (", ", ", ") "
		case "between":
			if v.Len() != 2 {
				return "", nil, fmt.Errorf("wrong arguments count for operation 'between': %v", value)
			}
			prefix, delimiter, suffix = " ", " and ", " "
		default:
			return "", params, fmt.Errorf("unknown operation '%v' for slice", operation)
		}

		var in []string
		for idx := 0; idx < v.Len(); idx++ {
			*params = append(*params, v.Index(idx).Interface())
			in = append(in, d.Placeholder(len(*params)))
		}
		return field + " " + operation + prefix + strings.Join(in, delimiter) + suffix, params, nil
	}

	*params = append(*params, value)
	return field + " " + operation + " " + d.Placeholder(len(*params)), params, nil
}

func (p driver) operation(alias string) (string, bool) {
	o, found := p.op[alias]
	return o, found
}
