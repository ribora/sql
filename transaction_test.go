package sql

import (
	"context"
	"errors"
	"slices"
	"testing"

	"gitlab.com/gorib/sql/driver"
)

var errDo = errors.New("do")

func TestInTransactionCommitsWhatSucceeded(t *testing.T) {
	db := newFakeDb(driver.NewPostgres())

	var inside bool
	if err := InTransaction(context.Background(), db, func(ctx context.Context) error {
		if _, err := TransactionFromContext(ctx); err != nil {
			t.Errorf("the callback got a context with no transaction: %v", err)
		}
		inside = true
		return nil
	}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if !inside {
		t.Fatal("the callback never ran")
	}
	if got := db.calls.ops(); !slices.Equal(got, []string{"connect", "begin", "commit"}) {
		t.Errorf("calls = %v", got)
	}
}

func TestInTransactionRollsBackAndKeepsTheCallbackError(t *testing.T) {
	db := newFakeDb(driver.NewPostgres())

	err := InTransaction(context.Background(), db, func(context.Context) error { return errDo })

	if !errors.Is(err, errDo) {
		t.Errorf("err = %v, want %v", err, errDo)
	}
	if got := db.calls.ops(); !slices.Equal(got, []string{"connect", "begin", "rollback"}) {
		t.Errorf("calls = %v", got)
	}
}

// The case hand-written pairs miss; shark's adapter leaked here.
func TestInTransactionRollsBackAFailedCommit(t *testing.T) {
	db := newFakeDb(driver.NewPostgres())
	db.tx.fail = failOp("commit", errors.New("commit refused"))

	err := InTransaction(context.Background(), db, func(context.Context) error { return nil })

	if !errors.Is(err, ErrCannotCommitTransaction) {
		t.Errorf("err = %v, want %v", err, ErrCannotCommitTransaction)
	}
	if got := db.calls.ops(); !slices.Equal(got, []string{"connect", "begin", "commit", "rollback"}) {
		t.Errorf("calls = %v, want the commit followed by a rollback", got)
	}
}

func TestInTransactionWithoutATransaction(t *testing.T) {
	db := newFakeDb(driver.NewPostgres())
	db.beginErr = errors.New("no")

	var ran bool
	err := InTransaction(context.Background(), db, func(context.Context) error {
		ran = true
		return nil
	})

	if err == nil {
		t.Fatal("a transaction that never began was reported as done")
	}
	if ran {
		t.Error("the callback ran without a transaction")
	}
}

func TestTransactionRunSatisfiesAServicesOwnInterface(t *testing.T) {
	db := newFakeDb(driver.NewPostgres())

	var tx interface {
		Run(ctx context.Context, do func(ctx context.Context) error) error
	} = NewTransaction(db)

	if err := tx.Run(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := db.calls.ops(); !slices.Equal(got, []string{"connect", "begin", "commit"}) {
		t.Errorf("calls = %v", got)
	}
}

// A savepoint, not a second connection — what lets a repository call join.
func TestInTransactionNests(t *testing.T) {
	db := newFakeDb(driver.NewPostgres())

	err := InTransaction(context.Background(), db, func(ctx context.Context) error {
		return InTransaction(ctx, db, func(context.Context) error { return nil })
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if got := db.calls.trace(); !slices.Equal(got, []string{
		"connect", "begin",
		"exec: savepoint level_1",
		"exec: release savepoint level_1",
		"commit",
	}) {
		t.Errorf("calls = %v", got)
	}
}
