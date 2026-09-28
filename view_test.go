package sql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gorib/criteria"
	"gitlab.com/gorib/sql/driver"
)

var contractMappings = map[string]string{
	"customerId": "customer_id",
	"createdAt":  "created_at",
}

func contracts() *View {
	return NewView(Table("contracts").As("c"),
		LeftJoin(Table("lines").As("l"), "l.id = c.primary_line_id"),
	).
		Column("facility", "l.facility").
		Column("unit_type", "l.unit_type").
		Column("line_kind", "l.kind")
}

const contractSource = "contracts c left join lines l on l.id = c.primary_line_id"

func buildContracts(t *testing.T, b Builder) (string, []any) {
	t.Helper()
	got, params, err := b.Build(driver.NewPostgres(), WithMappings(contractMappings))
	require.NoError(t, err)
	return got, params
}

func TestView_SelectJoinsAndPlacesEveryName(t *testing.T) {
	got, params := buildContracts(t, Select().From(contracts()).
		Returning("id", "customer_id", "line_kind").
		Where(criteria.And("facility", "eq", "msk-1").
			Add("unitType", "eq", "box-5").
			Add("customerId", "eq", 10)).
		Sort("unitType desc").
		Page(0, 20))

	assert.Equal(t, "select c.id, c.customer_id, l.kind line_kind from "+contractSource+
		" where (l.facility = $1) and (l.unit_type = $2) and (c.customer_id = $3)"+
		" order by l.unit_type desc limit 20", got)
	assert.Equal(t, []any{"msk-1", "box-5", 10}, params)
}

func TestView_UnknownNameBelongsToTheBase(t *testing.T) {
	got, _ := buildContracts(t, Select().From(contracts()).Where(criteria.And("surfaceArea", "eq", 10)))
	assert.Contains(t, got, "where (c.surface_area = $1)")
}

func TestView_QualifiedNameCannotReachPastIt(t *testing.T) {
	for name, q := range map[string]SelectQuery{
		"where": Select().From(contracts()).Where(criteria.And("l.price", "eq", 1)),
		"field": Select().From(contracts()).Where(criteria.And("facility", "eq", Field("l.price"))),
		"sort":  Select().From(contracts()).Sort("l.price desc"),
		"base":  Select().From(contracts()).Where(criteria.And("c.customer_id", "eq", 1)),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := q.Build(driver.NewPostgres(), WithMappings(contractMappings))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot reach past a view")
		})
	}
}

func TestView_MappingMayQualify(t *testing.T) {
	got, _, err := Select().From(contracts()).Where(criteria.And("price", "eq", 1)).
		Build(driver.NewPostgres(), WithMappings(map[string]string{"price": "l.price"}))
	require.NoError(t, err)
	assert.Contains(t, got, "where (l.price = $1)")
}

func TestView_TableStillTakesQualifiedNames(t *testing.T) {
	got, _, err := Select().From(Table("users").As("u"), Join(Table("orders").As("o"), "u.id = o.user_id")).
		Where(criteria.And("o.total", "gt", 1000)).Build(driver.NewPostgres())
	require.NoError(t, err)
	assert.Contains(t, got, "where (o.total > $1)")
}

func TestView_StarStaysOnTheBase(t *testing.T) {
	got, _ := buildContracts(t, Select().From(contracts()).Returning("*"))
	assert.Contains(t, got, "select c.* from ")
}

func TestView_DefaultSortIsQualified(t *testing.T) {
	for name, q := range map[string]SelectQuery{
		"none":  Select().From(contracts()).Returning("id"),
		"empty": Select().From(contracts()).Returning("id").Sort(),
		"blank": Select().From(contracts()).Returning("id").Sort(""),
	} {
		t.Run(name, func(t *testing.T) {
			q.(*selectQuery).sortByDefault([]string{"id", "created_at desc"})
			got, _ := buildContracts(t, q)
			assert.Equal(t, "select c.id from "+contractSource+" order by c.id,c.created_at desc", got)
		})
	}
}

