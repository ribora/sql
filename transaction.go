package sql

import (
	"context"
	"fmt"
	"strconv"
)

type transactionKey struct{}

type transaction struct {
	Tx
	level  int
	isDone bool
}

func NewContextWithTransaction(ctx context.Context, connection Db) (_ context.Context, commit func() error, rollback func() error, _ error) {
	if existsTx, ok := ctx.Value(transactionKey{}).(*transaction); ok && !existsTx.isDone {
		existsTx.level++
		isDone := false
		savepoint := "level_" + strconv.Itoa(existsTx.level)
		if _, err := existsTx.Exec(ctx, "savepoint "+savepoint); err != nil {
			return nil, nil, nil, fmt.Errorf("%w: %v", ErrCannotCreateSavepoint, err)
		}
		commit = func() error {
			if !isDone {
				if _, err := existsTx.Exec(ctx, "release savepoint "+savepoint); err != nil {
					return fmt.Errorf("%w: %v", ErrCannotReleaseSavepoint, err)
				}
			}
			isDone = true
			return nil
		}
		rollback = func() error {
			if !isDone {
				if _, err := existsTx.Exec(ctx, "rollback to savepoint "+savepoint); err != nil {
					return fmt.Errorf("%w: %v", ErrCannotRollbackSavepoint, err)
				}
			}
			isDone = true
			return nil
		}
	} else {
		conn, err := connection.Connect(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		if tx, err := conn.Begin(ctx); err != nil {
			return nil, nil, nil, fmt.Errorf("%w: %v", ErrCannotStartTx, err)
		} else {
			existsTx = &transaction{Tx: tx}
		}
		ctx = context.WithValue(ctx, transactionKey{}, existsTx)
		commit = func() error {
			if err := existsTx.Commit(ctx); err != nil {
				return fmt.Errorf("%w: %v", ErrCannotCommitTransaction, err)
			}
			existsTx.isDone = true
			return nil
		}
		rollback = func() error {
			if !existsTx.isDone {
				if err := existsTx.Rollback(ctx); err != nil {
					return fmt.Errorf("%w: %v", ErrCannotRollbackTransaction, err)
				}
			}
			existsTx.isDone = true
			return nil
		}
	}

	return ctx, commit, rollback, nil
}

func TransactionFromContext(ctx context.Context) (Tx, error) {
	if existsTx, ok := ctx.Value(transactionKey{}).(*transaction); ok && !existsTx.isDone {
		return existsTx.Tx, nil
	}
	return nil, ErrNoTransactionInContext
}
