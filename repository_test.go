package sql

import (
	"context"
	dbSql "database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gorib/criteria"
	"gitlab.com/gorib/sql/driver"
)

type person struct {
	ID   int
	Name string
}

func (p *person) Scan() []any { return []any{&p.ID, &p.Name} }

func (p *person) Returns() []string { return []string{"id", "name"} }

type nameless struct {
	ID int
}

func (n *nameless) Scan() []any { return []any{&n.ID} }

func (n *nameless) Returns() []string { return nil }

type method struct {
	name string
	op   string
	call func(context.Context, *repository[person]) error
}

func methods() []method {
	return []method{
		{"Get", "queryRow", func(ctx context.Context, r *repository[person]) error {
			_, err := r.Get(ctx, Select().From(Table("t")))
			return err
		}},
		{"Select", "query", func(ctx context.Context, r *repository[person]) error {
			_, err := r.Select(ctx, Select().From(Table("t")))
			return err
		}},
		{"Delete", "exec", func(ctx context.Context, r *repository[person]) error {
			return r.Delete(ctx, Delete().From(Table("t")))
		}},
		{"Count", "queryRow", func(ctx context.Context, r *repository[person]) error {
			_, err := r.Count(ctx, Count().From(Table("t")))
			return err
		}},
	}
}

func newFakeRepository(t *testing.T, drv Driver) (*repository[person], *fakeDb) {
	t.Helper()
	db := newFakeDb(drv)
	repo, err := NewRepository[person, *person](db)
	require.NoError(t, err)
	return repo, db
}

func TestRepository_BeginsAndCommitsItsOwnTransaction(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			repo, db := newFakeRepository(t, driver.NewPostgres())
			require.NoError(t, m.call(context.Background(), repo))
			// No rollback: a successful commit marks the transaction done and
			// the unconditional rollback that follows it finds nothing to do.
			assert.Equal(t, []string{"connect", "begin", m.op, "commit"}, db.calls.ops())
		})
	}
}

func TestRepository_JoinsCallerTransactionWithoutEndingIt(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			repo, db := newFakeRepository(t, driver.NewPostgres())
			ctx, commit, rollback, err := repo.Begin(context.Background())
			require.NoError(t, err)

			require.NoError(t, m.call(ctx, repo))
			assert.Equal(t, []string{"connect", "begin", m.op}, db.calls.ops())

			require.NoError(t, commit())
			require.NoError(t, rollback())
			assert.Equal(t, []string{"connect", "begin", m.op, "commit"}, db.calls.ops())
		})
	}
}

func TestRepository_RollsBackWhenTheQueryFails(t *testing.T) {
	boom := errors.New("boom")
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			repo, db := newFakeRepository(t, driver.NewPostgres())
			db.tx.fail = failEveryQuery(boom)

			err := m.call(context.Background(), repo)
			require.ErrorIs(t, err, boom)
			assert.Equal(t, []string{"connect", "begin", m.op, "rollback"}, db.calls.ops())
		})
	}
}

// commit() marks the transaction done only when it succeeds, so the rollback
// that always follows it is what closes a transaction whose commit failed.
func TestRepository_FailedCommitIsRolledBack(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			repo, db := newFakeRepository(t, driver.NewPostgres())
			db.tx.fail = failOp("commit", errors.New("connection lost"))

			err := m.call(context.Background(), repo)
			require.ErrorIs(t, err, ErrCannotCommitTransaction)
			assert.Equal(t, []string{"connect", "begin", m.op, "commit", "rollback"}, db.calls.ops())
		})
	}
}

func TestRepository_CommitErrorWinsOverRollbackError(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.fail = func(op, _ string) error {
		switch op {
		case "commit":
			return errors.New("connection lost")
		case "rollback":
			return errors.New("connection still lost")
		}
		return nil
	}

	err := repo.Delete(context.Background(), Delete().From(Table("t")))
	require.ErrorIs(t, err, ErrCannotCommitTransaction)
	assert.NotErrorIs(t, err, ErrCannotRollbackTransaction)
}

