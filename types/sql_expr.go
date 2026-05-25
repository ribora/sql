package types

type SqlExpr interface {
	String() string
	raw()
}
