package sql

import "strings"

// What `store:",convert:csv"` calls. Here rather than in the generated file so a
// correction arrives as a library upgrade, not a regeneration of every _gen.go.

// No elements is NULL rather than an empty string, so an absent column reads back
// as absent.
func JoinCsv(v []string) *string {
	if len(v) == 0 {
		return nil
	}
	s := strings.Join(v, ",")
	return &s
}

// An empty string is no elements: strings.Split would answer one empty element
// and put a value in the model that nobody stored. A NOT NULL column defaulting
// to the empty string is the usual way in.
func SplitCsv(v *string) []string {
	if v == nil || *v == "" {
		return nil
	}
	return strings.Split(*v, ",")
}
