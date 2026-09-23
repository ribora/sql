package sql

import "testing"

// The migrate command asks for the handle without calling Connect.
func TestSqlDbSqlWithoutConnect(t *testing.T) {
	db, err := NewDb("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.DbSql()
	if err != nil {
		t.Fatal(err)
	}
	if handle == nil {
		t.Fatal("DbSql returned no handle before Connect")
	}
	if err = handle.Ping(); err != nil {
		t.Fatal(err)
	}
}

// modernc.org/sqlite registers "sqlite" only.
func TestSqliteNames(t *testing.T) {
	for _, name := range []string{"sqlite", "sqlite3"} {
		t.Run(name, func(t *testing.T) {
			db, err := NewDb(name, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			if db.Driver() == nil {
				t.Fatal("no dialect behind the driver name")
			}
			handle, err := db.DbSql()
			if err != nil {
				t.Fatal(err)
			}
			if err = handle.Ping(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Connect runs on every transaction; a pool swapped on a failed ping leaks the
// old one's connections.
func TestSqlConnectKeepsTheHandle(t *testing.T) {
	db, err := NewDb("mysql", "harness@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.DbSql()
	if err != nil {
		t.Fatal(err)
	}
	if first.Ping() == nil {
		t.Skip("something listens on 127.0.0.1:1")
	}
	if _, err = db.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	second, err := db.DbSql()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("Connect opened a second pool, and the Db owns one")
	}
}
