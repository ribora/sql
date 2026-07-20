package sql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.com/gorib/criteria"
	"gitlab.com/gorib/sql/driver"
)

// Postgres' Build with a query just for testing the WHERE clause shape.
func buildPostgres(t *testing.T, expr criteria.Expression, opts ...BuildOption) (string, []any) {
	t.Helper()
	q := Select().From(Table("t")).Where(expr)
	query, params, err := q.Build(driver.NewPostgres(), opts...)
	assert.NoError(t, err)
	return query, params
}

func TestField_Reference_WithMapping(t *testing.T) {
	expr := criteria.And(Lt("syncedAt", Field("updatedAt")))
	query, params := buildPostgres(t, expr, WithMappings(map[string]string{
		"syncedAt":  "synced_at",
		"updatedAt": "updated_at",
	}))
	assert.Contains(t, query, "synced_at < (updated_at)")
	assert.Empty(t, params, "field-to-field comparison must not produce bound params")
}

func TestField_Reference_AutoSnakeCase(t *testing.T) {
	// No mapping — sanitize() should camelCase→snake_case for both sides.
	expr := criteria.And(Lt("syncedAt", Field("updatedAt")))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "synced_at < (updated_at)")
	assert.Empty(t, params)
}

func TestField_Reference_InjectionRejected(t *testing.T) {
	expr := criteria.And(Lt("x", Field("bad;DROP TABLE users")))
	q := Select().From(Table("t")).Where(expr)
	_, _, err := q.Build(driver.NewPostgres())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestField_Reference_ChainedWithRegularConditions(t *testing.T) {
	// Mix Field-ref with regular scalar conditions in one AND.
	expr := criteria.And(
		Eq("status", "active"),
		Lt("syncedAt", Field("updatedAt")),
	)
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "status = $1")
	assert.Contains(t, query, "synced_at < (updated_at)")
	assert.Equal(t, []any{"active"}, params)
}

func TestPlain_Value_OperatorTranslated(t *testing.T) {
	// `Eq` on a Plain value: operator translation must still happen, but
	// the right side is the pre-built SQL fragment (no extra placeholder).
	expr := criteria.And(Eq("price", Plain("jsonb_set(price, '{$?}', $?::text::jsonb, true)", "US", 123)))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "price in (jsonb_set(price, '{$1}', $2::text::jsonb, true))",
		"= with Plain value renders as IN (...) — universal-fit for scalar/subquery alike")
	assert.Equal(t, []any{"US", 123}, params)
}

func TestSlice_EqUsesANY(t *testing.T) {
	expr := criteria.And(Eq("name", []string{"John", "Kevin"}))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "name = ANY($1)")
	assert.Equal(t, []any{[]string{"John", "Kevin"}}, params)
}

func TestSlice_NotInUsesALL(t *testing.T) {
	// Regression: `<> ANY` would semantically mean "differs from at least one",
	// which is almost always true. `<> ALL` is the correct NOT IN semantics.
	expr := criteria.And(Ne("name", []string{"John", "Mike"}))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "<> ALL($1)")
	assert.NotContains(t, query, "<> ANY")
	assert.Equal(t, []any{[]string{"John", "Mike"}}, params)
}

func TestSlice_EmptyEqIsFalse(t *testing.T) {
	expr := criteria.And(Eq("name", []string{}))
	query, _ := buildPostgres(t, expr)
	assert.Contains(t, query, "false",
		"empty IN-set short-circuits to literal false (no row matches)")
}

func TestSlice_AnyHomogenizedToTyped(t *testing.T) {
	expr := criteria.And(Eq("sku", []any{"108311370", "108311371"}))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "sku = ANY($1)")
	assert.Equal(t, []any{[]string{"108311370", "108311371"}}, params,
		"pgx can't encode []any — driver must convert to typed slice")
}

func TestSlice_AnyMixedTypesNotHomogenized(t *testing.T) {
	expr := criteria.And(Eq("x", []any{"a", 1}))
	_, params := buildPostgres(t, expr)
	assert.Equal(t, []any{[]any{"a", 1}}, params,
		"heterogeneous []any passes through — pgx surfaces the error")
}

