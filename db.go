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
	return &tracedDb{inner: inner, system: systemName(driver)}, nil
}

func systemName(driver string) string {
	switch driver {
	case "postgres", "pgx", "pgsql":
		return "postgresql"
	case "mysql":
		return "mysql"
	case "sqlite", "sqlite3":
		return "sqlite"
	default:
		return driver
	}
}

type tracedDb struct {
	inner  Db
	system string
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

	txCtx, span := tracer.Start(ctx, "sql.transaction",
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(
			hilton.StringAttr("db.system", t.system),
			hilton.StringAttr("db.operation", "begin"),
		),
	)
	start := time.Now()

	defer func() {
		if tx == nil {
			status := recordSpanErr(span, err)
			recordDb(ctx, duration, t.system, "begin", time.Since(start).Seconds(), status)
			span.End()
		}
	}()

	innerTx, err := t.inner.Begin(txCtx)
	if err != nil {
		return nil, err
	}

	recordDb(txCtx, duration, t.system, "begin", time.Since(start).Seconds(), "ok")

	closer := &spanCloser{span: span}
	ttx := &tracedTx{
		inner:    innerTx,
		closer:   closer,
		tracer:   tracer,
		duration: duration,
		txCtx:    txCtx,
		system:   t.system,
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
	inner    Tx
	closer   *spanCloser
	tracer   hilton.Tracer
	duration hilton.Histogram
	txCtx    context.Context
	system   string
}

func (t *tracedTx) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	return doOp(t, ctx, "query", query, func(callCtx context.Context) (Rows, error) {
		return t.inner.Query(callCtx, query, args...)
	})
}

func (t *tracedTx) QueryRow(ctx context.Context, query string, args ...any) Row {
	_, span := t.tracer.Start(t.txCtx, "sql.queryRow",
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(
			hilton.StringAttr("db.system", t.system),
			hilton.StringAttr("db.operation", "queryRow"),
			hilton.StringAttr("db.statement", query),
		),
	)

	callCtx := hilton.ContextWithSpan(ctx, span)
	fin := &rowFinalizer{
		closer:   &spanCloser{span: span},
		duration: t.duration,
		start:    time.Now(),
		ctx:      callCtx,
		system:   t.system,
	}
	row := &tracedRow{
		row: t.inner.QueryRow(callCtx, query, args...),
		fin: fin,
	}

	runtime.AddCleanup(row, func(f *rowFinalizer) {
		f.finish(nil)
	}, fin)

	return row
}

func (t *tracedTx) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return doOp(t, ctx, "exec", query, func(callCtx context.Context) (Result, error) {
		return t.inner.Exec(callCtx, query, args...)
	})
}

func (t *tracedTx) Commit(ctx context.Context) (err error) {
	start := time.Now()
	_, span := t.tracer.Start(t.txCtx, "sql.commit",
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(
			hilton.StringAttr("db.system", t.system),
			hilton.StringAttr("db.operation", "commit"),
		),
	)
	callCtx := hilton.ContextWithSpan(ctx, span)
	defer func() {
		took := time.Since(start).Seconds()
		status := recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.system, "commit", took, status)
		span.End()
		t.closer.end(err)
	}()
	return t.inner.Commit(callCtx)
}

func (t *tracedTx) Rollback(ctx context.Context) (err error) {
	start := time.Now()
	_, span := t.tracer.Start(t.txCtx, "sql.rollback",
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(
			hilton.StringAttr("db.system", t.system),
			hilton.StringAttr("db.operation", "rollback"),
		),
	)
	callCtx := hilton.ContextWithSpan(ctx, span)
	defer func() {
		took := time.Since(start).Seconds()
		status := recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.system, "rollback", took, status)
		span.End()
		t.closer.end(err)
	}()
	return t.inner.Rollback(callCtx)
}

func doOp[T any](t *tracedTx, ctx context.Context, op, query string, fn func(context.Context) (T, error)) (res T, err error) {
	_, span := t.tracer.Start(t.txCtx, "sql."+op,
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(
			hilton.StringAttr("db.system", t.system),
			hilton.StringAttr("db.operation", op),
			hilton.StringAttr("db.statement", query),
		),
	)
	callCtx := hilton.ContextWithSpan(ctx, span)
	start := time.Now()
	defer func() {
		took := time.Since(start).Seconds()
		status := recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.system, op, took, status)
		span.End()
	}()
	return fn(callCtx)
}

type rowFinalizer struct {
	once     sync.Once
	closer   *spanCloser
	duration hilton.Histogram
	start    time.Time
	ctx      context.Context
	system   string
}

func (f *rowFinalizer) finish(err error) {
	f.once.Do(func() {
		status := recordSpanErr(f.closer.span, err)
		recordDb(f.ctx, f.duration, f.system, "queryRow", time.Since(f.start).Seconds(), status)
		f.closer.end(err)
	})
}

type tracedRow struct {
	row Row
	fin *rowFinalizer
}

func (r *tracedRow) Scan(dest ...any) (err error) {
	defer func() { r.fin.finish(err) }()
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

func recordDb(ctx context.Context, duration hilton.Histogram, system, op string, took float64, status string) {
	attrs := []hilton.Attribute{
		hilton.StringAttr("db.system", system),
		hilton.StringAttr("db.operation", op),
		hilton.StringAttr("status", status),
	}
	duration.Record(ctx, took, attrs...)
}
