package domain

import "time"

type NAVPoint struct {
	SessionDate time.Time
	NAV         Money
}