func TestRepository_QueryErrorWinsOverRollbackError(t *testing.T) {
	boom := errors.New("boom")
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.fail = func(op, _ string) error {
		switch op {
		case "exec":
			return boom
		case "rollback":
			return errors.New("connection lost")
		}
		return nil
	}

	err := repo.Delete(context.Background(), Delete().From(Table("t")))
	require.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, ErrCannotRollbackTransaction)
}

func TestRepository_ReportsBeginFailure(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			repo, db := newFakeRepository(t, driver.NewPostgres())
			db.beginErr = errors.New("too many connections")

			err := m.call(context.Background(), repo)
			require.ErrorIs(t, err, ErrCannotStartTx)
			assert.Equal(t, []string{"connect", "begin"}, db.calls.ops())
		})
	}
}

func TestRepository_ReportsConnectFailure(t *testing.T) {
	down := errors.New("database is down")
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.connErr = down

	err := repo.Delete(context.Background(), Delete().From(Table("t")))
	require.ErrorIs(t, err, down)
	assert.Equal(t, []string{"connect"}, db.calls.ops())
}

func TestRepository_JoinsAtSavepointDepth(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	outer, commitOuter, rollbackOuter, err := repo.Begin(context.Background())
	require.NoError(t, err)
	inner, commitInner, rollbackInner, err := repo.Begin(outer)
	require.NoError(t, err)

	require.NoError(t, repo.Delete(inner, Delete().From(Table("t"))))

	require.NoError(t, commitInner())
	require.NoError(t, rollbackInner())
	require.NoError(t, commitOuter())
	require.NoError(t, rollbackOuter())

	assert.Equal(t, []string{
		"connect",
		"begin",
		"exec: savepoint level_1",
		"exec: delete from t",
		"exec: release savepoint level_1",
		"commit",
	}, db.calls.trace())
}

func TestRepository_FailedSavepointReleaseIsRolledBack(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.fail = failStatement("release savepoint", errors.New("no such savepoint"))

	outer, _, _, err := repo.Begin(context.Background())
	require.NoError(t, err)
	inner, commitInner, rollbackInner, err := repo.Begin(outer)
	require.NoError(t, err)

	require.NoError(t, repo.Delete(inner, Delete().From(Table("t"))))
	require.ErrorIs(t, commitInner(), ErrCannotReleaseSavepoint)
	require.NoError(t, rollbackInner())

	assert.Equal(t, []string{
		"connect",
		"begin",
		"exec: savepoint level_1",
		"exec: delete from t",
		"exec: release savepoint level_1",
		"exec: rollback to savepoint level_1",
	}, db.calls.trace())
}

func TestGet_ScansTheModel(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.row = []any{7, "ann"}

	model, err := repo.Get(context.Background(), Select().From(Table("t")))
	require.NoError(t, err)
	assert.Equal(t, &person{ID: 7, Name: "ann"}, model)
}

func TestSelect_ScansEveryRow(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.rows = [][]any{{1, "ann"}, {2, "bob"}}

	models, err := repo.Select(context.Background(), Select().From(Table("t")))
	require.NoError(t, err)
	assert.Equal(t, []*person{{ID: 1, Name: "ann"}, {ID: 2, Name: "bob"}}, models)
}

func TestSelect_RollsBackWhenARowFailsToScan(t *testing.T) {
	boom := errors.New("boom")
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.rows = [][]any{{1, "ann"}}
	db.tx.fail = failOp("scan", boom)

	models, err := repo.Select(context.Background(), Select().From(Table("t")))
	require.ErrorIs(t, err, boom)
	assert.Nil(t, models)
	assert.Equal(t, []string{"connect", "begin", "query", "rollback"}, db.calls.ops())
}

func TestSelect_RollsBackWhenTheCursorBreaks(t *testing.T) {
	dropped := errors.New("connection dropped")
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.rows = [][]any{{1, "ann"}, {2, "bob"}}
	db.tx.fail = failOp("rows", dropped)

	models, err := repo.Select(context.Background(), Select().From(Table("t")))
	require.ErrorIs(t, err, dropped)
	assert.Nil(t, models)
	assert.Equal(t, []string{"connect", "begin", "query", "rollback"}, db.calls.ops())
}

