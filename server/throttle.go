package server

import (
	"sync"
	"time"
)

// paceThrottler serializes calls and enforces a minimum gap between
// consecutive Wait() returns. It is the server-side mechanism that prevents
// rapid-fire commands (e.g. multi-move or full sync MQTT bursts) from
// overwhelming a printer that processes them one at a time.
//
// Usage: call Wait() before performing the rate-limited operation. The first
// Wait returns immediately; subsequent Waits block until `gap` has elapsed
// since the previous Wait return. Safe for concurrent use — overlapping
// callers queue on the internal mutex in arrival order.
type paceThrottler struct {
	mu   sync.Mutex
	gap  time.Duration
	last time.Time
}

// newPaceThrottler returns a throttler that enforces at least `gap` between
// successive Wait() returns. A zero or negative gap reduces Wait to a pure
// serialization point with no sleep.
func newPaceThrottler(gap time.Duration) *paceThrottler {
	return &paceThrottler{gap: gap}
}

// Wait blocks until the throttler admits the caller. The caller is expected
// to perform its rate-limited operation immediately after Wait returns;
// while the caller holds no lock, the throttler internally records the
// admission time so the next Wait paces relative to it.
func (p *paceThrottler) Wait() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.last.IsZero() && p.gap > 0 {
		if elapsed := time.Since(p.last); elapsed < p.gap {
			time.Sleep(p.gap - elapsed)
		}
	}
	p.last = time.Now()
}
