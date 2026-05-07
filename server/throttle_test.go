package server

import (
	"sync"
	"testing"
	"time"
)

func TestPaceThrottlerFirstWaitReturnsImmediately(t *testing.T) {
	p := newPaceThrottler(50 * time.Millisecond)

	start := time.Now()
	p.Wait()
	elapsed := time.Since(start)

	if elapsed > 5*time.Millisecond {
		t.Errorf("first Wait took %v, expected near-zero", elapsed)
	}
}

func TestPaceThrottlerEnforcesGapBetweenCalls(t *testing.T) {
	gap := 30 * time.Millisecond
	p := newPaceThrottler(gap)

	p.Wait()
	start := time.Now()
	p.Wait()
	elapsed := time.Since(start)

	if elapsed < gap-2*time.Millisecond {
		t.Errorf("second Wait elapsed %v, expected at least %v", elapsed, gap)
	}
	if elapsed > gap*3 {
		t.Errorf("second Wait elapsed %v, took far longer than expected gap %v", elapsed, gap)
	}
}

func TestPaceThrottlerNoSleepWhenGapAlreadyElapsed(t *testing.T) {
	gap := 10 * time.Millisecond
	p := newPaceThrottler(gap)

	p.Wait()
	time.Sleep(gap * 2) // simulate slow caller; gap has elapsed by the time we re-enter

	start := time.Now()
	p.Wait()
	elapsed := time.Since(start)

	if elapsed > 5*time.Millisecond {
		t.Errorf("Wait slept for %v despite gap already elapsed", elapsed)
	}
}

func TestPaceThrottlerSerializesConcurrentCallers(t *testing.T) {
	// Three goroutines fire Wait simultaneously. With a 30ms gap, the three
	// returns must be spaced at least ~30ms apart — otherwise they're racing
	// past the gap.
	gap := 30 * time.Millisecond
	p := newPaceThrottler(gap)

	const N = 3
	returnTimes := make([]time.Time, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			p.Wait()
			returnTimes[idx] = time.Now()
		}(i)
	}
	wg.Wait()

	// Sort returnTimes so we can compare consecutive pairs regardless of
	// goroutine scheduling order.
	sorted := append([]time.Time(nil), returnTimes...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Before(sorted[j-1]); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	for i := 1; i < N; i++ {
		delta := sorted[i].Sub(sorted[i-1])
		if delta < gap-2*time.Millisecond {
			t.Errorf("returns %d and %d only %v apart, expected at least %v", i-1, i, delta, gap)
		}
	}
}

func TestPaceThrottlerZeroGapStillSerializes(t *testing.T) {
	// gap == 0: Wait should not sleep, but it must still hold the mutex,
	// so concurrent calls don't run reentrant publish logic. We can't
	// directly observe mutex contention, but we can confirm zero-gap is
	// non-blocking.
	p := newPaceThrottler(0)

	start := time.Now()
	p.Wait()
	p.Wait()
	p.Wait()
	elapsed := time.Since(start)

	if elapsed > 5*time.Millisecond {
		t.Errorf("zero-gap throttler took %v across 3 calls, expected near-zero", elapsed)
	}
}
