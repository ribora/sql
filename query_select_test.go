package sql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gorib/sql/driver"
)

func TestSort_UsesMappings(t *testing.T) {
	q := Select().From(Table("t")).Sort("facility desc", "createdAt")
	query, _, err := q.Build(driver.NewPostgres(), WithMappings(map[string]string{"facility": "line_facility"}))
	require.NoError(t, err)
	assert.Contains(t, query, "order by line_facility desc,created_at")
}

func TestSort_DirectionLowercased(t *testing.T) {
	q := Select().From(Table("t")).Sort("createdAt DESC")
	query, _, err := q.Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Contains(t, query, "order by created_at desc")
}

func TestSort_NullsOrdering(t *testing.T) {
	q := Select().From(Table("t")).Sort("endedAt asc nulls last")
	query, _, err := q.Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Contains(t, query, "order by ended_at asc nulls last")
}

// имя поля и направление приходят из внешнего sort[], поэтому мусор должен давать ошибку,
// а не молча превращаться в другую сортировку
func TestSort_InjectionRejected(t *testing.T) {
	q := Select().From(Table("t")).Sort("id asc; drop table users")
	_, _, err := q.Build(driver.NewPostgres())
	require.Error(t, err)
}

func TestSort_NullsOrderingWithoutDirection(t *testing.T) {
	q := Select().From(Table("t")).Sort("endedAt NULLS FIRST")
	query, _, err := q.Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Contains(t, query, "order by ended_at nulls first")
}

// хвост разбирается позиционно, поэтому набор верных слов в неверном порядке или количестве
// отсекается здесь, а не уезжает в базу синтаксической ошибкой
func TestSort_MalformedClauseRejected(t *testing.T) {
	for _, item := range []string{"id upwards", "id asc desc", "id nulls", "id first", "id nulls upper", "id asc nulls last extra"} {
		_, _, err := Select().From(Table("t")).Sort(item).Build(driver.NewPostgres())
		require.Error(t, err, item)
		assert.Contains(t, err.Error(), "unsupported sort clause", item)
	}
}

func TestSort_DefaultsToId(t *testing.T) {
	q := Select().From(Table("t"))
	query, _, err := q.Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Contains(t, query, "order by id asc")
}

func TestGroupAndReturning_UseMappings(t *testing.T) {
	mappings := WithMappings(map[string]string{"facility": "line_facility"})
	q := Select("count(*)").From(Table("t")).Group("facility")
	query, _, err := q.Build(driver.NewPostgres(), mappings)
	require.NoError(t, err)
	assert.Contains(t, query, "group by line_facility")

	u := Update(map[string]any{"x": 1}).Table(Table("t")).Returning("facility", "createdAt")
	query, _, err = u.Build(driver.NewPostgres(), mappings)
	require.NoError(t, err)
	assert.Contains(t, query, "returning line_facility,created_at")
}

// без явных полей select берёт returning — они тоже обязаны быть отображены на колонки
func TestSelect_FieldsFallBackToMappedReturning(t *testing.T) {
	q := Select().From(Table("t")).Returning("facility")
	query, _, err := q.Build(driver.NewPostgres(), WithMappings(map[string]string{"facility": "line_facility"}))
	require.NoError(t, err)
	assert.Contains(t, query, "select line_facility from t")
}
