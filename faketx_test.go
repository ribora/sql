package sql

import (
	"context"
	"reflect"
	"strings"
)

// Recording doubles rather than sqltest: a test here demands that a commit
// or a savepoint release fails, which a real database will not do on request.
type fakeTx struct {
	calls *calls
	// op is exec, query, queryRow, scan, commit or rollback. scan is the
	// error Query's rows produce rather than Query itself.
	fail func(op, query string) error
	row  []any
	rows [][]any
}

func (t *fakeTx) err(op, query string) error {
	if t.fail == nil {
		return nil
	}
	return t.fail(op, query)
}

func (t *fakeTx) Exec(_ context.Context, query string, args ...any) (Result, error) {
	t.calls.add("exec", query, args...)
	return fakeResult{}, t.err("exec", query)
}

func (t *fakeTx) QueryRow(_ context.Context, query string, args ...any) Row {
	t.calls.add("queryRow", query, args...)
	return fakeRow{values: t.row, err: t.err("queryRow", query)}
}

func (t *fakeTx) Query(_ context.Context, query string, args ...any) (Rows, error) {
	t.calls.add("query", query, args...)
	if err := t.err("query", query); err != nil {
		return nil, err
	}
	return &fakeRows{values: t.rows, err: t.err("scan", query)}, nil
}

func (t *fakeTx) Commit(context.Context) error {
	t.calls.add("commit", "")
	return t.err("commit", "")
}

func (t *fakeTx) Rollback(context.Context) error {
	t.calls.add("rollback", "")
	return t.err("rollback", "")
}

func failOp(op string, err error) func(string, string) error {
	return func(o, _ string) error {
		if o == op {
			return err
		}
		return nil
	}
}

func failStatement(fragment string, err error) func(string, string) error {
	return func(_, query string) error {
		if strings.Contains(query, fragment) {
			return err
		}
		return nil
	}
}

func failEveryQuery(err error) func(string, string) error {
	return func(op, _ string) error {
		switch op {
		case "exec", "query", "queryRow", "scan":
			return err
		}
		return nil
	}
}

type fakeResult struct{}

func (fakeResult) RowsAffected() int64 { return 0 }

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return scanInto(dest, r.values)
}

type fakeRows struct {
	values [][]any
	pos    int
	err    error
}

func (r *fakeRows) Next() bool {
	r.pos++
	return r.pos <= len(r.values)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return scanInto(dest, r.values[r.pos-1])
}

func (r *fakeRows) Close() {}

func (r *fakeRows) Err() error { return r.err }

func scanInto(dest []any, values []any) error {
	for i, d := range dest {
		if i >= len(values) {
			break
		}
		reflect.ValueOf(d).Elem().Set(reflect.ValueOf(values[i]))
	}
	return nil
}
