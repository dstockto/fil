package server

import (
	"testing"
	"time"
)

// reportPayload returns a minimal MQTT report JSON with the given gcode_state.
func reportPayload(gcodeState string) []byte {
	return []byte(`{"print":{"gcode_state":"` + gcodeState + `"}}`)
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
