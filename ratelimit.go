package main

import (
	"sync"
	"time"
)

var (
	rateMu           sync.RWMutex
	rateLimitedUntil time.Time
)

func setRateLimit(d time.Duration) {
	rateMu.Lock()
	rateLimitedUntil = time.Now().Add(d)
	rateMu.Unlock()
}
func rateStatus() (bool, int) {
	rateMu.RLock()
	defer rateMu.RUnlock()
	if time.Now().Before(rateLimitedUntil) {
		return true, int(time.Until(rateLimitedUntil).Minutes()) + 1
	}
	return false, 0
}