func TestView_SubqueryKeepsItsOwnNames(t *testing.T) {
	vip := Select("id").From(Table("customers")).Where(criteria.And("status", "eq", "vip"))
	got, _ := buildContracts(t, Select().From(contracts()).Returning("id").Where(criteria.And("customerId", "in", vip)))
	assert.Contains(t, got, "where (c.customer_id in (select id from customers where (status = $1)))")
}

func TestView_CountJoinsWithoutOrdering(t *testing.T) {
	got, params := buildContracts(t, Count().From(contracts()).Where(criteria.And("facility", "eq", "msk-1")))
	assert.Equal(t, "select count(*) cnt from "+contractSource+" where (l.facility = $1)", got)
	assert.Equal(t, []any{"msk-1"}, params)
}

func TestView_ExpressionColumnInEveryPlace(t *testing.T) {
	view := contracts().
		Column("title", "coalesce(l.facility || '/' || l.kind, c.name)").
		Column("expired", "c.ended_at < now()")

	got, params := buildContracts(t, Select().From(view).
		Returning("id", "title").
		Where(criteria.And("title", "eq", "msk-1/rent").Add("expired", "eq", true)).
		Group("title").
		Sort("title desc"))

	assert.Equal(t, "select c.id, (coalesce(l.facility || '/' || l.kind, c.name)) title from "+contractSource+
		" where ((coalesce(l.facility || '/' || l.kind, c.name)) = $1) and ((c.ended_at < now()) = $2)"+
		" group by (coalesce(l.facility || '/' || l.kind, c.name))"+
		" order by (coalesce(l.facility || '/' || l.kind, c.name)) desc", got)
	assert.Equal(t, []any{"msk-1/rent", true}, params)
}

func TestView_WritesGoToTheBaseTable(t *testing.T) {
	values := map[string]any{"status": "closed"}
	for name, c := range map[string]struct {
		got  Builder
		want string
	}{
		"insert": {Insert("customer_id").Into(contracts()).Values(10).Returning("id"),
			"insert into contracts (customer_id) values ($1) returning id"},
		"update": {Update(values).Table(contracts()).Where(criteria.And("customerId", "eq", 10)).Returning("id"),
			"update contracts set status=$1 where (customer_id = $2) returning id"},
		"delete": {Delete().From(contracts()).Where(criteria.And("customerId", "eq", 10)),
			"delete from contracts where (customer_id = $1)"},
	} {
		t.Run(name, func(t *testing.T) {
			got, _ := buildContracts(t, c.got)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestView_SingleTableBuildsWhatTheTableDoes(t *testing.T) {
	customers := NewView(Table("customers"))
	byEmail := func() criteria.Expression { return criteria.And("email", "eq", "a@b.c").Add("blocked", "eq", false) }

	cases := map[string][2]Builder{
		"select": {
			Select().From(customers).Returning("id", "email").Where(byEmail()).Sort("createdAt desc").Page(1, 10),
			Select().From(Table("customers")).Returning("id", "email").Where(byEmail()).Sort("createdAt desc").Page(1, 10),
		},
		"count": {
			Count().From(customers).Where(byEmail()),
			Count().From(Table("customers")).Where(byEmail()),
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotParams := buildContracts(t, c[0])
			want, wantParams := buildContracts(t, c[1])
			assert.Equal(t, want, got)
			assert.Equal(t, wantParams, gotParams)
		})
	}
}

func TestView_ColumnDerivesANewView(t *testing.T) {
	v := contracts()
	_ = v.Column("period", "l.period")
	got, _ := buildContracts(t, Select().From(v).Returning("period"))
	assert.Contains(t, got, "select c.period from ")
}

func TestView_QueriesDoNotShareState(t *testing.T) {
	v := contracts()
	_, _ = buildContracts(t, Delete().From(v))
	_, _ = buildContracts(t, Insert("customer_id").Into(v).Values(10))
	got, _ := buildContracts(t, Select().From(v).Returning("id"))
	assert.Equal(t, "select c.id from "+contractSource, got)
}
