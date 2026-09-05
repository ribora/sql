package sqltest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pgx accepts this form, so anyone may set <SERVICE>_TEST_DSN in it — and
// net/url puts the whole string, password included, where the name belongs.
const keywordDsn = "host=127.0.0.1 port=15432 dbname=warehouse_test user=postgres password=fake-pw sslmode=disable"

func TestAddressRedactsBothDsnForms(t *testing.T) {
	for name, dsn := range map[string]string{
		"url":     "postgres://postgres:fake-pw@127.0.0.1:15432/warehouse_test?sslmode=disable",
		"keyword": keywordDsn,
	} {
		t.Run(name, func(t *testing.T) {
			reported := address(dsn)

			assert.Equal(t, "127.0.0.1:15432/warehouse_test", reported)
			assert.NotContains(t, reported, "fake-pw")
			assert.NotContains(t, unreachable(dsn, assert.AnError).Error(), "fake-pw")
		})
	}
}

func TestDatabaseNameReadsBothDsnForms(t *testing.T) {
	for name, dsn := range map[string]string{
		"url":     "postgres://postgres@127.0.0.1:15432/warehouse_test",
		"keyword": keywordDsn,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := databaseName(dsn)

			require.NoError(t, err)
			assert.Equal(t, "warehouse_test", got)
		})
	}
}

// Losing sslmode or a timeout would silently change how the test connects.
func TestWithDatabaseKeepsEveryOtherOption(t *testing.T) {
	url, err := withDatabase("postgres://postgres@127.0.0.1:15432/warehouse_test?sslmode=disable", "postgres")
	require.NoError(t, err)
	assert.Equal(t, "postgres://postgres@127.0.0.1:15432/postgres?sslmode=disable", url)

	keyword, err := withDatabase(keywordDsn, "warehouse_test_42")
	require.NoError(t, err)

	name, err := databaseName(keyword)
	require.NoError(t, err)
	assert.Equal(t, "warehouse_test_42", name)
	for _, kept := range []string{"host=127.0.0.1", "port=15432", "user=postgres", "sslmode=disable"} {
		assert.Contains(t, keyword, kept)
	}
	assert.Equal(t, 1, strings.Count(keyword, "dbname="), "the old dbname survived alongside the new one")
}

func TestWithDatabaseRefusesNonsense(t *testing.T) {
	_, err := withDatabase("://nonsense", "postgres")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "nonsense", "the message must not quote the address back")
}

// Every call site needs a live database, so nothing else covers this. camelCase
// is the case that bites.
func TestQuoteIdent(t *testing.T) {
	for name, want := range map[string]string{
		"guards":       `"guards"`,
		"userProfile":  `"userProfile"`,
		"Order":        `"Order"`,
		`we"ird`:       `"we""ird"`,
		"":             `""`,
		"warehouse_42": `"warehouse_42"`,
	} {
		assert.Equal(t, want, quoteIdent(name), name)
	}
}

func TestQuotingOnlyWhenTheValueNeedsIt(t *testing.T) {
	assert.Equal(t, "plain", quoteKeyword("plain"))
	assert.Equal(t, `'two words'`, quoteKeyword("two words"))
	assert.Equal(t, `'it\'s'`, quoteKeyword("it's"))
	assert.Equal(t, `''`, quoteKeyword(""))
}

func TestSplitKeywordsKeepsQuotedSpaces(t *testing.T) {
	fields := splitKeywords("host=127.0.0.1 dbname='two words' user=postgres")

	assert.Equal(t, []string{"host=127.0.0.1", "dbname='two words'", "user=postgres"}, fields)
}

// No handle before Provision and none after Down; both used to panic.
func TestResetSaysTheDatabaseIsNotOpen(t *testing.T) {
	p := New(Dsn("postgres://host:5432/db"))
	p.tables = []string{"guards"}

	err := p.Reset(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not open")
}
