package sql

type Value any

type Valuer interface {
	Value() (Value, error)
}

type nilValuer interface {
	IsNil() bool
}

func values(params []any) ([]any, error) {
	v := make([]any, 0, len(params))
	for _, p := range params {
		if p == nil {
			v = append(v, nil)
		} else if pp, ok := p.(nilValuer); ok && pp.IsNil() {
			v = append(v, nil)
		} else if pp, ok := p.(Valuer); ok {
			vv, err := pp.Value()
			if err != nil {
				return nil, err
			}
			v = append(v, vv)
		} else {
			v = append(v, p)
		}
	}
	return v, nil
}
