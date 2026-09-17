package model

import "time"

type Status int

type Doc struct {
	ID   int
	At   time.Time
	Kind Status
}
