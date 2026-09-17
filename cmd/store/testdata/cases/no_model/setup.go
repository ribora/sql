package no_model

import "time"

// A read model: projected by a query, never turned back into a domain type. Both
// constructors are declined, so there is nothing to check the fields against.
type StoredReading struct {
	_ any `store:"nonew,nounwrap"`

	At    time.Time `db:"at"`
	Event string    `db:"event"`
}
