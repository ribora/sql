package sqltest

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEphemeralGivesTheProcessItsOwnDatabase(t *testing.T) {
	p := New(Dsn("postgres://host:5432/warehouse_test?sslmode=disable"), Ephemeral())

	require.NoError(t, p.resolve(t.Context()))
	require.NoError(t, p.personalize())

	name, err := p.databaseName()
	require.NoError(t, err)
	assert.Equal(t, "warehouse_test_"+strconv.Itoa(os.Getpid()), name)
	assert.Equal(t, "warehouse_test", p.base, "the base is kept, for the sweep")
}

func TestWithoutEphemeralTheNameIsTakenAsGiven(t *testing.T) {
	p := New(Dsn("postgres://host:5432/warehouse_test?sslmode=disable"))

	require.NoError(t, p.resolve(t.Context()))
	require.NoError(t, p.personalize())

	name, err := p.databaseName()
	require.NoError(t, err)
	assert.Equal(t, "warehouse_test", name)
	assert.Empty(t, p.base)
}

func TestPersonalizeDoesNotStack(t *testing.T) {
	p := New(Dsn("postgres://host:5432/warehouse_test?sslmode=disable"), Ephemeral())
	require.NoError(t, p.resolve(t.Context()))

	require.NoError(t, p.personalize())
	first, err := p.databaseName()
	require.NoError(t, err)
	require.NoError(t, p.personalize())
	second, err := p.databaseName()
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestPersonalizeKeepsTheRestOfTheAddress(t *testing.T) {
	p := New(Dsn("postgres://user@host:5432/db?sslmode=disable"), Ephemeral())
	require.NoError(t, p.resolve(t.Context()))

	require.NoError(t, p.personalize())

	assert.Contains(t, p.dsn, "sslmode=disable")
	assert.Contains(t, p.dsn, "user@host:5432")
}

func TestSuffixAppliesToANameFromTheDsn(t *testing.T) {
	p := New(Dsn("postgres://host:5432/warehouse_test?sslmode=disable"), Database("ignored"), Suffix("order"))
	require.NoError(t, p.resolve(t.Context()))

	require.NoError(t, p.separate())
	require.NoError(t, p.personalize())

	name, err := p.databaseName()
	require.NoError(t, err)
	assert.Equal(t, "warehouse_test_order", name)
}

func TestSuffixIsPartOfTheSweptBase(t *testing.T) {
	p := New(Dsn("postgres://host:5432/warehouse_test?sslmode=disable"), Suffix("order"), Ephemeral())
	require.NoError(t, p.resolve(t.Context()))

	require.NoError(t, p.separate())
	require.NoError(t, p.separate())
	require.NoError(t, p.personalize())

	name, err := p.databaseName()
	require.NoError(t, err)
	assert.Equal(t, "warehouse_test_order_"+strconv.Itoa(os.Getpid()), name)
	assert.Equal(t, "warehouse_test_order", p.base)
	assert.False(t, sweepable(name, "warehouse_test"), "the unsuffixed run's sweep must not take it")
}

func TestSweepableRecognisesOnlyOurOwnNames(t *testing.T) {
	for name, want := range map[string]bool{
		"warehouse_test_1":       true,
		"warehouse_test_48213":   true,
		"warehouse_test":         false, // the base itself — somebody's real database
		"warehouse_test_":        false,
		"warehouse_test_abc":     false,
		"warehouse_test_1a":      false,
		"warehouse_test_1_2":     false,
		"warehouseXtest_1":       false, // would have matched a LIKE pattern
		"other_test_1":           false,
		"warehouse_test_backup1": false,
		"postgres":               false,
	} {
		assert.Equal(t, want, sweepable(name, "warehouse_test"), name)
	}
}

func TestSweepableIsFalseWithoutABase(t *testing.T) {
	assert.False(t, sweepable("warehouse_test_1", ""))
}

func TestCreatedAtReadsOurOwnMark(t *testing.T) {
	created, ok := createdAt(createdMark + "2026-09-04T10:00:00Z")

	require.True(t, ok)
	assert.Equal(t, 2026, created.Year())
	assert.Equal(t, time.September, created.Month())
}

func TestCreatedAtRejectsAnythingElse(t *testing.T) {
	for _, comment := range []string{
		"",
		"a database somebody documented",
		createdMark,
		createdMark + "not a timestamp",
		"2026-09-04T10:00:00Z",
	} {
		_, ok := createdAt(comment)
		assert.False(t, ok, comment)
	}
}

func TestSweepThresholdIsGenerous(t *testing.T) {
	assert.GreaterOrEqual(t, sweepAfter, time.Hour)
}
