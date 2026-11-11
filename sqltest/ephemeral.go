package sqltest

import (
	"context"
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
func (s *server) separate() error {
	if s.suffix == "" || s.suffixed {
		return nil
	}
	name, err := s.databaseName()
	if err != nil {
		return err
	}
	dsn, err := s.host.withDatabase(s.dsn, name+"_"+s.suffix)
	if err != nil {
		return err
	}
	s.dsn, s.suffixed = dsn, true
	return nil
}

func (s *server) personalize() error {
	if !s.ephemeral || s.base != "" {
		return nil
	}
	base, err := s.databaseName()
	if err != nil {
		return err
	}
	dsn, err := s.host.withDatabase(s.dsn, personalizedName(base))
	if err != nil {
		return err
	}
	s.base, s.dsn = base, dsn
	return nil
}

func (s *server) sweep(ctx context.Context) error {
	if !s.ephemeral {
		return nil
	}
	ours, err := s.databaseName()
	if err != nil {
		return err
	}
	admin, err := s.maintenance()
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close() }()

	marks, err := s.host.marks(ctx, admin)
	if err != nil {
		return fmt.Errorf("cannot list the databases of %s: %w", s.where(), err)
	}
	for name, mark := range marks {
		if name == ours || !sweepable(name, s.base) {
			continue
		}
		created, ok := createdAt(mark)
		if !ok || time.Since(created) < sweepAfter {
			continue
		}
		if busy, err := s.host.connected(ctx, admin, name); err != nil || busy {
			continue
		}
		// Best effort: a concurrent sweep may have taken it already.
		_ = s.host.drop(ctx, admin, name)
	}
	return nil
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
