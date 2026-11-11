package sqltest

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Outside current_schema(), like the baseline, so discover never lists it.
const refusalSchema = "sqltest_refusal"

const refusalSetup = `
create schema if not exists ` + refusalSchema + `;
create table if not exists ` + refusalSchema + `.quota (
	relation oid,
	name text,
	remaining int not null,
	primary key (relation, name)
);
create or replace function ` + refusalSchema + `.refuse() returns trigger language plpgsql as $$
declare
	rest int;
begin
	update ` + refusalSchema + `.quota set remaining = remaining - 1
	where relation = tg_relid and name = tg_name
	returning remaining into rest;
	if rest < 0 then
		raise exception 'sqltest: % on % is refused', lower(tg_op), tg_table_name;
	end if;
	if tg_op = 'DELETE' then
		return old;
	end if;
	return new;
end $$;`

type Write string

const (
	Insert Write = "insert"
	Update Write = "update"
	Delete Write = "delete"
)

var writes = []Write{Insert, Update, Delete}

func (p *Postgres) Refuse(ctx context.Context, table string, refused ...Write) error {
	return p.RefuseAfter(ctx, table, 0, refused...)
}

func (p *Postgres) RefuseAfter(ctx context.Context, table string, rows int, refused ...Write) error {
	return p.refuse(ctx, table, rows, refused, "", func(name, events string) string {
		return "create trigger " + name + " before " + events + " on " + quoteRelation(table) + " for each row"
	})
}

func (p *Postgres) RefuseCommit(ctx context.Context, table string, refused ...Write) error {
	return p.refuse(ctx, table, 0, refused, "commit_", func(name, events string) string {
		return "create constraint trigger " + name + " after " + events + " on " + quoteRelation(table) +
			" deferrable initially deferred for each row"
	})
}

func (p *Postgres) refuse(ctx context.Context, table string, rows int, refused []Write, kind string, trigger func(name, events string) string) error {
	events, err := triggerEvents(refused)
	if err != nil {
		return err
	}
	name := "sqltest_refuse_" + kind + strings.Join(events, "_")

	tx, err := p.handle.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, refusalSetup); err != nil {
		return fmt.Errorf("cannot prepare the refusal on %s: %w", address(p.dsn), err)
	}
	if _, err = tx.ExecContext(ctx, `
		insert into `+refusalSchema+`.quota (relation, name, remaining) values ($1::regclass, $2, $3)
		on conflict (relation, name) do update set remaining = excluded.remaining`,
		quoteRelation(table), name, rows); err != nil {
		return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
	}
	if _, err = tx.ExecContext(ctx, "drop trigger if exists "+name+" on "+quoteRelation(table)); err != nil {
		return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
	}
	if _, err = tx.ExecContext(ctx, trigger(name, strings.Join(events, " or "))+
		" execute function "+refusalSchema+".refuse()"); err != nil {
		return fmt.Errorf("cannot refuse writes to %s: %w", table, err)
	}
	return tx.Commit()
}

func (p *Postgres) lift(ctx context.Context) error {
	if _, err := p.handle.ExecContext(ctx, "drop schema if exists "+refusalSchema+" cascade"); err != nil {
		return fmt.Errorf("cannot lift the refusals on %s: %w", address(p.dsn), err)
	}
	return nil
}

func quoteRelation(table string) string {
	if schema, name, qualified := strings.Cut(table, "."); qualified {
		return quoteIdent(schema) + "." + quoteIdent(name)
	}
	return quoteIdent(table)
}

func triggerEvents(refused []Write) ([]string, error) {
	for _, write := range refused {
		if !slices.Contains(writes, write) {
			return nil, fmt.Errorf("sqltest: %q is not a write, refuse an insert, update or delete", write)
		}
	}
	var events []string
	for _, write := range writes {
		if len(refused) == 0 || slices.Contains(refused, write) {
			events = append(events, string(write))
		}
	}
	return events, nil
}
