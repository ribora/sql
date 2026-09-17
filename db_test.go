package sql

import (
	"context"
	dbSql "database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"gitlab.com/gorib/hilton"
)

// An unbounded value here is what breaks the histogram.
func TestErrorType(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"success", nil, ""},
		{"unique violation", &pgconn.PgError{Code: "23505"}, "conflict"},
		{"foreign key", &pgconn.PgError{Code: "23503"}, "conflict"},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, "deadlock"},
		{"not null", &pgconn.PgError{Code: "23502"}, "bad_request"},
		{"any other pg code", &pgconn.PgError{Code: "42601"}, "internal"},
		{"no rows", dbSql.ErrNoRows, "no_rows"},
		{"no rows from pgx", pgx.ErrNoRows, "no_rows"},
		{"connect", fmt.Errorf("%w: dial tcp 10.0.0.5:5432", ErrCannotConnectDb), "connect"},
		{"commit", fmt.Errorf("%w: x", ErrCannotCommitTransaction), "tx"},
		{"rollback", fmt.Errorf("%w: x", ErrCannotRollbackTransaction), "tx"},
		{"cancelled", context.Canceled, otherErrorType},
		{"scan", errors.New("sql: Scan error on column index 3"), otherErrorType},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errorType(c.err); got != c.want {
				t.Errorf("errorType(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}

// The message carries the DSN and the row values.
func TestErrorTypeIgnoresTheMessage(t *testing.T) {
	first := errorType(fmt.Errorf("%w: host=a.internal", ErrCannotConnectDb))
	second := errorType(fmt.Errorf("%w: host=b.internal", ErrCannotConnectDb))
	if first != second {
		t.Errorf("%q != %q", first, second)
	}
}

// A key present on every success is a key nobody can group by.
func TestSuccessCarriesNoErrorType(t *testing.T) {
	rec := hilton.Recorder()
	duration := rec.Meter(instrumentationName).Histogram("db.client.operation.duration")

	recordDb(context.Background(), duration, "postgresql", "query", 0.1, nil)

	for _, m := range rec.Measurements() {
		for _, a := range m.Attributes {
			if a.Key() == "error.type" {
				t.Errorf("error.type = %v, want it absent", a.Value())
			}
		}
	}
}
