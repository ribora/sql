# Storage SQL Module

The `storage/sql` package is a high-performance SQL toolkit for Go built on zero-runtime-reflection and clean domain model principles.

## 1. Architecture and Drivers

The package supports multiple databases using the optimal driver for each:

- **PostgreSQL**: implemented via the native **`pgx/v5`** driver for maximum performance and support for PostgreSQL-specific types (JSONB, arrays).
- **MySQL, SQLite**: implemented via the standard **`database/sql`** interface.

The driver is selected when creating the `Db` instance:

```go
// PostgreSQL (pgx)
db, _ := sql.NewDb("postgres", dsn)

// MySQL (database/sql)
db, _ := sql.NewDb("mysql", dsn)
```

---

## 2. SQL Builder

The builder constructs SQL queries programmatically, providing injection safety and multi-dialect support.

### Inspection and Drivers

`Build()` accepts a driver instance and returns the final query string with placeholders.

```go
import "gitlab.com/gorib/sql/driver"

q := sql.Select().From(sql.Table("users")).Where(criteria.And().Add("id", "eq", 10))

// PostgreSQL: SELECT * FROM users WHERE id = $1
sqlPostgres, params, _ := q.Build(driver.NewPostgres())

// MySQL: SELECT * FROM users WHERE id = ?
sqlMysql, params, _ := q.Build(driver.NewMysql())
```

### Operations

The operation is a plain string as far as `criteria` is concerned; `sql` owns
the vocabulary. The `op` package names it, so a condition can be written
without a literal:

```go
import "gitlab.com/gorib/sql/op"

criteria.And("id", string(op.Eq), 10)
sql.Eq("id", 10) // the same condition
```

**Not every driver supports every operation**, and a driver that does not know
one fails when the query is built:

| constructor | `op` | Postgres | MySQL | SQLite |
|---|---|---|---|---|
| `Eq` `Ne` `Gt` `Ge` `Lt` `Le` | `Eq` … | ✅ | ✅ | ✅ |
| `Between` `Like` | `Between` `Like` | ✅ | ✅ | ✅ |
| `ILike` | `ILike` | `ILIKE` | falls back to `LIKE` | falls back to `LIKE` |
| `JsonIn` | `JsonIn` | `@>` | ❌ | ❌ |
| `JsonEq` | `JsonEq` | `?` / `?\|` | ❌ | ❌ |

`ILike` is the one that fails quietly: outside Postgres it becomes a
case-sensitive match instead of an error. The matrix is pinned by
`driver/operations_test.go`.

Each driver also accepts the raw SQL spelling (`=`, `!=`, `@>`) and the usual
synonyms (`in`, `neq`, `nin`, `gte`, `lte`).

### SELECT

```go
// Simple select with conditions and ordering
q := sql.Select("id", "name", "email").
    From(sql.Table("users")).
    Where(criteria.And().
        Add("status", "eq", "active").
        Add("age", "gt", 18),
    ).
    Sort("created_at desc").
    Page(0, 20)

// Aliases and JOINs
q := sql.Select("u.name", "o.total").
    From(sql.Table("users").As("u"),
        sql.Join(sql.Table("orders").As("o"), "u.id = o.user_id"),
        sql.LeftJoin(sql.Table("profiles").As("p"), "u.id = p.user_id"))

// Subquery in WHERE
sub := sql.Select("id").From(sql.Table("banned_users"))
q := sql.Select().From(sql.Table("posts")).
    Where(criteria.And().Add("user_id", "in", sub))

// Column-to-column comparison
q := sql.Select().From(sql.Table("users")).
    Where(criteria.And().Add("updated_at", "gt", sql.Expr("created_at")))
```

### INSERT, UPDATE, DELETE

```go
// INSERT with RETURNING
q := sql.Insert("name").Into(sql.Table("users")).Values("Alice").Returning("id")

// UPDATE with aliases
q := sql.Update(map[string]any{"u.status": "active"}).
    Table(sql.Table("users").As("u")).
    From(sql.Join(sql.Table("orders").As("o"), "u.id = o.user_id")).
    Where(criteria.And().Add("o.total", "gt", 1000))

// Atomic increment via sql.Expr
q := sql.Update(map[string]any{"version": sql.Expr("version + 1")}).
    Table(sql.Table("items")).Where(criteria.And().Add("id", "eq", 1))

// DELETE
q := sql.Delete().From(sql.Table("logs")).Where(criteria.And().Add("created_at", "lt", oldDate))
```

### PLAIN (Raw SQL)

The `Plain` builder executes a hand-written SQL string.

```go
q := sql.Plain("SELECT * FROM users WHERE status = $? AND age > $?", "active", 25)
```

**Placeholder behavior:**

1. **Universal placeholder `$?` (recommended)**
   - *Portability*: write the query once; the library replaces `$?` with the driver-native placeholder (`$1, $2` for PostgreSQL, `?` for MySQL).
   - *Safety*: the library verifies that the number of `$?` placeholders matches the number of arguments. A mismatch returns `sql.ErrArgumentsMismatch`.