func TestJsonEq_ScalarUsesQuestion(t *testing.T) {
	expr := criteria.And(JsonEq("metadata", "key1"))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "metadata ? $1")
	assert.Equal(t, []any{"key1"}, params)
}

func TestJsonEq_SliceRewritesToAnyOf(t *testing.T) {
	// JsonEq mirrors Eq: scalar → '?', slice → '?|' (any-of).
	expr := criteria.And(JsonEq("metadata", []string{"k1", "k2"}))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "metadata ?| $1")
	assert.Equal(t, []any{[]string{"k1", "k2"}}, params)
}

func TestJsonEq_AnySliceHomogenized(t *testing.T) {
	// JSON-decoded keys arrive as []any; pgx can't encode []any as text[].
	// Homogenize + slice→?| rewrite together restore the path.
	expr := criteria.And(JsonEq("metadata", []any{"k1", "k2"}))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "metadata ?| $1")
	assert.Equal(t, []any{[]string{"k1", "k2"}}, params)
}

func TestBetween(t *testing.T) {
	expr := criteria.And(Between("age", 18, 65))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "age between $1 and $2")
	assert.Equal(t, []any{18, 65}, params)
}

func TestField_RegularPath_CamelCaseToSnake(t *testing.T) {
	// Ensure the existing camelCase→snake_case auto-conversion still works
	// after the sanitize() refactor.
	expr := criteria.And(Eq("facilityCode", "X1"))
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "facility_code = $1")
	assert.Equal(t, []any{"X1"}, params)
}

func TestField_RegularPath_Mapping(t *testing.T) {
	expr := criteria.And(Eq("facilityCode", "X1"))
	query, params := buildPostgres(t, expr, WithMappings(map[string]string{
		"facilityCode": "f.code", // alias-prefixed
	}))
	assert.Contains(t, query, "f.code = $1")
	assert.Equal(t, []any{"X1"}, params)
}

func TestField_RegularPath_InjectionRejected(t *testing.T) {
	expr := criteria.And(Eq("a; DROP TABLE x", "v"))
	q := Select().From(Table("t")).Where(expr)
	_, _, err := q.Build(driver.NewPostgres())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestNestedAndOr(t *testing.T) {
	// Combine AND and OR; ensure conditions are correctly grouped.
	expr := criteria.And(
		Eq("status", "active"),
		criteria.Or(
			Eq("type", "a"),
			Eq("type", "b"),
		),
	)
	query, params := buildPostgres(t, expr)
	assert.Contains(t, query, "(status = $1) and ((type = $2) or (type = $3))")
	assert.Equal(t, []any{"active", "a", "b"}, params)
}

func TestSanitize_HelperDirect(t *testing.T) {
	// Map hit
	out, err := sanitize("facilityCode", map[string]string{"facilityCode": "f.code"})
	assert.NoError(t, err)
	assert.Equal(t, "f.code", out)

	// No mapping → auto camelCase to snake_case
	out, err = sanitize("createdAt", nil)
	assert.NoError(t, err)
	assert.Equal(t, "created_at", out)

	// No mapping, simple lowercase → unchanged
	out, err = sanitize("code", nil)
	assert.NoError(t, err)
	assert.Equal(t, "code", out)

	// Already underscored — pass through
	out, err = sanitize("facility_code", nil)
	assert.NoError(t, err)
	assert.Equal(t, "facility_code", out)

	// Injection rejected
	_, err = sanitize("a; DROP", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")

	// Initialism — lowerReg replaces each uppercase letter independently.
	// Documented as the current behaviour; if you adopt initialism-aware
	// conversion (e.g. strcase.ToSnake), update this test.
	out, err = sanitize("URLPath", nil)
	assert.NoError(t, err)
	assert.Equal(t, "_u_r_l_path", out, "documents current naive camelCase→snake conversion")
}
