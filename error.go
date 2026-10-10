package sql

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
)

var (
	ErrCannotConnectDb = errors.New("unable to connect to a db")

	ErrNoTransactionInContext = errors.New("no started transaction in context")
	ErrCannotStartTx          = errors.New("unable to start a transaction")

	ErrCannotCreateSavepoint   = errors.New("unable to create a savepoint")
	ErrCannotReleaseSavepoint  = errors.New("unable to release a savepoint")
	ErrCannotRollbackSavepoint = errors.New("unable to rollback a savepoint")

	ErrCannotCommitTransaction   = errors.New("unable to commit a transaction")
	ErrCannotRollbackTransaction = errors.New("unable to rollback a transaction")

	ErrExprBuilderNotImplemented = errors.New("expression builder is not defined")
	ErrUnsupportedQueryMode      = errors.New("unsupported query mode")

	ErrArgumentsMismatch = errors.New("number of placeholders does not match number of arguments")
)

var (
	ErrQueryFailed = errors.New("query failed")
	ErrConflict    = errors.New("duplicate key value violates unique constraint")
	ErrNoRows      = errors.New("no rows in result set")
	ErrBadRequest  = errors.New("bad request")
	ErrDeadlock    = errors.New("deadlock")
	ErrNoColumn    = errors.New("column does not exist")
	ErrInternal    = errors.New("internal error")
)

type sqlError interface {
	_sqlError() *e
}

func NewError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, ErrQueryFailed) {
		return err
	}

	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return &e{err: err, code: ErrNoRows}
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		result := &e{
			err:        err,
			code:       ErrInternal,
			constraint: pgErr.ConstraintName,
			field:      pgErr.ColumnName,
		}

		switch pgErr.Code {
		case "23505": // unique_violation
			result.code = ErrConflict
			if result.field == "" {
				result.field = pgErr.ConstraintName
			}
		case "23503", "23514": // foreign_key or check constraint
			result.code = ErrConflict
		case "23502": // not_null_violation
			result.code = ErrBadRequest
		case "40P01": // deadlock_detected
			result.code = ErrDeadlock
		case "42703": // undefined_column
			result.code = ErrNoColumn
		}
		return result
	}

	if mysqlErr, ok := errors.AsType[*mysql.MySQLError](err); ok {
		result := &e{
			err:  err,
			code: ErrInternal,
		}
		switch mysqlErr.Number {
		case 1062: // Duplicate entry
			result.code = ErrConflict
			re := regexp.MustCompile("key '(.+?)'")
			match := re.FindStringSubmatch(mysqlErr.Message)
			if len(match) > 1 {
				result.field = match[1]
			}
		case 1213: // Deadlock
			result.code = ErrDeadlock
		case 1451, 1452: // FK violation
			result.code = ErrConflict
		case 1048, 1364: // Column cannot be null / doesn't have default
			result.code = ErrBadRequest
		case 1054: // Unknown column
			result.code = ErrNoColumn
		}
		return result
	}

	if sqliteErr, ok := errors.AsType[*sqlite.Error](err); ok {
		result := &e{
			err:  err,
			code: ErrInternal,
		}
		code := sqliteErr.Code()
		switch code {
		case 1555, 2067: // SQLITE_CONSTRAINT_PRIMARYKEY, SQLITE_CONSTRAINT_UNIQUE
			result.code = ErrConflict
			parts := strings.Split(sqliteErr.Error(), ": ")
			if len(parts) > 1 {
				result.field = parts[1]
			}
		case 787: // SQLITE_CONSTRAINT_FOREIGNKEY
			result.code = ErrConflict
		case 5, 261: // SQLITE_BUSY (can act as deadlock/lock)
			result.code = ErrDeadlock
		case 1299: // SQLITE_CONSTRAINT_NOTNULL
			result.code = ErrBadRequest
		case 1: // SQLITE_ERROR has no extended code for a missing column
			if strings.Contains(sqliteErr.Error(), "no such column") {
				result.code = ErrNoColumn
			}
		}
		return result
	}

	return err
}

func FromError(err error) (*e, bool) {
	if ee, ok := err.(sqlError); ok {
		return ee._sqlError(), true
	}
	return nil, false
}

type e struct {
	code, err  error
	field      string
	constraint string
}

func (e *e) Is(err error) bool {
	if err == ErrQueryFailed {
		return true
	}
	if err == e.code {
		return true
	}
	return errors.Is(e.err, err)
}

func (e *e) Error() string {
	return e.err.Error()
}

func (e *e) Unwrap() []error {
	return []error{e.code, e.err}
}

func (e *e) Code() error {
	return e.code
}

func (e *e) Field() string {
	return e.field
}

func (e *e) Constraint() string {
	return e.constraint
}

func (e *e) _sqlError() *e {
	return e
}
