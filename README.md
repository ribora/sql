# gorib/sql

`gitlab.com/gorib/sql` is a high-performance SQL toolkit for Go built on zero-runtime-reflection and clean domain model principles.

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

Each driver also accepts the raw SQL spelling (`=`, `!=`, and `@>` on Postgres)
and the usual synonyms (`in`, `neq`, `nin`, `gte`, `lte`).

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

// Subquery in FROM
q := sql.Select().From(sql.Query(sub, "b"))

// Column-to-column comparison
q := sql.Select().From(sql.Table("users")).
    Where(criteria.And().Add("updated_at", "gt", sql.Expr("created_at")))

// COUNT, for Repository.Count
q := sql.Count().From(sql.Table("users")).Where(criteria.And("status", "eq", "active"))
```

### INSERT, UPDATE, DELETE

```go
// INSERT with RETURNING
q := sql.Insert("name").Into(sql.Table("users")).Values("Alice").Returning("id")

// INSERT that updates on a key conflict: the key, then the columns to update
q := sql.Insert("id", "name").Into(sql.Table("users")).Values(1, "Alice").Conflict("id", "name")

// UPDATE with aliases
q := sql.Update(map[string]any{"vip": true}).
    Table(sql.Table("users").As("u")).
    From(sql.Join(sql.Table("orders").As("o"), "u.id = o.user_id")).
    Where(criteria.And().Add("o.total", "gt", 1000))

// Atomic increment via sql.Expr
q := sql.Update(map[string]any{"version": sql.Expr("version + 1")}).
    Table(sql.Table("items")).Where(criteria.And().Add("id", "eq", 1))

// DELETE
q := sql.Delete().From(sql.Table("logs")).Where(criteria.And().Add("created_at", "lt", oldDate))
```

The keys of `Update`'s map name columns of the target table without a
qualifier: a key such as `u.status` is valid in MySQL and refused by Postgres.

### View: a Table With Its Joins

A view declares once what a model is read from: a base table, its joins, and
where the columns that are not the base table's come from. It lives in Go only;
nothing is created in the database. It stands where a table does as the main
source or target of a statement — `Select().From`, `Count().From`,
`Insert().Into`, `Update().Table`, `Delete().From` — but is not itself joined or
aliased: its base table carries the alias.

```go
var contracts = sql.NewView(sql.Table("contracts").As("c"),
    sql.LeftJoin(sql.Table("lines").As("l"), "l.id = c.primary_line_id"),
).
    Column("facility", "l.facility").
    Column("line_kind", "l.kind")

sql.Select().From(contracts).Returning("id", "line_kind").
    Where(criteria.And("facility", "eq", "msk-1").Add("customerId", "eq", 10)).
    Sort("createdAt desc")
