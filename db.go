// Cardinality: db.operation.name labels are bounded ({begin,query,queryRow,
// exec,commit,rollback}); db.query.text is a span attribute only — never a
// metric label.

package sql

import (
	"context"
	dbSql "database/sql"
	"errors"
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

// Cached: the provider arrives per operation, and building instruments on every
// transaction is not free.
var instruments = hilton.NewInstruments(func(p hilton.Provider) sqlState {
	return sqlState{
		tracer: p.Tracer(instrumentationName),
		duration: p.Meter(instrumentationName).Histogram("db.client.operation.duration",
			hilton.WithDescription("Duration of SQL client operations"),
			hilton.WithUnit("s"),
			hilton.WithSecondsBoundaries(),
		),
	}
})

type sqlState struct {
	tracer   hilton.Tracer
	duration hilton.Histogram
}

func (t *tracedDb) Begin(ctx context.Context) (tx Tx, err error) {
	state := instruments.FromContext(ctx)
	tracer := state.tracer
	duration := state.duration

	txCtx, span := tracer.Start(ctx, "sql.transaction",
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(
			hilton.Attr("db.system.name", t.system),
			hilton.Attr("db.operation.name", "begin"),
		),
	)
	start := time.Now()

	defer func() {
		if tx == nil {
			recordSpanErr(span, err)
			recordDb(ctx, duration, t.system, "begin", time.Since(start).Seconds(), err)
			span.End()
		}
	}()

	innerTx, err := t.inner.Begin(txCtx)
	if err != nil {
		return nil, err
	}

	recordDb(txCtx, duration, t.system, "begin", time.Since(start).Seconds(), nil)

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
			hilton.Attr("db.system.name", t.system),
			hilton.Attr("db.operation.name", "queryRow"),
			hilton.Attr("db.query.text", query),
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
			hilton.Attr("db.system.name", t.system),
			hilton.Attr("db.operation.name", "commit"),
		),
	)
	callCtx := hilton.ContextWithSpan(ctx, span)
	defer func() {
		took := time.Since(start).Seconds()
		recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.system, "commit", took, err)
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
			hilton.Attr("db.system.name", t.system),
			hilton.Attr("db.operation.name", "rollback"),
		),
	)
	callCtx := hilton.ContextWithSpan(ctx, span)
	defer func() {
		took := time.Since(start).Seconds()
		recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.system, "rollback", took, err)
		span.End()
		t.closer.end(err)
	}()
	return t.inner.Rollback(callCtx)
}

func doOp[T any](t *tracedTx, ctx context.Context, op, query string, fn func(context.Context) (T, error)) (res T, err error) {
	_, span := t.tracer.Start(t.txCtx, "sql."+op,
		hilton.WithSpanKind(hilton.SpanKindClient),
		hilton.WithAttributes(
			hilton.Attr("db.system.name", t.system),
			hilton.Attr("db.operation.name", op),
			hilton.Attr("db.query.text", query),
		),
	)
	callCtx := hilton.ContextWithSpan(ctx, span)
	start := time.Now()
	defer func() {
		took := time.Since(start).Seconds()
		recordSpanErr(span, err)
		recordDb(callCtx, t.duration, t.system, op, took, err)
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
		recordSpanErr(f.closer.span, err)
		recordDb(f.ctx, f.duration, f.system, "queryRow", time.Since(f.start).Seconds(), err)
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

func recordSpanErr(span hilton.Span, err error) {
	if err == nil {
		return
	}
	if errors.Is(NewError(err), ErrNoRows) {
		return
	}
	span.RecordError(err)
	span.SetStatus(hilton.StatusError, err.Error())
}

func recordDb(ctx context.Context, duration hilton.Histogram, system, op string, took float64, err error) {
	attrs := []hilton.Attribute{
		hilton.Attr("db.system.name", system),
		hilton.Attr("db.operation.name", op),
	}
	if t := errorType(err); t != "" {
		attrs = append(attrs, hilton.Attr("error.type", t))
	}
	duration.Record(ctx, took, attrs...)
}

const otherErrorType = "_OTHER"

func errorType(err error) string {
	if err == nil {
		return ""
	}
	classified := NewError(err)
	switch {
	case errors.Is(classified, ErrConflict):
		return "conflict"
	case errors.Is(classified, ErrDeadlock):
		return "deadlock"
	case errors.Is(classified, ErrNoRows):
		return "no_rows"
	case errors.Is(classified, ErrBadRequest):
		return "bad_request"
	case errors.Is(classified, ErrInternal):
		return "internal"
	case errors.Is(err, ErrCannotConnectDb):
		return "connect"
	case errors.Is(err, ErrCannotStartTx),
		errors.Is(err, ErrCannotCommitTransaction),
		errors.Is(err, ErrCannotRollbackTransaction):
		return "tx"
	}
	return otherErrorType
}
