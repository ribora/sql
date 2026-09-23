package sqltest

import (
	"context"
	dbSql "database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	createdMark = "sqltest " // followed by the creation time, RFC 3339

	// For runs that were killed before Down. Generous because the drop is forced:
	// "idle and older than this" must be impossible for a merely slow run.
	sweepAfter = time.Hour
)

func personalizedName(base string) string {
	return base + "_" + strconv.Itoa(os.Getpid())
}

// Before personalize, so the suffix is part of the base the sweep matches on.
func (p *Postgres) separate() error {
	if p.suffix == "" || p.suffixed {
		return nil
	}
	name, err := p.databaseName()
	if err != nil {
		return err
	}
	dsn, err := withDatabase(p.dsn, name+"_"+p.suffix)
	if err != nil {
		return err
	}
	p.dsn, p.suffixed = dsn, true
	return nil
}

func (p *Postgres) personalize() error {
	if !p.ephemeral || p.base != "" {
		return nil
	}
	base, err := p.databaseName()
	if err != nil {
		return err
	}
	dsn, err := withDatabase(p.dsn, personalizedName(base))
	if err != nil {
		return err
	}
	p.base, p.dsn = base, dsn
	return nil
}

func (p *Postgres) sweep(ctx context.Context) error {
	if !p.ephemeral {
		return nil
	}
	ours, err := p.databaseName()
	if err != nil {
		return err
	}
	handle, err := p.maintenance()
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()

	rows, err := handle.QueryContext(ctx, `
		select d.datname, coalesce(shobj_description(d.oid, 'pg_database'), '')
		from pg_database d where d.datistemplate = false`)
	if err != nil {
		return fmt.Errorf("cannot list the databases of %s: %w", address(p.dsn), err)
	}

	var stale []string
	for rows.Next() {
		var name, comment string
		if err = rows.Scan(&name, &comment); err != nil {
			_ = rows.Close()
			return err
		}
		if name == ours || !sweepable(name, p.base) {
			continue
		}
		created, ok := createdAt(comment)
		if !ok || time.Since(created) < sweepAfter {
			continue
		}
		stale = append(stale, name)
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}

	for _, name := range stale {
		if busy, err := connected(ctx, handle, name); err != nil || busy {
			continue
		}
		// Best effort: a concurrent sweep may have taken it already.
		_, _ = handle.ExecContext(ctx, "drop database if exists "+quoteIdent(name)+" with (force)")
	}
	return nil
}

func connected(ctx context.Context, handle *dbSql.DB, name string) (bool, error) {
	var count int
	row := handle.QueryRowContext(ctx, "select count(*) from pg_stat_activity where datname = $1", name)
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// Matched in Go rather than with LIKE: the base contains underscores, which
// LIKE would treat as single-character wildcards.
func sweepable(name, base string) bool {
	if base == "" {
		return false
	}
	suffix, found := strings.CutPrefix(name, base+"_")
	if !found || suffix == "" {
		return false
	}
	for _, digit := range suffix {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func createdAt(comment string) (time.Time, bool) {
	stamp, found := strings.CutPrefix(comment, createdMark)
	if !found {
		return time.Time{}, false
	}
	created, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return created, true
}
