package main

import (
	"slices"
	"testing"
)

// A trailing vN is ambiguous, so both readings are registered and the qualifier
// the source writes picks one. Deciding it here would be a guess either way.
func TestPathCandidates(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"gitlab.com/gorib/sql", []string{"sql"}},
		{"github.com/jackc/pgx/v5", []string{"v5", "pgx"}},
		{"gitlab.com/x/y/v2", []string{"v2", "y"}},
		{"k8s.io/api/core/v1", []string{"v1", "core"}},
		{"example.com/vault", []string{"vault"}},
		{"example.com/v2x", []string{"v2x"}},
		{"single", []string{"single"}},
	}
	for _, c := range cases {
		if got := pathCandidates(c.path); !slices.Equal(got, c.want) {
			t.Errorf("pathCandidates(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
