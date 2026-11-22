package sql

import (
	"context"
	dbSql "database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ribora/hilton"
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
		{"undefined column", &pgconn.PgError{Code: "42703"}, "no_column"},
		{"unknown column in mysql", &mysql.MySQLError{Number: 1054}, "no_column"},
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

func TestNoSuchColumnInSqlite(t *testing.T) {
	db, err := dbSql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec("create table t (id int)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("select status from t")
	if !errors.Is(NewError(err), ErrNoColumn) {
		t.Errorf("NewError(%v) is not ErrNoColumn", err)
	}

	_, err = db.Exec("select from t")
	if errors.Is(NewError(err), ErrNoColumn) {
		t.Errorf("syntax error %v classified as ErrNoColumn", err)
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

func spanFor(t *testing.T, err error) *hilton.RecordedSpan {
	t.Helper()

	rec := hilton.Recorder()
	_, span := rec.Tracer(instrumentationName).Start(context.Background(), "sql.queryRow")
	recordSpanErr(span, err)

	recorded := rec.Span("sql.queryRow")
	if recorded == nil {
		t.Fatal("no span")
	}
	return recorded
}

func TestNoRowsIsCountedButNotAFailure(t *testing.T) {
	if got := errorType(dbSql.ErrNoRows); got != "no_rows" {
		t.Errorf("error.type = %q, want no_rows", got)
	}
	span := spanFor(t, dbSql.ErrNoRows)
	if span.Status == hilton.StatusError {
		t.Error("the span was marked failed")
	}
	if len(span.Errors) != 0 {
		t.Errorf("the span recorded %v", span.Errors)
	}
}

func TestRealFailureStillMarksTheSpan(t *testing.T) {
	span := spanFor(t, &pgconn.PgError{Code: "23505"})
	if span.Status != hilton.StatusError {
		t.Error("the span was not marked failed")
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
