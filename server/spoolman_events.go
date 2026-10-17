package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// SpoolmanWatcher listens on Spoolman's root websocket (/api/v1/), which
// reports every database change, and publishes a debounced "inventory" event.
// This catches changes from any client — the fil CLI, the Spoolman web UI, or
// fil serve itself. One `fil move` produces a setting (locations_spoolorders)
// update and a spool update in quick succession; the debounce turns that into
// one event.
type SpoolmanWatcher struct {
	BaseURL       string // Spoolman base URL, without /api/v1
	TLSSkipVerify bool
	Hub           *EventHub

	Debounce   time.Duration // quiet period before publishing; default 500ms
	MinBackoff time.Duration // first reconnect delay; default 1s
	MaxBackoff time.Duration // reconnect delay cap; default 30s
	PingEvery  time.Duration // keepalive ping interval; default 30s
}

// spoolmanMessage is the envelope of a Spoolman websocket message.
type spoolmanMessage struct {
	Type     string `json:"type"`     // "added", "updated", "deleted"
	Resource string `json:"resource"` // "spool", "filament", "vendor", "setting"
}

func (w *SpoolmanWatcher) defaults() {
	if w.Debounce == 0 {
		w.Debounce = 500 * time.Millisecond
	}
	if w.MinBackoff == 0 {
		w.MinBackoff = time.Second
	}
	if w.MaxBackoff == 0 {
		w.MaxBackoff = 30 * time.Second
	}
	if w.PingEvery == 0 {
		w.PingEvery = 30 * time.Second
	}
}

// websocketURL turns the Spoolman base URL into its root websocket URL.
func websocketURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported Spoolman URL scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/"
	return u.String(), nil
}

// Run connects and reconnects until ctx is cancelled.
func (w *SpoolmanWatcher) Run(ctx context.Context) {
	w.defaults()
	wsURL, err := websocketURL(w.BaseURL)
	if err != nil {
		fmt.Printf("[events] spoolman watcher disabled: %v\n", err)
		return
	}
	dialer := *websocket.DefaultDialer
	if w.TLSSkipVerify {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-configured for local CA trust issues
	}

	backoff := w.MinBackoff
	connectedBefore := false
	for ctx.Err() == nil {
		conn, _, err := dialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			fmt.Printf("[events] spoolman websocket: %v (retrying in %s)\n", err, backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, w.MaxBackoff)
			continue
		}
		backoff = w.MinBackoff
		if connectedBefore {
			// Anything that changed while we were disconnected was missed.
			w.Hub.PublishJSON(EventInventory, InventoryChange{Resync: true})
		}
		connectedBefore = true
		w.listen(ctx, conn)
		_ = conn.Close()
	}
}

// listen reads messages until the connection fails or ctx is cancelled,
// publishing one inventory event per burst of changes.
func (w *SpoolmanWatcher) listen(ctx context.Context, conn *websocket.Conn) {
	deadline := func() time.Time { return time.Now().Add(w.PingEvery * 5 / 2) }
	_ = conn.SetReadDeadline(deadline())
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(deadline()) })

	msgs := make(chan spoolmanMessage)
	readErr := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			_ = conn.SetReadDeadline(deadline())
			var m spoolmanMessage
			if json.Unmarshal(data, &m) != nil || m.Resource == "" {
				continue
			}
			select {
			case msgs <- m:
			case <-done:
				return
			}
		}
	}()

	ping := time.NewTicker(w.PingEvery)
	defer ping.Stop()
	var debounce <-chan time.Time
	pending := map[string]bool{}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		res := make([]string, 0, len(pending))
		for r := range pending {
			res = append(res, r)
		}
		sort.Strings(res)
		w.Hub.PublishJSON(EventInventory, InventoryChange{Resources: res})
		pending = map[string]bool{}
		debounce = nil
	}

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			return
		case m := <-msgs:
			pending[m.Resource] = true
			if debounce == nil {
				debounce = time.After(w.Debounce)
			}
		case <-debounce:
			flush()
		case <-ping.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				flush()
				return
			}
		case err := <-readErr:
			flush()
			if ctx.Err() == nil {
				fmt.Printf("[events] spoolman websocket closed: %v\n", err)
			}
			return
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