2. **Native placeholders (`$1`, `:name`, `?`)**
   - Use these when you need database-specific features.
   - The library passes the string and arguments through as-is without validation. Mismatch errors come from the database itself.

3. **Important restriction**: `$?` and native placeholders **cannot be mixed** in the same query. If at least one `$?` is found, the library enters auto-numbering mode, which will conflict with any existing native indices.

---

## 3. `store` Utility (Code Generator)

The generator creates the mapping between the database and your Go types, eliminating runtime reflection.

### Defining a Stored Struct

```go
//go:generate go tool store $GOFILE
package postgres

type storedUser struct {
    _ *model.User

    // readonly: excluded from the base Updates(),
    // but INCLUDED in a named group when that group is requested explicitly.
    Id        int       `db:"id" store:",readonly,updates:Identity"`

    Email     string    `db:"email"`
    LastLogin time.Time `db:"last_login" store:",updates:Status"`
    // The column is a nullable text; the model field is a []string.
    Roles     *string   `db:"roles" store:"Roles,convert:csv"`

    // Override the source field:
    // the Name column in the DB is populated from the FullName field of the domain model.
    Name      string    `db:"name" store:"FullName"`
}
```

### Generated Code (simplified)

```go
// newStoredUser — maps a domain model to the DB struct
func newStoredUser(m *model.User) *storedUser {
    return &storedUser{
        Id:    m.Id,
        Email: m.Email,
        Name:  m.FullName, // uses the overridden field name
        Roles: sql.JoinCsv(m.Roles),
    }
}

// Unwrap — maps the DB struct back to the domain model
func (m *storedUser) Unwrap() *model.User {
    return &model.User{
        Id:       m.Id,
        Email:    m.Email,
        FullName: m.Name, // mapped back to FullName
        Roles:    sql.SplitCsv(m.Roles),
    }
}

// Scan returns a slice of pointers to fields for rows.Scan()
func (m *storedUser) Scan() []any {
    return []any{&m.Id, &m.Email, &m.LastLogin, &m.Roles, &m.Name}
}

// Returns lists all column names
func (m *storedUser) Returns() []string {
    return []string{"id", "email", "last_login", "roles", "name"}
}

// Updates contains only non-readonly fields
func (m *storedUser) Updates() map[string]any {
    return map[string]any{
        "email":      m.Email,
        "last_login": m.LastLogin,
        "roles":      m.Roles,
        "name":       m.Name,
    }
}

// UpdatesIdentity ignores readonly and includes the field because the group is named explicitly
func (m *storedUser) UpdatesIdentity() map[string]any {
    return map[string]any{"id": m.Id}
}
```

### Flags on the `_` field

| Flag | Effect |
|------|--------|
| `nonew` | Do not generate `newStoredUser`. |
| `nounwrap` | Do not generate `Unwrap()`. |

Use these when you need a custom mapping implementation.

With both of them the `_` field may name no model at all:

```go
type storedReading struct {
    _ any `store:"nonew,nounwrap"`

    At    time.Time `db:"at"`
    Event string    `db:"event"`
}
```

That is how a read model is written — projected by a query, never turned back
into a domain type. Neither constructor is generated, so there is no model to
check the fields against.

### Field Tags

| Tag | Description |
|-----|-------------|
| `db:"column"` | Column name in the database. |
| `store:"SourceField"` | Name of the corresponding field in the domain model (when it differs from the stored struct field name). |
| `store:",readonly"` | Excluded from the base `Inserts()` and `Updates()`. |
| `store:",updates:Group1\|Group2"` | Included in the named update methods. Overrides `readonly`. |
| `store:",inserts:Group1\|Group2"` | Included in the named insert methods. Overrides `readonly`. |
| `store:",convert:csv"` | Maps a model `[]string` to a stored `*string` through `sql.JoinCsv` / `sql.SplitCsv`. Lossy: an element containing a comma comes back as two, and no elements is NULL. |
| `store:"-"` | Not a column and not mapped: a cache, a computed value, a collection loaded by another query. |

### Every Field Is Answered For

The generator type-checks the package, so a mapping that does not line up is
reported against your tag rather than against the generated file:

- A field with neither a `db` tag nor `store:"-"` stops the run. Silence used to
  mean "not a column", and the column then went missing from `Scan()` and
  `Inserts()` with nothing to read about it.
- `store:"Foo"` naming a field the model does not have stops the run.
- A stored field whose type does not match the model field's stops the run.

A model field with nothing on the stored struct to carry it would be left zero by
`Unwrap()`, so the generator **writes it into the struct**, without tags, and
stops. Name its column, or decline it with `store:"-"`:

