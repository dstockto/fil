package server

import (
	"sync"
	"testing"
	"time"
)

func prusaStatus(state string) map[string]interface{} {
	return map[string]interface{}{
		"printer": map[string]interface{}{"state": state},
	}
}

// TestPrusaOfflineToFinishedSuppressed mirrors the Bambu test: the constructor
// seeds state.State="offline" and poll() resets it to "offline" on fetch error,
// so the first status after (re)connect could fire a spurious "print finished"
// callback and stamp LastFinishedAt with the restart time if the printer is
// parked at FINISHED.
func TestPrusaOfflineToFinishedSuppressed(t *testing.T) {
	p := NewPrusaAdapter("test", "127.0.0.1", "u", "pw")
	if p.state.State != "offline" {
		t.Fatalf("precondition: expected constructor to seed state=offline, got %q", p.state.State)
	}

	var fired []StateChangeEvent
	var mu sync.Mutex
	p.OnStateChange(func(e StateChangeEvent) {
		mu.Lock()
		fired = append(fired, e)
		mu.Unlock()
	})

	oldState := p.applyStatusUpdate(prusaStatus("FINISHED"))
	p.dispatchStateChange(oldState)

	if got := p.state.State; got != "finished" {
		t.Fatalf("expected state advanced to 'finished', got %q", got)
	}
	if !p.state.LastFinishedAt.IsZero() {
		t.Errorf("expected LastFinishedAt zero on offline->finished first observation, got %v", p.state.LastFinishedAt)
	}

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 0 {
		t.Errorf("expected no state-change callback on offline->finished, got %d: %+v", len(fired), fired)
	}
}

// TestPrusaOfflineThenFinishedThenPrintingThenFinished exercises the full
// restart-during-FINISHED sequence: first FINISHED is suppressed, subsequent
// PRINTING and FINISHED are real transitions and must fire callbacks.
func TestPrusaOfflineThenFinishedThenPrintingThenFinished(t *testing.T) {
	p := NewPrusaAdapter("test", "127.0.0.1", "u", "pw")

	var fired []StateChangeEvent
	var mu sync.Mutex
	p.OnStateChange(func(e StateChangeEvent) {
		mu.Lock()
		fired = append(fired, e)
		mu.Unlock()
	})

	// 1. offline -> finished: suppressed
	old := p.applyStatusUpdate(prusaStatus("FINISHED"))
	p.dispatchStateChange(old)
	if !p.state.LastFinishedAt.IsZero() {
		t.Errorf("after first FINISHED from offline: LastFinishedAt should be zero, got %v", p.state.LastFinishedAt)
	}

	// 2. finished -> printing: real transition, fires
	old = p.applyStatusUpdate(prusaStatus("PRINTING"))
	p.dispatchStateChange(old)

	// 3. printing -> finished: real transition, fires AND stamps LastFinishedAt
	before := time.Now()
	old = p.applyStatusUpdate(prusaStatus("FINISHED"))
	p.dispatchStateChange(old)
	after := time.Now()

	if p.state.LastFinishedAt.IsZero() {
		t.Fatal("LastFinishedAt should be set after the second FINISHED (real transition)")
	}
	if p.state.LastFinishedAt.Before(before) || p.state.LastFinishedAt.After(after) {
		t.Errorf("LastFinishedAt %v not within [%v, %v]", p.state.LastFinishedAt, before, after)
	}

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()

	transitions := make(map[string]int)
	for _, e := range fired {
		transitions[e.OldState+"->"+e.NewState]++
	}
	if got := transitions["finished->printing"]; got != 1 {
		t.Errorf("finished->printing should fire exactly once; got %d (all: %v)", got, transitions)
	}
	if got := transitions["printing->finished"]; got != 1 {
		t.Errorf("printing->finished should fire exactly once; got %d (all: %v)", got, transitions)
	}
	if got := transitions["offline->finished"]; got != 0 {
		t.Errorf("offline->finished must NOT fire; got %d (all: %v)", got, transitions)
	}
	if len(fired) != 2 {
		t.Errorf("expected exactly 2 callbacks, got %d: %+v", len(fired), fired)
	}
}

func TestPrusaLastFinishedAtSetOnFinishTransition(t *testing.T) {
	p := NewPrusaAdapter("test", "127.0.0.1", "u", "pw")

	p.applyStatusUpdate(prusaStatus("PRINTING"))
	if got := p.state.State; got != "printing" {
		t.Fatalf("expected state 'printing', got %q", got)
	}
	if !p.state.LastFinishedAt.IsZero() {
		t.Fatal("expected LastFinishedAt zero before any FINISH")
	}

	before := time.Now()
	p.applyStatusUpdate(prusaStatus("FINISHED"))
	after := time.Now()

	if got := p.state.State; got != "finished" {
		t.Fatalf("expected state 'finished', got %q", got)
	}
	if p.state.LastFinishedAt.Before(before) || p.state.LastFinishedAt.After(after) {
		t.Errorf("LastFinishedAt %v not within [%v, %v]", p.state.LastFinishedAt, before, after)
	}
}