func TestCount_ScansTheTotal(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.row = []any{7}

	total, err := repo.Count(context.Background(), Count().From(Table("t")))
	require.NoError(t, err)
	assert.Equal(t, 7, total)
}

func TestCount_TreatsNoRowsAsZero(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.fail = failOp("queryRow", dbSql.ErrNoRows)

	total, err := repo.Count(context.Background(), Count().From(Table("t")))
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Equal(t, []string{"connect", "begin", "queryRow", "commit"}, db.calls.ops())
}

func TestGet_ClassifiesNoRows(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.fail = failOp("queryRow", dbSql.ErrNoRows)

	_, err := repo.Get(context.Background(), Select().From(Table("t")))
	require.ErrorIs(t, err, ErrNoRows)
	assert.Equal(t, []string{"connect", "begin", "queryRow", "rollback"}, db.calls.ops())
}

// The commit wrapper flattens its cause, so a caller watching for
// ErrQueryFailed will not see a commit failure.
func TestRepository_DoesNotClassifyCommitFailure(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.fail = failOp("commit", errors.New("connection lost"))

	err := repo.Delete(context.Background(), Delete().From(Table("t")))
	require.ErrorIs(t, err, ErrCannotCommitTransaction)
	assert.NotErrorIs(t, err, ErrQueryFailed)
}

func TestGet_ClassifiesTheQueryErrorThroughAFailingRollback(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())
	db.tx.fail = func(op, _ string) error {
		switch op {
		case "queryRow":
			return dbSql.ErrNoRows
		case "rollback":
			return errors.New("connection lost")
		}
		return nil
	}

	_, err := repo.Get(context.Background(), Select().From(Table("t")))
	require.ErrorIs(t, err, ErrNoRows)
	assert.NotErrorIs(t, err, ErrCannotRollbackTransaction)
}

func TestRepository_KeepsResultsWhenTheCommitFails(t *testing.T) {
	ctx := context.Background()

	t.Run("Get", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewPostgres())
		db.tx.row = []any{7, "ann"}
		db.tx.fail = failOp("commit", errors.New("connection lost"))

		model, err := repo.Get(ctx, Select().From(Table("t")))
		require.Error(t, err)
		assert.Equal(t, &person{ID: 7, Name: "ann"}, model)
	})

	t.Run("Select", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewPostgres())
		db.tx.rows = [][]any{{1, "ann"}}
		db.tx.fail = failOp("commit", errors.New("connection lost"))

		models, err := repo.Select(ctx, Select().From(Table("t")))
		require.Error(t, err)
		assert.Equal(t, []*person{{ID: 1, Name: "ann"}}, models)
	})

	t.Run("Count", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewPostgres())
		db.tx.row = []any{7}
		db.tx.fail = failOp("commit", errors.New("connection lost"))

		total, err := repo.Count(ctx, Count().From(Table("t")))
		require.Error(t, err)
		assert.Equal(t, 7, total)
	})
}

type reader struct {
	name string
	op   string
	call func(context.Context, *repository[person], Builder) error
}

func readers() []reader {
	return []reader{
		{"Get", "queryRow", func(ctx context.Context, r *repository[person], b Builder) error {
			_, err := r.Get(ctx, b)
			return err
		}},
		{"Select", "query", func(ctx context.Context, r *repository[person], b Builder) error {
			_, err := r.Select(ctx, b)
			return err
		}},
	}
}

func TestRepository_AsksForTheModelColumns(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		builder   func() Builder
		statement string
	}{
		{"select", func() Builder {
			return Select().From(Table("t"))
		}, "select id, name from t"},
		{"insert", func() Builder {
			return Insert("name").Into(Table("t")).Values("ann")
		}, "insert into t (name) values ($1) returning id,name"},
		{"update", func() Builder {
			return Update(map[string]any{"name": "ann"}).Table(Table("t"))
		}, "update t set name=$1 returning id,name"},
		// A builder with no Returning method of its own is left alone.
		{"other", func() Builder {
			return Count().From(Table("t"))
		}, "select count(*) cnt from t"},
	}

	for _, m := range readers() {
		for _, c := range cases {
			t.Run(m.name+"/"+c.name, func(t *testing.T) {
				repo, db := newFakeRepository(t, driver.NewPostgres())
				require.NoError(t, m.call(ctx, repo, c.builder()))
				statement, _ := db.calls.first(m.op)
				assert.Equal(t, c.statement, statement.query)
			})
		}
	}
}

