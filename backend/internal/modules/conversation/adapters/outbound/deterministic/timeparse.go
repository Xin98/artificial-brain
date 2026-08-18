package deterministic

import "time"

// atLocal returns the UTC instant of hour:00 on the local date of now plus
// dayOffset days, interpreted in loc. It is the only date arithmetic the
// corpus performs, keeping every resolved instant deterministic.
func atLocal(now time.Time, loc *time.Location, dayOffset, hour int) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day()+dayOffset, hour, 0, 0, 0, loc).UTC()
}
