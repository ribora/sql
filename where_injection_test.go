package sql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ribora/criteria"
	"github.com/ribora/sql/driver"
)

func injectWhere(field string, m map[string]string) (string, error) {
	q, _, err := Select().
		From(Table("t")).
		Where(criteria.And(Eq(field, 1))).
		Build(driver.NewPostgres(), WithMappings(m))
	return q, err
}

// The LIMIT after the ORDER BY is what a trailing comment swallows.
func injectOrder(field string, m map[string]string) (string, error) {
	q, _, err := Select().
		From(Table("t")).
		Sort(field).
		Page(0, 20).
		Build(driver.NewPostgres(), WithMappings(m))
	return q, err
}

func injectProjection(field string, m map[string]string) (string, error) {
	q, _, err := Select().
		From(Table("t")).
		Returning(field).
		Build(driver.NewPostgres(), WithMappings(m))
	return q, err
}

func TestSanitize_Injection(t *testing.T) {
	// An empty want means the name must be rejected.
	cases := []struct {
		name     string
		field    string
		mappings map[string]string
		where    string
		order    string
		proj     string
	}{
		{
			name:  "plain name",
			field: "code",
			where: "select * from t where (code = $1)",
			order: "select * from t order by code limit 20",
			proj:  "select code from t",
		},
		{
			name:  "camelCase folded to snake_case",
			field: "facilityCode",
			where: "select * from t where (facility_code = $1)",
			order: "select * from t order by facility_code limit 20",
			proj:  "select facility_code from t",
		},
		{
			name:  "dotted name",
			field: "u.name",
			where: "select * from t where (u.name = $1)",
			order: "select * from t order by u.name limit 20",
			proj:  "select u.name from t",
		},
		{
			name:     "mapped name",
			field:    "facilityCode",
			mappings: map[string]string{"facilityCode": "f.code"},
			where:    "select * from t where (f.code = $1)",
			order:    "select * from t order by f.code limit 20",
			proj:     "select f.code from t",
		},
		{
			name:  "trailing comment, single token",
			field: "id-- ",
		},
		{
			name:  "trailing comment, own token",
			field: "id --",
		},
		{
			name:  "comment glued to the name",
			field: "id--",
		},
		{
			name:  "statement terminator",
			field: "id; drop table users",
		},
		{
			name:  "whitespace",
			field: "id name",
		},
		{
			name:  "star",
			field: "*",
			proj:  "select * from t",
		},
		{
			name:  "qualified star",
			field: "t.*",
			proj:  "select t.* from t",
		},
		{
			name:  "star glued to a name",
			field: "id*",
		},
		{
			name:  "empty name, dropped from sort and leaving the page without an order",
			field: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, path := range []struct {
				clause string
				render func(string, map[string]string) (string, error)
				want   string
			}{
				{"where", injectWhere, c.where},
				{"order", injectOrder, c.order},
				{"projection", injectProjection, c.proj},
			} {
				query, err := path.render(c.field, c.mappings)
				if path.want == "" {
					assert.Error(t, err, "%s: %q must be rejected, rendered %q", path.clause, c.field, query)
					continue
				}
				if assert.NoError(t, err, "%s: %q", path.clause, c.field) {
					assert.Equal(t, path.want, query, path.clause)
				}
			}
		})
	}
}

func TestSanitize_MappedValueIsTrustedVerbatim(t *testing.T) {
	query, err := injectWhere("sort", map[string]string{"sort": "id-- "})
	require.NoError(t, err)
	assert.Equal(t, "select * from t where (id--  = $1)", query)

	_, err = injectWhere("id-- ", nil)
	assert.Error(t, err)
}
