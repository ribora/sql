// A project that puts its own package in front of github.com/ribora/sql. What the
// generated file calls is whatever the author's field named, so this has to carry
// the same handful of functions.
package wrapsql

import "github.com/ribora/sql"

type Json[T any] = sql.Json[T]

func NewJson[T any](value T) *Json[T] { return sql.NewJson(value) }

func JoinCsv(v []string) *string { return sql.JoinCsv(v) }

func SplitCsv(v *string) []string { return sql.SplitCsv(v) }
