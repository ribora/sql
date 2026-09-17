package sql

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gorib/criteria"

	"gitlab.com/gorib/sql/driver"
)

// A tree built with these constructors is marshalled and sent to another
// service, which reads it back with criteria.FromBytes.
func TestConstructorsMarshalToTheWireTriple(t *testing.T) {
	expr := criteria.And(
		Eq("status", []string{"active", "locked"}),
		criteria.Or(Eq("is_default", true), Eq("id", 3)),
		Between("age", 18, 65),
		Eq("syncedAt", nil),
	)

	encoded, err := json.Marshal(expr)
	require.NoError(t, err)
	assert.Equal(
		t,
		`{"conditions":[["status","eq",["active","locked"]],{"conditions":[["is_default","eq",true],["id","eq",3]],"mode":"or"},["age","between",[18,65]],["syncedAt","eq",null]],"mode":"and"}`,
		string(encoded),
	)

	back, err := criteria.FromBytes(encoded)
	require.NoError(t, err)
	again, err := json.Marshal(back)
	require.NoError(t, err)
	assert.Equal(t, string(encoded), string(again))
}

// The literals are the wire format, not a copy of the op constants: spelling
// them as op.Eq would let a rename pass through unnoticed.
func TestConstructorsEmitTheWireOperation(t *testing.T) {
	cases := map[string]criteria.Condition{
		"eq":      Eq("f", 1),
		"ne":      Ne("f", 1),
		"gt":      Gt("f", 1),
		"ge":      Ge("f", 1),
		"lt":      Lt("f", 1),
		"le":      Le("f", 1),
		"between": Between("f", 1, 2),
		"like":    Like("f", "x"),
		"ilike":   ILike("f", "x"),
		"json_in": JsonIn("f", 1),
		"json_eq": JsonEq("f", 1),
	}

	for operation, condition := range cases {
		assert.Equal(t, operation, condition.Operation)
	}
}

func TestBuildFromTheWireMatchesTheConstructors(t *testing.T) {
	built := criteria.And(Eq("status", "active"), criteria.Or(Eq("type", "a"), Eq("type", "b")))
	encoded, err := json.Marshal(built)
	require.NoError(t, err)
	parsed, err := criteria.FromBytes(encoded)
	require.NoError(t, err)

	builtQuery, builtParams := buildPostgres(t, built)
	parsedQuery, parsedParams := buildPostgres(t, parsed)
	assert.Equal(t, builtQuery, parsedQuery)
	assert.Equal(t, builtParams, parsedParams)
}

// A hand-built tree used to reach an unchecked assertion in build().
func TestBuildRejectsAMalformedCondition(t *testing.T) {
	cases := []struct {
		name string
		expr criteria.Expression
		err  string
	}{
		{"NonStringOperation", criteria.And("a", 5, 1), "operation must be a string"},
		{"NonStringField", criteria.And(5, "eq", 1), "field must be a string"},
		{"WrongArity", criteria.And("a", "eq"), "operations support only 3 argument"},
	}

	for _, c := range cases {
		t.Run(
			c.name,
			func(t *testing.T) {
				_, _, err := Select().From(Table("t")).Where(c.expr).Build(driver.NewPostgres())
				assert.ErrorContains(t, err, c.err)
			},
		)
	}
}
