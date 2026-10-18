package collector

import (
	"sync"
	"time"
)

// Automatic retries (a DNS name to update, a paused device's new address)
// back off while a device keeps refusing them: after each failure in a row
// the next attempt waits twice as long, from a minute up to RetryMax.

// RetryMax is the longest wait between two automatic retries.
const RetryMax = 6 * time.Hour

type retries struct {
	mu sync.Mutex
	m  map[string]retryState
}

type retryState struct {
	failures int
	next     time.Time
}

// due reports whether the retry key may be attempted now.
func (r *retries) due(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.m[key]
	return !ok || !now.Before(s.next)
}

// done records an attempt: a success forgets the key, a failure pushes the
// next attempt further away.
func (r *retries) done(key string, now time.Time, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		delete(r.m, key)
		return
	}
	if r.m == nil {
		r.m = map[string]retryState{}
	}
	s := r.m[key]
	s.failures++
	wait := RetryMax
	if s.failures <= 9 { // 2^9 minutes is past RetryMax
		wait = min(time.Minute<<(s.failures-1), RetryMax)
	}
	s.next = now.Add(wait)
	r.m[key] = s
}
