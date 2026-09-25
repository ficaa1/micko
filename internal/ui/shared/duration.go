package shared

import (
	"fmt"
	"time"
)

// ShortDuration renders a duration to the second while it is short and to
// the minute once it is long, in at most six cells: 42s, 4m12s, 2h05m, 3d04h.
// The Nodes tab, the Timeline and the Explain section all state durations
// with it, so one run reads the same in each of them.
func ShortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int64(d / time.Second)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	case s < 86400:
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	default:
		return fmt.Sprintf("%dd%02dh", s/86400, s%86400/3600)
	}
}