```go
type storedUser struct {
    _ *model.User

    Id    int    `db:"id"`
    Email string `db:"email"`

    // Written in by the generator; the answer below is the author's.
    Sessions []*model.Session `store:"-"`
}
```

The answer is recorded on the field, next to the type it belongs to, and it
cannot go stale: rename the model field and the mapping moves with it.

### The Build Guard

Generated files carry `//go:build !store`, and the generator loads packages with
`-tags store`. Its own output is therefore invisible to it, so a stale or broken
`_gen.go` cannot block the run that replaces it — including output written before
the guard existed, which is recognised by its header line and regenerated.

---

## 4. Repository: CRUD Automation

`sql.Repository[Model]` is a high-level generic abstraction that automatically uses the `Scannable` and `Returnable` interfaces.

### Model Requirements

| Interface | Method | Purpose |
|-----------|--------|---------|
| `Scannable` | `Scan() []any` | Returns pointers to fields for `rows.Scan`. |
| `Returnable` | `Returns() []string` | Defines the column list for `SELECT` / `RETURNING`. |

The `store` code generator produces both methods automatically.

### Automatic RETURNING

The repository automatically appends `Returning(...)` to `INSERT` and `UPDATE` queries when the driver supports it (e.g., PostgreSQL). This lets you retrieve generated values (such as auto-incremented IDs) in the same round-trip.

```go
func (r *userRepo) Create(ctx context.Context, user *model.User) (*model.User, error) {
    fields, values := newStoredUser(user).Inserts()

    // Repository adds .Returning() automatically based on stored.Returns()
    stored, err := r.Repository.Get(ctx, sql.Insert(fields...).Into(sql.Table("users")).Values(values...))
    if err != nil {
        return nil, err
    }

    return stored.Unwrap(), nil
}
```

### Available Methods

| Method | Description |
|--------|-------------|
| `Get(ctx, builder)` | Returns a single `*Model`. |
| `Select(ctx, builder)` | Returns `[]*Model`. |
| `Delete(ctx, builder)` | Executes a DELETE. |
| `Count(ctx, builder)` | Returns the row count. |

---

## 5. Transactions and Savepoints

Transactions are propagated via `context.Context`. Nested `Begin` calls automatically create `SAVEPOINT`s.

```go
ctx, commit, rollback, err := repo.Begin(ctx)
if err != nil {
    return err
}
defer rollback()

if err := repo.Save(ctx, user); err != nil {
    return err
}

return commit()
```

---

## 6. Additional Features

### JSON Fields

Use `sql.Json[T]` for JSON / JSONB columns. The wrapper handles marshalling and unmarshalling transparently.

```go
type storedPayment struct {
    Meta *sql.Json[model.PaymentMeta] `db:"meta"`
}
```

---

## 7. Telemetry

`NewDb` returns the pool wrapped in a tracing decorator, so every operation
produces a `CLIENT` span and a `db.client.operation.duration` observation
without the caller doing anything.

| Attribute | On | Values |
|---|---|---|
| `db.system.name` | span and metric | `postgresql`, `mysql`, `sqlite` |
| `db.operation.name` | span and metric | `begin`, `query`, `queryRow`, `exec`, `commit`, `rollback` |
| `error.type` | metric, when the operation did not succeed | `conflict`, `deadlock`, `no_rows`, `bad_request`, `internal`, `connect`, `tx`, `_OTHER` |
| `db.query.text` | **span only** | the statement |

The split in that last row is the point: operation names are a closed set and
make a safe metric label, a query string is not. `error.type` comes from the
same classification as `sql.NewError`, so a metric and a handled error agree on
what went wrong; anything the classification does not recognise is `_OTHER`
rather than a Go type, which would say nothing and change with the wrapping.

A lookup that matched nothing is counted but not failed: `ErrNoRows` reports
`error.type=no_rows` on the histogram, while the span is left unmarked, because
"the row is not there" is usually the question being asked. `gorib/redis`
treats a cache miss the same way and reports the same value.

Durations are in seconds with explicit second-scale buckets — the SDK default is
millisecond-scaled and would put every query in one bucket.

The provider is read from the context per operation rather than captured at
construction, so a query inherits the trace of the request that issued it. The
instruments themselves are cached per provider by `hilton.NewInstruments`
instead of being rebuilt on every call.

A transaction is one span: `Begin` opens it, `Commit` or `Rollback` closes it,
and the statements inside are children. A transaction that is never finished is
closed by a `runtime.AddCleanup` finalizer rather than leaking the span.

waffle registers the pool under `config/service/sql`, as `di.Fallback()` — a
service that needs different options declares its own.

---

## Installing the `store` Utility

Register it as a module tool, so the version is pinned in `go.mod` alongside the other generators:

```sh
go get -tool gitlab.com/gorib/sql/cmd/store
```

It is then invoked as `go tool store` — which is what the `//go:generate` line in a stored struct file expects:

```go
//go:generate go tool store $GOFILE
```
