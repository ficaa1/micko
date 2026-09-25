package shared

import (
	"testing"
	"time"
)

func TestShortDuration(t *testing.T) {
	cases := map[time.Duration]string{
		-time.Second:                   "0s",
		0:                              "0s",
		42 * time.Second:               "42s",
		4*time.Minute + 12*time.Second: "4m12s",
		4*time.Minute + 5*time.Second:  "4m05s",
		2*time.Hour + 5*time.Minute:    "2h05m",
		75 * time.Hour:                 "3d03h",
	}
	for d, want := range cases {
		if got := ShortDuration(d); got != want {
			t.Errorf("ShortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