func TestRepository_FallsBackToEveryColumn(t *testing.T) {
	ctx := context.Background()

	t.Run("Get", func(t *testing.T) {
		db := newFakeDb(driver.NewPostgres())
		repo, err := NewRepository[nameless, *nameless](db)
		require.NoError(t, err)

		_, err = repo.Get(ctx, Select().From(Table("t")))
		require.NoError(t, err)
		statement, _ := db.calls.first("queryRow")
		assert.Equal(t, "select * from t", statement.query)
	})

	t.Run("Select", func(t *testing.T) {
		db := newFakeDb(driver.NewPostgres())
		repo, err := NewRepository[nameless, *nameless](db)
		require.NoError(t, err)

		_, err = repo.Select(ctx, Select().From(Table("t")))
		require.NoError(t, err)
		statement, _ := db.calls.first("query")
		assert.Equal(t, "select * from t", statement.query)
	})
}

func TestRepository_ExecutesWritesOnADriverWithoutReturning(t *testing.T) {
	ctx := context.Background()

	t.Run("Get", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewMysql())
		model, err := repo.Get(ctx, Insert("name").Into(Table("t")).Values("ann"))
		require.NoError(t, err)
		assert.Equal(t, &person{}, model)
		assert.Equal(t, []string{"connect", "begin", "exec", "commit"}, db.calls.ops())
		statement, _ := db.calls.first("exec")
		assert.Equal(t, "insert into t (name) values (?)", statement.query)
	})

	t.Run("Select", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewMysql())
		models, err := repo.Select(ctx, Insert("name").Into(Table("t")).Values("ann"))
		require.NoError(t, err)
		assert.Nil(t, models)
		assert.Equal(t, []string{"connect", "begin", "exec", "commit"}, db.calls.ops())
		statement, _ := db.calls.first("exec")
		assert.Equal(t, "insert into t (name) values (?)", statement.query)
	})

	t.Run("update", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewMysql())
		_, err := repo.Get(ctx, Update(map[string]any{"name": "ann"}).Table(Table("t")))
		require.NoError(t, err)
		statement, _ := db.calls.first("exec")
		assert.Equal(t, "update t set name=?", statement.query)
	})

	t.Run("read", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewMysql())
		_, err := repo.Get(ctx, Select().From(Table("t")))
		require.NoError(t, err)
		assert.Equal(t, []string{"connect", "begin", "queryRow", "commit"}, db.calls.ops())
	})
}

type upper string

func (u upper) Value() (Value, error) { return strings.ToUpper(string(u)), nil }

type unusable struct{}

func (unusable) Value() (Value, error) { return nil, errors.New("cannot be used as a value") }

func TestRepository_ResolvesArgumentsThroughValuer(t *testing.T) {
	repo, db := newFakeRepository(t, driver.NewPostgres())

	err := repo.Delete(context.Background(), Delete().From(Table("t")).Where(criteria.And(Eq("name", upper("ann")))))
	require.NoError(t, err)
	statement, _ := db.calls.first("exec")
	assert.Equal(t, []any{"ANN"}, statement.args)
}

func TestRepository_RollsBackWhenAnArgumentCannotBeResolved(t *testing.T) {
	expr := criteria.And(Eq("name", unusable{}))
	cases := []struct {
		name string
		call func(context.Context, *repository[person]) error
	}{
		{"Get", func(ctx context.Context, r *repository[person]) error {
			_, err := r.Get(ctx, Select().From(Table("t")).Where(expr))
			return err
		}},
		{"Select", func(ctx context.Context, r *repository[person]) error {
			_, err := r.Select(ctx, Select().From(Table("t")).Where(expr))
			return err
		}},
		{"Delete", func(ctx context.Context, r *repository[person]) error {
			return r.Delete(ctx, Delete().From(Table("t")).Where(expr))
		}},
		{"Count", func(ctx context.Context, r *repository[person]) error {
			_, err := r.Count(ctx, Count().From(Table("t")).Where(expr))
			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, db := newFakeRepository(t, driver.NewPostgres())
			err := c.call(context.Background(), repo)
			require.EqualError(t, err, "cannot be used as a value")
			assert.Equal(t, []string{"connect", "begin", "rollback"}, db.calls.ops())
		})
	}
}

