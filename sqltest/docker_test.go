package sqltest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvWinsOverTheContainer(t *testing.T) {
	t.Setenv("TEST_DSN", "postgres://host:5432/from_env")
	p := New(Env("TEST_DSN"), Docker("postgres:18-alpine"), Database("ignored"))

	require.NoError(t, p.resolve(t.Context()))
	assert.Equal(t, "postgres://host:5432/from_env", p.dsn)
}

func TestDsnWinsOverTheContainer(t *testing.T) {
	p := New(Docker("postgres:18-alpine"), Database("ignored"), Dsn("postgres://host:5432/explicit"))

	require.NoError(t, p.resolve(t.Context()))
	assert.Equal(t, "postgres://host:5432/explicit", p.dsn)
}

func TestDockerWithoutADatabaseNameSaysSo(t *testing.T) {
	p := New(Docker("postgres:18-alpine"))

	err := p.resolve(t.Context())

	require.ErrorIs(t, err, errNoDatabase)
	assert.Contains(t, err.Error(), "sqltest.Database")
}

func TestContainerAddressCarriesNoCredential(t *testing.T) {
	c := &container{port: 55432}

	dsn := c.dsn("bastion_test")

	assert.Equal(t, "postgres://postgres@127.0.0.1:55432/bastion_test?sslmode=disable", dsn)
	assert.NotContains(t, dsn, ":@")
}

func TestDefaultsAreSharedAcrossServices(t *testing.T) {
	first := New(Docker("postgres:18-alpine"), Database("warehouse_test"))
	second := New(Docker("postgres:18-alpine"), Database("bastion_test"))

	assert.Equal(t, first.container.name, second.container.name)
	assert.Equal(t, first.container.port, second.container.port)
	assert.Equal(t, defaultContainer, first.container.name)
	assert.Equal(t, defaultPort, first.container.port)
}

func TestContainerOptionsOverrideTheDefaults(t *testing.T) {
	p := New(Docker("postgres:13", Container("sherpa-pg"), Port(15433), Runtime("podman")), Database("x"))

	assert.Equal(t, "postgres:13", p.container.image)
	assert.Equal(t, "sherpa-pg", p.container.name)
	assert.Equal(t, 15433, p.container.port)
	assert.Equal(t, "podman", p.container.runtime)
}

func TestMissingRuntimePointsAtTheOtherOptions(t *testing.T) {
	c := &container{runtime: "definitely-not-a-container-runtime"}

	_, err := c.binary()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-a-container-runtime")
	assert.Contains(t, err.Error(), "sqltest.Dsn")
}

func TestDownIsANoopWithoutEphemeral(t *testing.T) {
	p := New(Dsn("postgres://host:5432/db"))

	require.NoError(t, p.Down(t.Context()))
}

func TestDownIsANoopBeforeProvision(t *testing.T) {
	p := New(Env("UNSET_TEST_DSN"), Ephemeral())

	require.NoError(t, p.Down(t.Context()))
}

func TestAwaitIsSkippedForAGivenAddress(t *testing.T) {
	p := New(Dsn("postgres://postgres@127.0.0.1:1/db?sslmode=disable"))

	require.NoError(t, p.resolve(t.Context()))
	require.NoError(t, p.await(t.Context()))
}

// The documented configuration lists both, and CI resolves on the env branch:
// waiting for a container nobody started would blame it for an absent server.
func TestAwaitIsSkippedWhenEnvWinsOverAConfiguredContainer(t *testing.T) {
	t.Setenv("TEST_DSN", "postgres://postgres@127.0.0.1:1/db?sslmode=disable")
	p := New(Env("TEST_DSN"), Docker("postgres:18-alpine"), Database("ignored"))

	require.NoError(t, p.resolve(t.Context()))

	assert.False(t, p.started, "nothing was started, so nothing should be waited for")

	started := time.Now()
	require.NoError(t, p.await(t.Context()))
	assert.Less(t, time.Since(started), time.Second)
}