// select c.id, l.kind line_kind from contracts c left join lines l on l.id = c.primary_line_id
//   where (l.facility = $1) and (c.customer_id = $2) order by c.created_at desc
```

In `Select` and `Count` every name — returning, where, sort, group — is placed by
one rule: the mappings turn a field into a column, a declared column takes its
expression, and any other column is qualified with the base table. A view
without joins qualifies nothing and builds what its table does. A column may be
any expression, `Column("title", "coalesce(l.name, c.name)")`; it is written in
parentheses, as given, so it is code and never input.

A qualified name such as `l.price` is refused in a condition, a sort or a group
unless a mapping produced it: criteria usually come from outside, and through a
view they would reach every column of every joined table. Declare what may be
reached with `Column`, or map it.

A declared column wins over a base column of the same name, silently: name the
declared columns so they cannot meet one (`line_kind` rather than `kind`).

`Insert().Into`, `Update().Table` and `Delete().From` take a view too and write
its base table, without the alias: the joins and the declared columns take no
part, and a declared column in a write's condition fails in the database. A
field read through the joins is therefore `store:",readonly"` on the stored
struct.

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

// Inserts lists the non-readonly columns with their values;
// Inserts<Group>() does for `inserts:Group` what Updates<Group>() does for updates
func (m *storedUser) Inserts() ([]string, []any) {
    return []string{"email", "last_login", "roles", "name"},
        []any{m.Email, m.LastLogin, m.Roles, m.Name}
}

// Mappings turns a model field name into its column; the repository reads it
func (m *storedUser) Mappings() map[string]string {
    return map[string]string{"id": "id", "email": "email", "lastLogin": "last_login", "roles": "roles", "fullName": "name"}
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

### The Default Sort

`sort:` on the `_` field is what a select is ordered by when it has no `Sort` of
its own — the same items `Sort` takes, separated by `|`:

```go
type storedEvent struct {
    _ *model.Event `store:"sort:created_at desc|id"`
    ...
}
```

It generates `DefaultSort() []string`, which the repository applies. Every
item's column must be a `db` tag of the struct — a `store:"-"` field's counts,
the column is still in the table — and its tail `asc`/`desc` then
`nulls first`/`nulls last`, or the run stops: renaming a column cannot leave the
sort behind.

A select with neither its own `Sort` nor a default sort has no `order by`, and
`Page` on it is an error rather than pages in an arbitrary order.

`go tool store -strict $GOFILE` stops on a stored struct that names no default
sort, so one is declared everywhere. Flags go before the path.

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

Loading a package runs `go list`, so the module has to resolve: dependencies
present, `go.mod` in order. Reading the file with `go/ast` alone needed neither.

### The First Run After Upgrading

A package whose model carries fields the stored struct never did will have those
fields written in, and the run will stop asking for their tags. That is the
generator reporting a mapping that was already incomplete, not a new
requirement — until now those fields were silently zero after `Unwrap()`.

Answer each one and the next run is green. Nothing has to be undone: a run that
stopped wrote no `_gen.go`.

---

## 4. Repository: CRUD Automation

`sql.Repository[Model]` is a high-level generic abstraction that automatically uses the `Scannable` and `Returnable` interfaces.

### Model Requirements

| Interface | Method | Purpose |
|-----------|--------|---------|
| `Scannable` | `Scan() []any` | Returns pointers to fields for `rows.Scan`. |
| `Returnable` | `Returns() []string` | Defines the column list for `SELECT` / `RETURNING`. |

The `store` code generator produces both methods automatically.

### Constructing a Repository

```go
users, err := sql.NewRepository[storedUser](db)
```

`NewRepository` refuses a model whose `Scan` has a value receiver. A model with
`Mappings() map[string]string` — the generator writes it — tells the repository
which column a field name is; `sql.WithExtraMapping(map[string]string)` adds
entries, and a name with no entry becomes snake_case (`customerId` →
`customer_id`). A model with `DefaultSort() []string` supplies the order of a
select that has none.

### Automatic RETURNING

The repository automatically appends `Returning(...)` to `INSERT` and `UPDATE` queries when the driver supports it (e.g., PostgreSQL). This lets you retrieve generated values (such as auto-incremented IDs) in the same round-trip. Where the driver has no `RETURNING` (MySQL), `Get` on an `INSERT` or `UPDATE` runs the statement and returns a zero `Model` with a nil error: read the row back with a select.

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
| `Count(ctx, builder)` | Returns the row count; takes `sql.Count()`. |

### Errors

The four methods above return their errors through `sql.NewError`, which maps
the driver's error onto a sentinel — `ErrNoRows`, `ErrConflict`,
`ErrBadRequest`, `ErrDeadlock`, `ErrNoColumn`, `ErrInternal`, each also matching
`ErrQueryFailed` — and keeps the original in the chain. `sql.FromError` reads a
classified error as returned, without unwrapping:

```go
user, err := users.Get(ctx, q)
if errors.Is(err, sql.ErrNoRows) {
    // not found
}
if e, ok := sql.FromError(err); ok && errors.Is(e, sql.ErrConflict) {
    // e.Field() and e.Constraint() name what conflicted
}
```

---

## 5. Transactions and Savepoints

Transactions are propagated via `context.Context`. A nested one takes a `SAVEPOINT` rather than a second connection, so a repository call inside another transaction joins it.

`InTransaction` commits on success, rolls back on failure, and rolls back a commit that failed — `Commit` marks the transaction done only when it succeeds, so without that last one the transaction stays open.

```go
err := sql.InTransaction(ctx, db, func(ctx context.Context) error {
    if err := invoices.Save(ctx, invoice); err != nil {
        return err
    }
    return outbox.Enqueue(ctx, "invoice.paid", event)
})
```

It takes the connection rather than a repository: a transaction spans every repository it writes through, so making one of them its owner is arbitrary.

In a service, inject `NewTransaction` instead — it has the one method, so the domain package never names `sql`:

```go
// domain/<x>/service — the interface belongs to the consumer
type Transaction interface {
    Run(ctx context.Context, do func(ctx context.Context) error) error
}

// config/di/di.go
di.Wire[service.Transaction](sql.NewTransaction)
```

`repo.Begin(ctx)` still returns the four values for an adapter that needs them, and leaves the commit/rollback pair for you to get right.

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
| `error.type` | metric, when the operation did not succeed | `conflict`, `deadlock`, `no_rows`, `bad_request`, `no_column`, `internal`, `connect`, `tx`, `_OTHER` |
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

## 8. Testing

`gitlab.com/gorib/sql/sqltest` runs a component test over a real Postgres. It
speaks Postgres only; MySQL and SQLite have no counterpart.

```go
func setup(ctx context.Context) (*sqltest.Postgres, error) {
    pg := sqltest.New(
        sqltest.Env("TEST_DB_DSN"),
        sqltest.Docker("postgres:17"),
        sqltest.Database("orders"),
        sqltest.Migrations(migrate), // func(ctx context.Context, db *sql.DB) error
    )
    if err := pg.Provision(ctx); err != nil {
        return nil, err
    }
    return pg, pg.Migrate(ctx)
}
```

`pg.Db()` is the `sql.Db` the repositories take. Do not swap it for a stand-in:
it carries the SQL the repositories build, and a stand-in tests none of it.

- **Address.** The first that resolves: `Dsn`, then the `Env` variables in
  order, then `Docker` (a loopback-only container that outlives the run; it
  needs `Database`). There is no default DSN. When nothing resolves `Provision`
  fails naming the variables to set, and an unreachable database fails it too —
  nothing is skipped. Errors carry host, port and database, never the DSN.
- **`Migrate`** runs `Migrations`, then finds the tables unless `Tables` lists
  them; the bookkeeping tables (`goose_db_version`, `migrations`,
  `schema_migrations`) are left out. The rows the migrations wrote, and where
  each sequence stood, are kept aside in the `sqltest_baseline` schema.
- **`Reset`** puts those tables back as `Migrate` left them: a row a migration
  wrote stays, a scenario's row goes, and ids go on after the seeded ones.
  Putting rows back needs a superuser; migrations that write no rows need none.
  Without `Ephemeral` the database outlives the run, and what the last run left
  in it counts as what the migrations left.
- **`Ephemeral`** puts the process id into the database name so packages run side
  by side, and `Down` drops it. **`Suffix`** gives a second instance its own
  database.
- **`FailWith(err)`** fails every `Db` the resource handed out, with `err` (nil
  reads as a refused connection), until the next `Reset`.

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