// The transaction is opened before the query is built, so there is one to
// roll back even when nothing ran.
func TestRepository_RollsBackWhenTheQueryCannotBeBuilt(t *testing.T) {
	ctx := context.Background()

	t.Run("Get", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewPostgres())
		model, err := repo.Get(ctx, Select())
		require.EqualError(t, err, "no table specified for select (, [])")
		assert.NotNil(t, model)
		assert.Equal(t, []string{"connect", "begin", "rollback"}, db.calls.ops())
	})

	t.Run("Select", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewPostgres())
		models, err := repo.Select(ctx, Select())
		require.EqualError(t, err, "no table specified for select (, [])")
		assert.Nil(t, models)
		assert.Equal(t, []string{"connect", "begin", "rollback"}, db.calls.ops())
	})

	t.Run("Delete", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewPostgres())
		err := repo.Delete(ctx, Delete())
		require.EqualError(t, err, "no table for delete (, [])")
		assert.Equal(t, []string{"connect", "begin", "rollback"}, db.calls.ops())
	})

	t.Run("Count", func(t *testing.T) {
		repo, db := newFakeRepository(t, driver.NewPostgres())
		total, err := repo.Count(ctx, Count())
		require.EqualError(t, err, "no table specified for select (, [])")
		assert.Equal(t, 0, total)
		assert.Equal(t, []string{"connect", "begin", "rollback"}, db.calls.ops())
	})
}

func TestNewRepository_RejectsAValueReceiver(t *testing.T) {
	_, err := NewRepository[byValue, *byValue](newFakeDb(driver.NewPostgres()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "value receiver but pointer receiver required")
}

type byValue struct {
	ID int
}

func (b byValue) Scan() []any { return []any{&b.ID} }

func (b byValue) Returns() []string { return nil }

func TestNewRepository_ReportsAFailingOption(t *testing.T) {
	broken := func(*repositoryConfig) error { return errors.New("bad option") }

	_, err := NewRepository[person, *person](newFakeDb(driver.NewPostgres()), broken)
	require.EqualError(t, err, "bad option")
}

func TestNewRepository_CollectsMappings(t *testing.T) {
	db := newFakeDb(driver.NewPostgres())
	repo, err := NewRepository[mapped, *mapped](db, WithExtraMapping(map[string]string{"extra": "e.extra"}))
	require.NoError(t, err)

	err = repo.Delete(context.Background(), Delete().From(Table("t")).Where(criteria.And(
		Eq("own", 1),
		Eq("extra", 2),
	)))
	require.NoError(t, err)
	statement, _ := db.calls.first("exec")
	assert.Contains(t, statement.query, "o.own = $1")
	assert.Contains(t, statement.query, "e.extra = $2")
}

type mapped struct {
	ID int
}

func (m *mapped) Scan() []any { return []any{&m.ID} }

func (m *mapped) Returns() []string { return nil }

func (m *mapped) Mappings() map[string]string { return map[string]string{"own": "o.own"} }

type sortedPerson struct{ person }

func (p *sortedPerson) DefaultSort() []string { return []string{"name", "age desc nulls last"} }

func TestRepository_ModelSortsByDefault(t *testing.T) {
	ctx := context.Background()
	db := newFakeDb(driver.NewPostgres())
	repo, err := NewRepository[sortedPerson](db)
	require.NoError(t, err)

	_, err = repo.Select(ctx, Select().From(Table("t")))
	require.NoError(t, err)
	_, err = repo.Select(ctx, Select().From(Table("t")).Sort("id desc"))
	require.NoError(t, err)

	var queries []string
	for _, e := range db.calls.entries {
		if e.op == "query" {
			queries = append(queries, e.query)
		}
	}
	assert.Equal(t, []string{
		"select id, name from t order by name,age desc nulls last",
		"select id, name from t order by id desc",
	}, queries)
}
