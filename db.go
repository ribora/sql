// Cardinality: db.operation labels are bounded ({begin,query,queryRow,exec,
// commit,rollback}); db.statement is a span attribute only — never a metric label.

package sql

import (
	"context"
	dbSql "database/sql"
	"runtime"
	"sync"
	"time"

	"gitlab.com/gorib/hilton"
)

const instrumentationName = "gitlab.com/gorib/sql"

func NewDb(driver, dsn string) (Db, error) {
	var inner Db
	var err error
	switch driver {
	case "postgres", "pgx", "pgsql":
		inner = newPgx(dsn)
	default:
		inner, err = newSql(driver, dsn)
		if err != nil {
			return nil, err
		}
	}
	return &tracedDb{inner: inner}, nil
}

type tracedDb struct {
	inner Db
}

func (t *tracedDb) Connect(ctx context.Context) (Db, error) {
	_, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (t *tracedDb) Begin(ctx context.Context) (tx Tx, err error) {
	provider := hilton.ProviderFromContext(ctx)
	tracer := provider.Tracer(instrumentationName)
	meter := provider.Meter(instrumentationName)
	duration := meter.Histogram("db.client.operation.duration", hilton.WithUnit("s"))
	operations := meter.Counter("db.client.operations")

	txCtx, span := tracer.Start(ctx, "sql.transaction", hilton.WithSpanKind(hilton.SpanKindClient))
	start := time.Now()

	defer func() {
		if tx == nil {
			status := recordSpanErr(span, err)
			recordDb(ctx, duration, operations, "begin", time.Since(start).Seconds(), status)
			span.End()
		}
	}()

	innerTx, err := t.inner.Begin(txCtx)
	if err != nil {
		return nil, err
	}

	recordDb(txCtx, duration, operations, "begin", time.Since(start).Seconds(), "ok")

	closer := &spanCloser{span: span}
	ttx := &tracedTx{
		inner:      innerTx,
		closer:     closer,
		tracer:     tracer,
		duration:   duration,
		operations: operations,
		txCtx:      txCtx,
	}

	runtime.AddCleanup(ttx, func(c *spanCloser) {
		c.end(nil)
	}, closer)

	return ttx, nil
}

func (t *tracedDb) Driver() Driver            { return t.inner.Driver() }
func (t *tracedDb) DbSql() (*dbSql.DB, error) { return t.inner.DbSql() }

type spanCloser struct {
	span hilton.Span
	once sync.Once
}

func (c *spanCloser) end(err error) {
	c.once.Do(func() {
		if c.span != nil {
			if err != nil {
				recordSpanErr(c.span, err)
			}
			c.span.End()
		}
	})
}

// tracedTx caches tracer/meter outputs resolved from the Begin ctx — they live
// for the transaction's lifetime, not per query.
type tracedTx struct {
	inner      Tx
	closer     *spanCloser
	tracer     hilton.Tracer
	duration   hilton.Histogram
	operations hilton.Counter
	txCtx      context.Context
}

func (t *tracedTx) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	return doOp(t, ctx, "query", query, func(callCtx context.Context) (Rows, error) {
		return t.inner.Query(callCtx, query, args...)
	})
}

func (t *tracedTx) QueryRow(ctx context.Context, query string, args ...any) Row {
	_, span := t.tracer.Start(t.txCtx, "sql.queryRow",
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(hilton.StringAttr("db.statement", query)),
	)

	callCtx := hilton.ContextWithSpan(ctx, span)
	closer := &spanCloser{span: span}
	row := &tracedRow{
		row:        t.inner.QueryRow(callCtx, query, args...),
		closer:     closer,
		duration:   t.duration,
		operations: t.operations,
		start:      time.Now(),
		ctx:        callCtx,
	}

	runtime.AddCleanup(row, func(c *spanCloser) {
		c.end(nil)
	}, closer)

	return row
}

func (t *tracedTx) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return doOp(t, ctx, "exec", query, func(callCtx context.Context) (Result, error) {
		return t.inner.Exec(callCtx, query, args...)
	})
}

func (t *tracedTx) Commit(ctx context.Context) (err error) {
	start := time.Now()
	_, span := t.tracer.Start(t.txCtx, "sql.commit", hilton.WithSpanKind(hilton.SpanKindClient))
	callCtx := hilton.ContextWithSpan(ctx, span)
	defer func() {
		took := time.Since(start).Seconds()
		status := recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.operations, "commit", took, status)
		span.End()
		t.closer.end(err)
	}()
	return t.inner.Commit(callCtx)
}

func (t *tracedTx) Rollback(ctx context.Context) (err error) {
	start := time.Now()
	_, span := t.tracer.Start(t.txCtx, "sql.rollback", hilton.WithSpanKind(hilton.SpanKindClient))
	callCtx := hilton.ContextWithSpan(ctx, span)
	defer func() {
		took := time.Since(start).Seconds()
		status := recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.operations, "rollback", took, status)
		span.End()
		t.closer.end(err)
	}()
	return t.inner.Rollback(ctx)
}

func doOp[T any](t *tracedTx, ctx context.Context, op, query string, fn func(context.Context) (T, error)) (res T, err error) {
	_, span := t.tracer.Start(t.txCtx, "sql."+op,
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(hilton.StringAttr("db.statement", query)),
	)
	callCtx := hilton.ContextWithSpan(ctx, span)
	start := time.Now()
	defer func() {
		took := time.Since(start).Seconds()
		status := recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.operations, op, took, status)
		span.End()
	}()
	return fn(callCtx)
}

type tracedRow struct {
	row        Row
	closer     *spanCloser
	duration   hilton.Histogram
	operations hilton.Counter
	start      time.Time
	ctx        context.Context
}

func (r *tracedRow) Scan(dest ...any) (err error) {
	defer func() {
		status := recordSpanErr(r.closer.span, err)
		recordDb(r.ctx, r.duration, r.operations, "queryRow", time.Since(r.start).Seconds(), status)
		r.closer.end(err)
	}()
	return r.row.Scan(dest...)
}

func recordSpanErr(span hilton.Span, err error) string {
	if err == nil {
		return "ok"
	}
	span.RecordError(err)
	span.SetStatus(hilton.StatusError, err.Error())
	return "error"
}

func recordDb(ctx context.Context, duration hilton.Histogram, operations hilton.Counter, op string, took float64, status string) {
	attrs := []hilton.Attribute{
		hilton.StringAttr("db.operation", op),
		hilton.StringAttr("status", status),
	}
	duration.Record(ctx, took, attrs...)
	operations.Add(ctx, 1, attrs...)
}
