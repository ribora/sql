package sqltest

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDsnWinsOverTheEnvironment(t *testing.T) {
	t.Setenv("TEST_DSN", "postgres://host:5432/from_env")
	p := New(Env("TEST_DSN"), Dsn("postgres://host:5432/explicit"))

	require.NoError(t, p.resolve(t.Context()))
	assert.Equal(t, "postgres://host:5432/explicit", p.dsn)
}

func TestEnvKeysAreTriedInOrder(t *testing.T) {
	t.Setenv("SECOND", "postgres://host:5432/second")
	p := New(Env("FIRST", "SECOND"))

	require.NoError(t, p.resolve(t.Context()))
	assert.Equal(t, "postgres://host:5432/second", p.dsn)
}

func TestMissingAddressNamesTheVariables(t *testing.T) {
	p := New(Env("BASTION_TEST_DSN", "FALLBACK_DSN"))

	err := p.resolve(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BASTION_TEST_DSN or FALLBACK_DSN")
}

func TestMissingAddressWithoutEnvKeysPointsAtTheOptions(t *testing.T) {
	err := New().resolve(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sqltest.Dsn")
}

func TestAddressDropsTheCredentials(t *testing.T) {
	reported := address("postgres://postgres:fake-pw@127.0.0.1:15432/bastion_test?sslmode=disable")

	assert.Equal(t, "127.0.0.1:15432/bastion_test", reported)
	assert.NotContains(t, reported, "fake-pw")
	assert.NotContains(t, reported, "postgres:")
}

func TestUnreachableKeepsTheCredentialsOut(t *testing.T) {
	err := New().unreachable("postgres://postgres:fake-pw@127.0.0.1:15432/bastion_test", assert.AnError)

	assert.NotContains(t, err.Error(), "fake-pw")
	assert.Contains(t, err.Error(), "127.0.0.1:15432/bastion_test")
}

func TestAddressSurvivesAnUnparsableDsn(t *testing.T) {
	assert.Equal(t, "the configured address", address("://nonsense"))
}

func TestBookkeepingTablesAreNeverTruncated(t *testing.T) {
	for _, name := range []string{"goose_db_version", "migrations", "schema_migrations"} {
		assert.True(t, slices.Contains(bookkeeping, name), name)
	}
	assert.False(t, slices.Contains(bookkeeping, "guards"))
}

func TestResetIsANoopUntilTablesAreKnown(t *testing.T) {
	require.NoError(t, New().Reset(t.Context()))
}

func TestDriverIsFixedToPostgres(t *testing.T) {
	assert.Equal(t, "pgx", New().host.driver())
}

func TestNameIsReportedForTheSeam(t *testing.T) {
	assert.Equal(t, "postgres", New().Name())
}

func TestTablesAccumulate(t *testing.T) {
	p := New(Tables("guards"), Tables("units", "facilities"))

	assert.Equal(t, "guards units facilities", strings.Join(p.tables, " "))
}
