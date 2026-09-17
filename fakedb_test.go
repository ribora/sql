package sql

import (
	"context"
	dbSql "database/sql"
)

type call struct {
	op    string
	query string
	args  []any
}

type calls struct {
	entries []call
}

func (c *calls) add(op, query string, args ...any) {
	c.entries = append(c.entries, call{op: op, query: query, args: args})
}

func (c *calls) ops() []string {
	ops := make([]string, 0, len(c.entries))
	for _, e := range c.entries {
		ops = append(ops, e.op)
	}
	return ops
}

func (c *calls) trace() []string {
	out := make([]string, 0, len(c.entries))
	for _, e := range c.entries {
		if e.query == "" {
			out = append(out, e.op)
			continue
		}
		out = append(out, e.op+": "+e.query)
	}
	return out
}

func (c *calls) first(op string) (call, bool) {
	for _, e := range c.entries {
		if e.op == op {
			return e, true
		}
	}
	return call{}, false
}

type fakeDb struct {
	calls    *calls
	drv      Driver
	tx       *fakeTx
	connErr  error
	beginErr error
}

func newFakeDb(drv Driver) *fakeDb {
	c := &calls{}
	return &fakeDb{calls: c, drv: drv, tx: &fakeTx{calls: c}}
}

func (d *fakeDb) Connect(context.Context) (Db, error) {
	d.calls.add("connect", "")
	if d.connErr != nil {
		return nil, d.connErr
	}
	return d, nil
}

func (d *fakeDb) Begin(context.Context) (Tx, error) {
	d.calls.add("begin", "")
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return d.tx, nil
}

func (d *fakeDb) Driver() Driver { return d.drv }

func (d *fakeDb) DbSql() (*dbSql.DB, error) { return nil, nil }
