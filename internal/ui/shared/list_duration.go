package shared

import (
	"strconv"
	"time"
)

// ListDuration renders compact list durations without seconds above a minute.
func ListDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		days := int(d / (24 * time.Hour))
		hours := int(d%(24*time.Hour)) / int(time.Hour)
		if hours > 0 {
			return strconv.Itoa(days) + "d" + strconv.Itoa(hours) + "h"
		}
		return strconv.Itoa(days) + "d"
	case d >= time.Hour:
		h := int(d / time.Hour)
		mins := int(d%time.Hour) / int(time.Minute)
		if mins > 0 {
			return strconv.Itoa(h) + "h" + strconv.Itoa(mins) + "m"
		}
		return strconv.Itoa(h) + "h"
	case d >= time.Minute:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	default:
		return strconv.Itoa(int(d/time.Second)) + "s"
	}
}
