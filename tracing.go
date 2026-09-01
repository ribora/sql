package sql

import (
	"gitlab.com/gorib/hilton"
)

// Per-driver rather than via DbSql(): on the pgx path that opens a fresh
// *sql.DB unrelated to the live pool, so its Stats() are always empty.
type poolConner interface {
	poolConns() (idle, used int64, ok bool)
}

// Separate from NewDb because the gauge reads at collection time, so it needs a
// provider for the pool's lifetime, not the per-operation one from the context.
func NewPoolTracing(provider hilton.Provider, db Db) (*PoolTracing, error) {
	conner, ok := db.(poolConner)
	if !ok {
		// A gauge reporting zeroes is worse than no gauge.
		return &PoolTracing{}, nil
	}

	system := "sql"
	if named, ok := db.(interface{ systemName() string }); ok {
		system = named.systemName()
	}

	// One instrument split by state, not two: both values then come from a
	// single snapshot and cannot disagree.
	reg, err := provider.Meter(instrumentationName).ObservableGauge(
		"db.client.connection.count",
		func(o hilton.Observer) {
			idle, used, ok := conner.poolConns()
			if !ok {
				return
			}
			o.Observe(idle, connectionState(system, "idle")...)
			o.Observe(used, connectionState(system, "used")...)
		},
		hilton.WithDescription("Number of connections in the SQL pool"),
		hilton.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}
	return &PoolTracing{registration: reg}, nil
}

func connectionState(system, state string) []hilton.Attribute {
	return []hilton.Attribute{
		hilton.Attr("db.system.name", system),
		hilton.Attr("db.client.connection.state", state),
	}
}

type PoolTracing struct {
	registration hilton.Registration
}

func (t *PoolTracing) Close() error {
	if t.registration == nil {
		return nil
	}
	reg := t.registration
	t.registration = nil
	return reg.Unregister()
}
