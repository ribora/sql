package sql

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ribora/criteria"
	"github.com/ribora/sql/driver"
)

func TestFrom_JoinLeavesTheTableAlone(t *testing.T) {
	users := Table("users")
	_, _, err := Select().From(Table("orders"), LeftJoin(users, "users.id = orders.user_id")).Build(driver.NewPostgres())
	require.NoError(t, err)

	got, _, err := Select().From(users).Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Equal(t, "select * from users", got)
}

func TestFrom_SharedTableBuildsConcurrently(t *testing.T) {
	users := Table("users")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, _, _ = Select().From(Table("orders"), LeftJoin(users, "users.id = orders.user_id")).Build(driver.NewPostgres())
			_, _, _ = Select().From(users).Build(driver.NewPostgres())
			_, _, _ = Delete().From(users).Build(driver.NewPostgres())
			_, _, _ = Insert("id").Into(users).From(users).Build(driver.NewPostgres())
		})
	}
	wg.Wait()
}

func TestUpdate_JoinWithoutAConditionOfItsOwn(t *testing.T) {
	q := Update(map[string]any{"a": 1}).Table(Table("t")).
		From(Join(Table("x"), "x.id = t.x_id"), Join(Table("y"), "y.id = x.y_id"))

	got, _, err := q.Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Equal(t, "update t set a=$1 from x join y on y.id = x.y_id where x.id = t.x_id", got)

	got, _, err = q.Where(criteria.And("b", "eq", 2)).Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Equal(t, "update t set a=$1 from x join y on y.id = x.y_id where (x.id = t.x_id) and ((b = $2))", got)
}

func TestInsert_TargetTakesNoAlias(t *testing.T) {
	got, _, err := Insert("a").Into(Table("t").As("x")).Values(1).Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Equal(t, "insert into t (a) values ($1)", got)
}
