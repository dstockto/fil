package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebsocketURL(t *testing.T) {
	tests := []struct{ base, want string }{
		{"https://spoolman.example.com", "wss://spoolman.example.com/api/v1/"},
		{"https://spoolman.example.com/", "wss://spoolman.example.com/api/v1/"},
		{"http://host-a:7912", "ws://host-a:7912/api/v1/"},
		{"http://host-a/spoolman", "ws://host-a/spoolman/api/v1/"},
	}
	for _, tt := range tests {
		got, err := websocketURL(tt.base)
		if err != nil || got != tt.want {
			t.Errorf("websocketURL(%q) = %q, %v; want %q", tt.base, got, err, tt.want)
		}
	}
	if _, err := websocketURL("ftp://host-a"); err == nil {
		t.Error("expected an error for a non-HTTP scheme")
	}
}

// fakeSpoolman serves Spoolman's root websocket. Each accepted connection is
// handed to the test over conns.
func fakeSpoolman(t *testing.T) (base string, conns <-chan *websocket.Conn) {
	t.Helper()
	ch := make(chan *websocket.Conn, 4)
	up := websocket.Upgrader{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/" {
			http.NotFound(w, r)
			return
		}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ch <- c
	}))
	t.Cleanup(ts.Close)
	return ts.URL, ch
}

func recvConn(t *testing.T, conns <-chan *websocket.Conn) *websocket.Conn {
	t.Helper()
	select {
	case c := <-conns:
		t.Cleanup(func() { _ = c.Close() })
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("watcher never connected")
	}
	return nil
}

func sendSpoolman(t *testing.T, c *websocket.Conn, typ, resource string) {
	t.Helper()
	msg := map[string]any{"type": typ, "resource": resource, "date": time.Now().UTC().Format(time.RFC3339), "payload": map[string]any{"id": 194}}
	if err := c.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}
}

func decodeInventory(t *testing.T, ev Event) InventoryChange {
	t.Helper()
	if ev.Name != EventInventory {
		t.Fatalf("got event %q, want %q", ev.Name, EventInventory)
	}
	var ic InventoryChange
	if err := json.Unmarshal(ev.Data, &ic); err != nil {
		t.Fatal(err)
	}
	return ic
}

// A `fil move` makes Spoolman emit a setting update (locations_spoolorders)
// then a spool update; clients should get one inventory event for the pair.
func TestSpoolmanWatcherDebouncesAMove(t *testing.T) {
	base, conns := fakeSpoolman(t)
	hub := NewEventHub()
	events, cancel := hub.Subscribe()
	defer cancel()

	w := &SpoolmanWatcher{BaseURL: base, Hub: hub, Debounce: 50 * time.Millisecond}
	go w.Run(t.Context())
	c := recvConn(t, conns)

	sendSpoolman(t, c, "updated", "setting")
	sendSpoolman(t, c, "updated", "spool")

	ic := decodeInventory(t, recvEvent(t, events))
	if len(ic.Resources) != 2 || ic.Resources[0] != "setting" || ic.Resources[1] != "spool" || ic.Resync {
		t.Errorf("inventory change = %+v, want resources [setting spool]", ic)
	}
	expectNoEvent(t, events, 150*time.Millisecond)
}

func TestSpoolmanWatcherIgnoresNonEventFrames(t *testing.T) {
	base, conns := fakeSpoolman(t)
	hub := NewEventHub()
	events, cancel := hub.Subscribe()
	defer cancel()

	w := &SpoolmanWatcher{BaseURL: base, Hub: hub, Debounce: 20 * time.Millisecond}
	go w.Run(t.Context())
	c := recvConn(t, conns)

	_ = c.WriteMessage(websocket.TextMessage, []byte("not json"))
	_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"updated"}`))
	expectNoEvent(t, events, 100*time.Millisecond)
}

// After a dropped connection the watcher can't know what it missed, so the
// reconnect tells clients to refetch everything.
func TestSpoolmanWatcherReconnectsAndAsksForResync(t *testing.T) {
	base, conns := fakeSpoolman(t)
	hub := NewEventHub()
	events, cancel := hub.Subscribe()
	defer cancel()

	w := &SpoolmanWatcher{BaseURL: base, Hub: hub, Debounce: 20 * time.Millisecond, MinBackoff: 10 * time.Millisecond}
	go w.Run(t.Context())

	first := recvConn(t, conns)
	expectNoEvent(t, events, 50*time.Millisecond) // the first connection is not a resync
	_ = first.Close()

	second := recvConn(t, conns)
	if ic := decodeInventory(t, recvEvent(t, events)); !ic.Resync {
		t.Errorf("after reconnect got %+v, want resync", ic)
	}

	sendSpoolman(t, second, "deleted", "spool")
	if ic := decodeInventory(t, recvEvent(t, events)); len(ic.Resources) != 1 || ic.Resources[0] != "spool" {
		t.Errorf("after reconnect, change = %+v", ic)
	}
}
