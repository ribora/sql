package op

type Operation string

const (
	Eq  Operation = "eq"
	In  Operation = "in"
	Ne  Operation = "ne"
	Neq Operation = "neq"
	Nin Operation = "nin"
	Gt  Operation = "gt"
	Ge  Operation = "ge"
	Gte Operation = "gte"
	Lt  Operation = "lt"
	Le  Operation = "le"
	Lte Operation = "lte"

	Between Operation = "between"
	Like    Operation = "like"
	ILike   Operation = "ilike"
	JsonIn  Operation = "json_in"
	JsonEq  Operation = "json_eq"
)
