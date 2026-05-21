package server

import (
	"sync"
	"testing"
	"time"
)

// reportPayload returns a minimal MQTT report JSON with the given gcode_state.
func reportPayload(gcodeState string) []byte {
	return []byte(`{"print":{"gcode_state":"` + gcodeState + `"}}`)
}

// TestBambuOfflineToFinishedSuppressed covers the restart/reconnect false-fire:
// the constructor seeds state.State="offline" and ConnectionLostHandler resets it
// to "offline" mid-life, so the first report after (re)connect arrives with
// oldState="offline". If the printer is parked at FINISH between prints, that
// arrives as gcode_state=FINISH. Treating "offline" as a real prior state would
// fire a spurious state-change callback (announcing "print finished" on every
// restart) and stamp LastFinishedAt with the restart time (corrupting plan-history
// FinishedAt). Neither should happen.
func TestBambuOfflineToFinishedSuppressed(t *testing.T) {
	b := NewBambuAdapter("test", "127.0.0.1", "00M00A000000000", "12345678")
	if b.state.State != "offline" {
		t.Fatalf("precondition: expected constructor to seed state=offline, got %q", b.state.State)
	}

	var fired []StateChangeEvent
	var mu sync.Mutex
	b.OnStateChange(func(e StateChangeEvent) {
		mu.Lock()
		fired = append(fired, e)
		mu.Unlock()
	})

	b.handleReport(reportPayload("FINISH"))

	// The state itself should advance to "finished" — we want the adapter to know
	// what the printer is doing — but the transition is "first observation",
	// not a print-completion event.
	if got := b.state.State; got != "finished" {
		t.Fatalf("expected state advanced to 'finished' after first report, got %q", got)
	}
	if !b.state.LastFinishedAt.IsZero() {
		t.Errorf("expected LastFinishedAt zero on offline->finished first observation, got %v", b.state.LastFinishedAt)
	}

	// Callbacks fire asynchronously (go cb(event) in handleReport); give them a
	// moment to land if any were dispatched, then assert none did.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 0 {
		t.Errorf("expected no state-change callback on offline->finished, got %d: %+v", len(fired), fired)
	}
}

// TestBambuOfflineThenFinishThenRunningThenFinish exercises the full sequence
// that triggers a real print completion after a restart-during-FINISH. The
// first FINISH (offline prior) is suppressed; the subsequent RUNNING and
// FINISH are real transitions and must fire callbacks. LastFinishedAt should
// be set on the second FINISH only.
func TestBambuOfflineThenFinishThenRunningThenFinish(t *testing.T) {
	b := NewBambuAdapter("test", "127.0.0.1", "00M00A000000000", "12345678")

	var fired []StateChangeEvent
	var mu sync.Mutex
	b.OnStateChange(func(e StateChangeEvent) {
		mu.Lock()
		fired = append(fired, e)
		mu.Unlock()
	})

	// 1. offline -> finished: suppressed (the bug fix)
	b.handleReport(reportPayload("FINISH"))
	if !b.state.LastFinishedAt.IsZero() {
		t.Errorf("after first FINISH from offline: LastFinishedAt should be zero, got %v", b.state.LastFinishedAt)
	}

	// 2. finished -> printing: real transition, fires
	b.handleReport(reportPayload("RUNNING"))

	// 3. printing -> finished: real transition, fires AND stamps LastFinishedAt
	before := time.Now()
	b.handleReport(reportPayload("FINISH"))
	after := time.Now()

	if b.state.LastFinishedAt.IsZero() {
		t.Fatal("LastFinishedAt should be set after the second FINISH (real transition)")
	}
	if b.state.LastFinishedAt.Before(before) || b.state.LastFinishedAt.After(after) {
		t.Errorf("LastFinishedAt %v not within [%v, %v]", b.state.LastFinishedAt, before, after)
	}

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()

	// Callbacks fire in goroutines, so order isn't deterministic — assert the
	// set of transitions instead. Each expected transition should appear once,
	// and the suppressed offline->finished must NOT appear.
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

func TestBambuLastFinishedAtSetOnFinishTransition(t *testing.T) {
	b := NewBambuAdapter("test", "127.0.0.1", "00M00A000000000", "12345678")

	// Prime to a non-empty starting state so the FINISH transition fires.
	b.handleReport(reportPayload("RUNNING"))
	if got := b.state.State; got != "printing" {
		t.Fatalf("expected state 'printing' after RUNNING report, got %q", got)
	}
	if !b.state.LastFinishedAt.IsZero() {
		t.Fatalf("expected zero LastFinishedAt before any FINISH, got %v", b.state.LastFinishedAt)
	}

	before := time.Now()
	b.handleReport(reportPayload("FINISH"))
	after := time.Now()

	if got := b.state.State; got != "finished" {
		t.Fatalf("expected state 'finished' after FINISH report, got %q", got)
	}
	if b.state.LastFinishedAt.IsZero() {
		t.Fatal("expected LastFinishedAt to be set after FINISH transition")
	}
	if b.state.LastFinishedAt.Before(before) || b.state.LastFinishedAt.After(after) {
		t.Errorf("LastFinishedAt %v not within [%v, %v]", b.state.LastFinishedAt, before, after)
	}
}

func TestBambuLastFinishedAtNotOverwrittenWhileFinished(t *testing.T) {
	b := NewBambuAdapter("test", "127.0.0.1", "00M00A000000000", "12345678")

	b.handleReport(reportPayload("RUNNING"))
	b.handleReport(reportPayload("FINISH"))
	first := b.state.LastFinishedAt
	if first.IsZero() {
		t.Fatal("expected non-zero LastFinishedAt after FINISH")
	}

	// Another FINISH report while already finished should NOT bump the time —
	// only transitions *into* finished count.
	time.Sleep(10 * time.Millisecond)
	b.handleReport(reportPayload("FINISH"))
	if !b.state.LastFinishedAt.Equal(first) {
		t.Errorf("LastFinishedAt should not change on repeat FINISH; got %v want %v", b.state.LastFinishedAt, first)
	}
}

func TestBambuLastFinishedAtUpdatedOnSecondFinishAfterRunning(t *testing.T) {
	b := NewBambuAdapter("test", "127.0.0.1", "00M00A000000000", "12345678")

	b.handleReport(reportPayload("RUNNING"))
	b.handleReport(reportPayload("FINISH"))
	first := b.state.LastFinishedAt

	// Simulate a new print: RUNNING again, then FINISH. The second FINISH
	// should overwrite LastFinishedAt with a newer time.
	time.Sleep(10 * time.Millisecond)
	b.handleReport(reportPayload("RUNNING"))
	if b.state.LastFinishedAt != first {
		t.Errorf("LastFinishedAt should be unchanged on RUNNING transition, got %v want %v", b.state.LastFinishedAt, first)
	}
	time.Sleep(10 * time.Millisecond)
	b.handleReport(reportPayload("FINISH"))
	if !b.state.LastFinishedAt.After(first) {
		t.Errorf("LastFinishedAt should advance on second FINISH; got %v not after %v", b.state.LastFinishedAt, first)
	}
}
