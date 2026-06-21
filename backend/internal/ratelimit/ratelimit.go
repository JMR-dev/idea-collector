// Package ratelimit provides a simple per-key (per-IP) token-bucket limiter,
// used to throttle auth-code attempts and slow brute-force guessing.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type Limiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     rate.Limit
	burst    int
	ttl      time.Duration
	now      func() time.Time
	stop     chan struct{}
}

// New builds a limiter allowing perMinute requests per key with the given burst.
func New(perMinute float64, burst int) *Limiter {
	if perMinute <= 0 {
		perMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	l := &Limiter{
		visitors: make(map[string]*visitor),
		rate:     rate.Limit(perMinute / 60.0),
		burst:    burst,
		ttl:      10 * time.Minute,
		now:      time.Now,
		stop:     make(chan struct{}),
	}
	go l.janitor()
	return l
}

// Allow reports whether a request for key may proceed now.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	v, ok := l.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.visitors[key] = v
	}
	v.lastSeen = l.now()
	lim := v.limiter
	l.mu.Unlock()
	return lim.Allow()
}

// Close stops the background cleanup goroutine.
func (l *Limiter) Close() { close(l.stop) }

func (l *Limiter) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			cutoff := l.now().Add(-l.ttl)
			l.mu.Lock()
			for k, v := range l.visitors {
				if v.lastSeen.Before(cutoff) {
					delete(l.visitors, k)
				}
			}
			l.mu.Unlock()
		}
	}
}
