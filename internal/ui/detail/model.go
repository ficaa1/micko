package detail

import (
	"fmt"
	"time"
)

func phaseName(p string) string {
	if p == "" {
		return "not started"
	}
	return p
}

// humanDuration renders a duration in coarse human units, deterministically
// (seconds < 1m, minutes < 1h, hours < 24h, otherwise days).
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d / (24 * time.Hour))
		h := int((d % (24 * time.Hour)) / time.Hour)
		if h == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd%dh", days, h)
	}
}
