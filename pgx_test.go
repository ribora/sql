package sql

import (
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Callers never close what DbSql gives them, so a pool per call leaks
// connections until postgres refuses new clients.
func TestPgxDbSqlIsOneHandle(t *testing.T) {
	db, err := NewDb("pgx", "postgres://harness.invalid:5432/db")
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.DbSql()
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.DbSql()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("DbSql opened a second pool, and the Db owns one")
	}
}

// Connect runs on every transaction, so the first requests after start race
// for it.
func TestPgxConcurrentConnectIsOnePool(t *testing.T) {
	db, err := NewDb("pgx", "postgres://harness.invalid:5432/db")
	if err != nil {
		t.Fatal(err)
	}
	inner := db.(*tracedDb).inner.(*pgxDb)

	const callers = 32
	pools := make([]*pgxpool.Pool, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			<-start
			if _, err := db.Connect(t.Context()); err != nil {
				t.Error(err)
				return
			}
			pools[i] = inner.db
		})
	}
	close(start)
	wg.Wait()

	for i, pool := range pools {
		if pool != pools[0] {
			t.Fatalf("caller %d got a pool of its own, and the Db owns one", i)
		}
	}
}

// The host is unreachable, so a ping fails either way; only the message tells
// a closed pool from a refused one.
func TestPgxCloseClosesPoolAndHandle(t *testing.T) {
	db, err := NewDb("pgx", "postgres://harness.invalid:5432/db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	handle, err := db.DbSql()
	if err != nil {
		t.Fatal(err)
	}
	pool := db.(*tracedDb).inner.(*pgxDb).db
	if err = closeDb(t, db); err != nil {
		t.Fatal(err)
	}
	if err = pool.Ping(t.Context()); err == nil || !strings.Contains(err.Error(), "closed pool") {
		t.Errorf("the pool is still open after Close: %v", err)
	}
	if err = handle.Ping(); err == nil || err.Error() != "sql: database is closed" {
		t.Errorf("the handle is still open after Close: %v", err)
	}
}

func TestPgxCloseBeforeConnect(t *testing.T) {
	db, err := NewDb("pgx", "postgres://harness.invalid:5432/db")
	if err != nil {
		t.Fatal(err)
	}
	if err = closeDb(t, db); err != nil {
		t.Fatal(err)
	}
}
