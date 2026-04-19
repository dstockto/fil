package server

import (
	"testing"
	"time"
)

func prusaStatus(state string) map[string]interface{} {
	return map[string]interface{}{
		"printer": map[string]interface{}{"state": state},
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
