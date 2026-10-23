package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event names published on GET /api/fil/events.
const (
	EventPrinters  = "printers"  // data: the same []PrinterState as GET /printers, sorted by name
	EventInventory = "inventory" // data: InventoryChange
	EventPlans     = "plans"     // data: {}; a plan file was added, changed, moved or removed
)

// eventBuffer is how many undelivered events a subscriber may queue. A
// subscriber that falls further behind is disconnected rather than allowed to
// block publishers; EventSource clients reconnect and refetch.
const eventBuffer = 32

// eventHeartbeat is how often an idle stream gets an SSE comment, so proxies
// and NAT don't time out the connection.
const eventHeartbeat = 25 * time.Second

// Event is one Server-Sent Event.
type Event struct {
	Name string
	Data []byte // single-line JSON
}

// InventoryChange is the payload of an "inventory" event. It only says that
// something changed; clients refetch what they display.
type InventoryChange struct {
	// Resources lists the Spoolman resource kinds that changed ("spool",
	// "setting", "filament", "vendor").
	Resources []string `json:"resources,omitempty"`
	// Resync is set after fil serve reconnects to Spoolman: changes made
	// while it was disconnected were not seen, so clients should refetch
	// everything.
	Resync bool `json:"resync,omitempty"`
}

// EventHub fans change notifications out to every connected event stream.
type EventHub struct {
	mu     sync.Mutex
	subs   map[chan Event]struct{}
	closed bool
}

// NewEventHub creates an empty hub.
func NewEventHub() *EventHub {
	return &EventHub{subs: make(map[chan Event]struct{})}
}

// Subscribe registers a new subscriber. The returned channel is closed when
// the hub closes or the subscriber falls too far behind; cancel unsubscribes.
func (h *EventHub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, eventBuffer)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(ch)
		return ch, func() {}
	}
	h.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Publish delivers an event to every subscriber without blocking.
func (h *EventHub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// PublishJSON marshals v and publishes it under name.
func (h *EventHub) PublishJSON(name string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Printf("[events] marshal %s: %v\n", name, err)
		return
	}
	h.Publish(Event{Name: name, Data: data})
}

// Close disconnects every subscriber; later subscribers get a closed channel.
// Register it with http.Server.RegisterOnShutdown so open streams don't hold
// up a graceful shutdown.
func (h *EventHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}

// printerSnapshot returns the printers sorted by name as JSON, plus a key for
// change detection that ignores LastUpdated (which moves on every report even
// when nothing visible changed).
func printerSnapshot(pm *PrinterManager) (key, data []byte) {
	states := pm.AllStatus()
	if states == nil {
		states = []PrinterState{}
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Name < states[j].Name })
	data, _ = json.Marshal(states)
	stable := make([]PrinterState, len(states))
	copy(stable, states)
	for i := range stable {
		stable[i].LastUpdated = time.Time{}
	}
	key, _ = json.Marshal(stable)
	return key, data
}

// WatchPrinters publishes a "printers" event whenever the printers' state
// differs from the last check. Adapter state is in memory (Bambu is pushed
// over MQTT, Prusa is polled by its adapter), so checking is cheap.
func (h *EventHub) WatchPrinters(ctx context.Context, pm *PrinterManager, interval time.Duration) {
	last, _ := printerSnapshot(pm)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			key, data := printerSnapshot(pm)
			if bytes.Equal(key, last) {
				continue
			}
			last = key
			h.Publish(Event{Name: EventPrinters, Data: data})
		}
	}
}

// plansSnapshot is a key for change detection over the plan directories: each
// file's directory, name, size and modification time. A missing directory
// (no pause or archive dir configured) just contributes nothing.
func plansSnapshot(dirs []string) string {
	var b strings.Builder
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || info.IsDir() {
				continue
			}
			fmt.Fprintf(&b, "%s/%s %d %d\n", dir, e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// WatchPlans publishes a "plans" event whenever a file in the plan
// directories is added, changed, moved or removed. It watches the files
// rather than fil serve's own handlers so plans saved by a CLI working on the
// directories directly are seen too.
func (h *EventHub) WatchPlans(ctx context.Context, dirs []string, interval time.Duration) {
	last := plansSnapshot(dirs)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			key := plansSnapshot(dirs)
			if key == last {
				continue
			}
			last = key
			h.Publish(Event{Name: EventPlans, Data: []byte(`{}`)})
		}
	}
}

func writeEvent(w http.ResponseWriter, ev Event) error {
	_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, ev.Data)
	return err
}

// handleEvents streams change notifications as Server-Sent Events. A new
// stream starts with a "printers" snapshot; clients should (re)fetch inventory
// whenever they (re)connect, since nothing is replayed.
func (s *PlanServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.Events == nil {
		http.Error(w, "event stream not enabled", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch, cancel := s.Events.Subscribe()
	defer cancel()

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("X-Accel-Buffering", "no") // stop nginx-style proxies buffering the stream
	w.WriteHeader(http.StatusOK)

	_, _ = fmt.Fprint(w, "retry: 3000\n\n")
	if s.Printers != nil {
		_, data := printerSnapshot(s.Printers)
		_ = writeEvent(w, Event{Name: EventPrinters, Data: data})
	}
	flusher.Flush()

	heartbeat := time.NewTicker(eventHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if writeEvent(w, ev) != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
