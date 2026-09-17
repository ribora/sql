package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/gorib/sql/op"
)

// The sql constructors emit these; a driver that does not know one fails only
// when the query runs.
var constructorOperations = []op.Operation{
	op.Eq, op.Ne, op.Gt, op.Ge, op.Lt, op.Le,
	op.Between, op.Like, op.ILike, op.JsonIn, op.JsonEq,
}

type fielder interface {
	Field(field string, operation string, value any, params *[]any) (string, *[]any, error)
}

func TestConstructorOperationsPerDriver(t *testing.T) {
	cases := []struct {
		name        string
		driver      fielder
		unsupported []op.Operation
	}{
		{name: "postgres", driver: NewPostgres(), unsupported: nil},
		{name: "mysql", driver: NewMysql(), unsupported: []op.Operation{op.JsonIn, op.JsonEq}},
		{name: "sqlite", driver: NewSqlite(), unsupported: []op.Operation{op.JsonIn, op.JsonEq}},
	}

	for _, c := range cases {
		t.Run(
			c.name,
			func(t *testing.T) {
				var rejected []op.Operation
				for _, operation := range constructorOperations {
					params := []any{}
					_, _, err := c.driver.Field("f", string(operation), 1, &params)
					if err != nil {
						assert.ErrorContains(t, err, "unsupported sql operation", "%s", operation)
						rejected = append(rejected, operation)
					}
				}
				assert.Equal(t, c.unsupported, rejected)
			},
		)
	}
}

// ILike has no case-insensitive counterpart outside Postgres, so it silently
// becomes a case-sensitive match there.
func TestILikeCollapsesOutsidePostgres(t *testing.T) {
	cases := []struct {
		name     string
		driver   fielder
		expected string
	}{
		{name: "postgres", driver: NewPostgres(), expected: "f ilike $1"},
		{name: "mysql", driver: NewMysql(), expected: "f like ?"},
		{name: "sqlite", driver: NewSqlite(), expected: "f like ?"},
	}

	for _, c := range cases {
		t.Run(
			c.name,
			func(t *testing.T) {
				params := []any{}
				query, _, err := c.driver.Field("f", string(op.ILike), "x%", &params)
				require.NoError(t, err)
				assert.Equal(t, c.expected, query)
			},
		)
	}
}
