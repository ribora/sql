package sql_test

import (
	"slices"
	"testing"

	"github.com/ribora/sql"
)

func TestJoinCsv(t *testing.T) {
	cases := []struct {
		label string
		in    []string
		want  *string
	}{
		{label: "nil", in: nil, want: nil},
		{label: "empty", in: []string{}, want: nil},
		{label: "one", in: []string{"go"}, want: ptr("go")},
		{label: "several", in: []string{"go", "sql"}, want: ptr("go,sql")},
	}
	for _, c := range cases {
		got := sql.JoinCsv(c.in)
		switch {
		case got == nil && c.want == nil:
		case got == nil || c.want == nil:
			t.Errorf("%s: JoinCsv(%v) = %v, want %v", c.label, c.in, str(got), str(c.want))
		case *got != *c.want:
			t.Errorf("%s: JoinCsv(%v) = %q, want %q", c.label, c.in, *got, *c.want)
		}
	}
}

func TestSplitCsv(t *testing.T) {
	cases := []struct {
		label string
		in    *string
		want  []string
	}{
		{label: "null", in: nil, want: nil},
		// A NOT NULL column defaults to '', and strings.Split would answer one
		// empty element for it.
		{label: "empty string", in: ptr(""), want: nil},
		{label: "one", in: ptr("go"), want: []string{"go"}},
		{label: "several", in: ptr("go,sql"), want: []string{"go", "sql"}},
		// Not trimmed and not filtered: the column holds what was stored, and
		// altering it here would corrupt data on the way out.
		{label: "spaces kept", in: ptr("go, sql"), want: []string{"go", " sql"}},
		{label: "empty parts kept", in: ptr("go,,sql"), want: []string{"go", "", "sql"}},
	}
	for _, c := range cases {
		if got := sql.SplitCsv(c.in); !slices.Equal(got, c.want) {
			t.Errorf("%s: SplitCsv(%v) = %#v, want %#v", c.label, str(c.in), got, c.want)
		}
	}
}

// Whatever survives one round trip survives every later one.
func TestCsvRoundTripIsStable(t *testing.T) {
	for _, in := range [][]string{nil, {}, {"go"}, {"go", "sql"}} {
		once := sql.SplitCsv(sql.JoinCsv(in))
		twice := sql.SplitCsv(sql.JoinCsv(once))
		if !slices.Equal(once, twice) {
			t.Errorf("%#v: first trip %#v, second %#v", in, once, twice)
		}
	}
}

func ptr(s string) *string { return &s }

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
