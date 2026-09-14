package app

import (
	"testing"
	"time"
)

// Every relative time on screen is measured from the clock: a workflow's
// age, and how long the snapshot has been stale. A clock that does not move
// reports the same age forever, which reads as a workflow frozen at 0s.
func TestTheSystemClockMoves(t *testing.T) {
	c := SystemClock{}
	first := c.Now()
	if first.IsZero() {
		t.Fatal("the system clock reported the zero time")
	}
	if first.Location() != time.UTC {
		t.Fatalf("the system clock reported %s, want UTC", first.Location())
	}
	deadline := time.Now().Add(time.Second)
	for c.Now().Equal(first) {
		if time.Now().After(deadline) {
			t.Fatal("the system clock did not advance within a second")
		}
	}
}