func TestPrusaLastFinishedAtNotOverwrittenOnRepeat(t *testing.T) {
	p := NewPrusaAdapter("test", "127.0.0.1", "u", "pw")
	p.applyStatusUpdate(prusaStatus("PRINTING"))
	p.applyStatusUpdate(prusaStatus("FINISHED"))
	first := p.state.LastFinishedAt

	time.Sleep(10 * time.Millisecond)
	p.applyStatusUpdate(prusaStatus("FINISHED"))
	if !p.state.LastFinishedAt.Equal(first) {
		t.Errorf("LastFinishedAt should not move on repeat FINISHED; got %v want %v", p.state.LastFinishedAt, first)
	}
}

// TestPrusaLastStartedAtSetOnPrintTransition mirrors the Bambu test: plan
// history durations must come from when the printer began, not from
// plate.StartedAt (when the user ran `fil p n`).
func TestPrusaLastStartedAtSetOnPrintTransition(t *testing.T) {
	p := NewPrusaAdapter("test", "127.0.0.1", "u", "pw")

	// offline -> finished: first observation after connect, not a start.
	p.applyStatusUpdate(prusaStatus("FINISHED"))
	if !p.state.LastStartedAt.IsZero() {
		t.Fatalf("expected LastStartedAt zero before any print start, got %v", p.state.LastStartedAt)
	}

	before := time.Now()
	p.applyStatusUpdate(prusaStatus("PRINTING"))
	after := time.Now()

	if p.state.LastStartedAt.IsZero() {
		t.Fatal("LastStartedAt should be set on finished->printing")
	}
	if p.state.LastStartedAt.Before(before) || p.state.LastStartedAt.After(after) {
		t.Errorf("LastStartedAt %v not within [%v, %v]", p.state.LastStartedAt, before, after)
	}
}

// TestPrusaOfflineToPrintingSuppressed: poll() resets to "offline" on fetch
// error, so a status arriving mid-print is a first observation. Stamping it
// would under-report the duration.
func TestPrusaOfflineToPrintingSuppressed(t *testing.T) {
	p := NewPrusaAdapter("test", "127.0.0.1", "u", "pw")

	p.applyStatusUpdate(prusaStatus("PRINTING"))

	if p.state.State != "printing" {
		t.Errorf("state should still advance to printing, got %q", p.state.State)
	}
	if !p.state.LastStartedAt.IsZero() {
		t.Errorf("expected LastStartedAt zero on offline->printing first observation, got %v", p.state.LastStartedAt)
	}
}

// TestPrusaLastStartedAtSurvivesPauseResume: a resume is not a new print. Note
// that ATTENTION also normalizes to "paused" on the Prusa, so filament-runout
// recovery must not restamp the start either.
func TestPrusaLastStartedAtSurvivesPauseResume(t *testing.T) {
	p := NewPrusaAdapter("test", "127.0.0.1", "u", "pw")

	p.applyStatusUpdate(prusaStatus("IDLE"))
	p.applyStatusUpdate(prusaStatus("PRINTING"))
	first := p.state.LastStartedAt
	if first.IsZero() {
		t.Fatal("precondition: LastStartedAt should be set after idle->printing")
	}

	p.applyStatusUpdate(prusaStatus("ATTENTION"))
	p.applyStatusUpdate(prusaStatus("PRINTING"))

	if !p.state.LastStartedAt.Equal(first) {
		t.Errorf("LastStartedAt should not move on resume; got %v want %v", p.state.LastStartedAt, first)
	}
}

// TestPrusaLastStartedAtNotOverwrittenOnRepeat: poll() runs every 30s during a
// print and must not push the start forward.
func TestPrusaLastStartedAtNotOverwrittenOnRepeat(t *testing.T) {
	p := NewPrusaAdapter("test", "127.0.0.1", "u", "pw")

	p.applyStatusUpdate(prusaStatus("IDLE"))
	p.applyStatusUpdate(prusaStatus("PRINTING"))
	first := p.state.LastStartedAt

	p.applyStatusUpdate(prusaStatus("PRINTING"))
	p.applyStatusUpdate(prusaStatus("PRINTING"))

	if !p.state.LastStartedAt.Equal(first) {
		t.Errorf("LastStartedAt should not move on repeat PRINTING; got %v want %v", p.state.LastStartedAt, first)
	}
}
