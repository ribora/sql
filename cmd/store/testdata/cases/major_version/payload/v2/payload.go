// A module path ending in /v2 names its package by the element before the
// version, so the qualifier a file writes is `payload`, not `v2`.
package payload

type Doc struct {
	ID int
}
