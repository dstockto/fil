package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedAdapter is a PrinterAdapter whose state can be changed while another
// goroutine reads it.
type lockedAdapter struct {
	mu    sync.Mutex
	state PrinterState
}

func (a *lockedAdapter) Connect() error                       { return nil }
func (a *lockedAdapter) Close() error                         { return nil }
func (a *lockedAdapter) PushTray(TrayUpdate) error            { return nil }
func (a *lockedAdapter) OnStateChange(func(StateChangeEvent)) {}
func (a *lockedAdapter) Status() PrinterState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}
func (a *lockedAdapter) set(f func(*PrinterState)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f(&a.state)
}

func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed, wanted an event")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	return Event{}
}

func expectNoEvent(t *testing.T, ch <-chan Event, wait time.Duration) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event %s: %s", ev.Name, ev.Data)
	case <-time.After(wait):
	}
}

func TestEventHubFanOut(t *testing.T) {
	h := NewEventHub()
	a, cancelA := h.Subscribe()
	defer cancelA()
	b, cancelB := h.Subscribe()
	defer cancelB()

	h.Publish(Event{Name: EventInventory, Data: []byte(`{}`)})

	for _, ch := range []<-chan Event{a, b} {
		if ev := recvEvent(t, ch); ev.Name != EventInventory {
			t.Errorf("got event %q, want %q", ev.Name, EventInventory)
		}
	}
}

func TestEventHubCancelStopsDelivery(t *testing.T) {
	h := NewEventHub()
	ch, cancel := h.Subscribe()
	cancel()
	cancel() // second cancel must not panic on the closed channel

	h.Publish(Event{Name: EventInventory})
	if _, ok := <-ch; ok {
		t.Error("canceled subscriber received an event")
	}
}

func TestEventHubDropsSlowSubscriber(t *testing.T) {
	h := NewEventHub()
	ch, cancel := h.Subscribe()
	defer cancel()

	// One more than the buffer: the publisher must not block, and the
	// subscriber is disconnected so its client reconnects and refetches.
	for i := 0; i < eventBuffer+1; i++ {
		h.Publish(Event{Name: EventInventory})
	}

	n := 0
	for range ch {
		n++
	}
	if n != eventBuffer {
		t.Errorf("drained %d buffered events, want %d before the channel closed", n, eventBuffer)
	}
}

func TestEventHubClose(t *testing.T) {
	h := NewEventHub()
	ch, cancel := h.Subscribe()
	defer cancel()

	h.Close()
	h.Close() // idempotent

	if _, ok := <-ch; ok {
		t.Error("subscriber channel still open after Close")
	}
	late, _ := h.Subscribe()
	if _, ok := <-late; ok {
		t.Error("Subscribe after Close returned an open channel")
	}
}

func TestWatchPrintersPublishesOnlyRealChanges(t *testing.T) {
	adapter := &lockedAdapter{state: PrinterState{Name: "X1C", State: "idle", LastUpdated: time.Now()}}
	pm := NewPrinterManager()
	if err := pm.AddAdapter("X1C", adapter); err != nil {
		t.Fatal(err)
	}
	h := NewEventHub()
	ch, cancel := h.Subscribe()
	defer cancel()

	ctx := t.Context()
	go h.WatchPrinters(ctx, pm, 10*time.Millisecond)

	// A new report with nothing visible changed only moves LastUpdated.
	adapter.set(func(s *PrinterState) { s.LastUpdated = time.Now().Add(time.Minute) })
	expectNoEvent(t, ch, 100*time.Millisecond)

	adapter.set(func(s *PrinterState) { s.State = "printing"; s.Progress = 3 })
	ev := recvEvent(t, ch)
	if ev.Name != EventPrinters {
		t.Fatalf("got event %q, want %q", ev.Name, EventPrinters)
	}
	var states []PrinterState
	if err := json.Unmarshal(ev.Data, &states); err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].State != "printing" || states[0].Progress != 3 {
		t.Errorf("printers payload = %+v", states)
	}
	expectNoEvent(t, ch, 100*time.Millisecond)
}

func TestPrinterSnapshotSortedByName(t *testing.T) {
	pm := NewPrinterManager()
	for _, name := range []string{"Prusa XL", "Bambu X1C", "Mini"} {
		if err := pm.AddAdapter(name, &lockedAdapter{state: PrinterState{Name: name}}); err != nil {
			t.Fatal(err)
		}
	}
	_, data := printerSnapshot(pm)
	var states []PrinterState
	if err := json.Unmarshal(data, &states); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range states {
		got = append(got, s.Name)
	}
	if strings.Join(got, ",") != "Bambu X1C,Mini,Prusa XL" {
		t.Errorf("order = %v", got)
	}
}

// sseReader reads Server-Sent Events from a stream, skipping comments.
type sseReader struct{ sc *bufio.Scanner }

func (r sseReader) next(t *testing.T) (name, data string) {
	t.Helper()
	for r.sc.Scan() {
		line := r.sc.Text()
		switch {
		case line == "":
			if name != "" || data != "" {
				return name, data
			}
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("stream ended before an event: %v", r.sc.Err())
	return "", ""
}

func startEventServer(t *testing.T, s *PlanServer) (*http.Response, sseReader) {
	t.Helper()
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/api/fil/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp, sseReader{bufio.NewScanner(resp.Body)}
}

func TestHandleEventsStreamsSnapshotThenChanges(t *testing.T) {
	pm := NewPrinterManager()
	if err := pm.AddAdapter("X1C", &lockedAdapter{state: PrinterState{Name: "X1C", State: "idle"}}); err != nil {
		t.Fatal(err)
	}
	s := &PlanServer{Printers: pm, Events: NewEventHub()}
	resp, sse := startEventServer(t, s)

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if name, data := sse.next(t); name != EventPrinters || !strings.Contains(data, `"name":"X1C"`) {
		t.Fatalf("first event = %q %s, want a printers snapshot", name, data)
	}

	s.Events.PublishJSON(EventInventory, InventoryChange{Resources: []string{"setting", "spool"}})
	name, data := sse.next(t)
	if name != EventInventory || data != `{"resources":["setting","spool"]}` {
		t.Errorf("got %q %s", name, data)
	}
}

func TestHandleEventsEndsWhenHubCloses(t *testing.T) {
	s := &PlanServer{Events: NewEventHub()}
	_, sse := startEventServer(t, s)

	// No printers configured: no snapshot, so publish one event to know the
	// stream is subscribed before closing the hub.
	s.Events.PublishJSON(EventInventory, InventoryChange{Resync: true})
	if name, _ := sse.next(t); name != EventInventory {
		t.Fatalf("got %q", name)
	}
	s.Events.Close()

	done := make(chan bool)
	go func() {
		for sse.sc.Scan() {
		}
		done <- true
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream still open after the hub closed")
	}
}

func TestHandleEventsDisabled(t *testing.T) {
	ts := httptest.NewServer((&PlanServer{}).Routes())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/fil/events")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}
