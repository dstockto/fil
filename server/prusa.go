package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/icholy/digest"
)

// PrusaAdapter communicates with a Prusa printer via PrusaLink REST API.
type PrusaAdapter struct {
	name     string
	ip       string
	username string
	password string

	mu             sync.RWMutex
	state          PrinterState
	stateCallbacks []func(StateChangeEvent)
	stopCh         chan struct{}
}

// NewPrusaAdapter creates a new Prusa printer adapter.
func NewPrusaAdapter(name, ip, username, password string) *PrusaAdapter {
	return &PrusaAdapter{
		name:     name,
		ip:       ip,
		username: username,
		password: password,
		state: PrinterState{
			Name:       name,
			Type:       "prusa",
			State:      "offline",
			ActiveTray: -1,
		},
		stopCh: make(chan struct{}),
	}
}

// Connect starts polling the Prusa printer for status updates.
func (p *PrusaAdapter) Connect() error {
	// Do an initial poll to verify connectivity
	if err := p.poll(); err != nil {
		return fmt.Errorf("prusa %s: initial poll failed: %w", p.name, err)
	}

	// Start background polling
	go p.pollLoop()
	return nil
}

// Close stops the polling loop.
func (p *PrusaAdapter) Close() error {
	close(p.stopCh)
	return nil
}

// Status returns the current printer state.
func (p *PrusaAdapter) Status() PrinterState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// PushTray is not supported on Prusa printers.
func (p *PrusaAdapter) PushTray(update TrayUpdate) error {
	return fmt.Errorf("prusa %s: tray updates not supported", p.name)
}

// OnStateChange registers a callback for printer state transitions.
func (p *PrusaAdapter) OnStateChange(cb func(event StateChangeEvent)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stateCallbacks = append(p.stateCallbacks, cb)
}

func (p *PrusaAdapter) pollLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			_ = p.poll()
		}
	}
}

func (p *PrusaAdapter) poll() error {
	status, err := p.fetchStatus()
	if err != nil {
		p.mu.Lock()
		p.state.State = "offline"
		p.mu.Unlock()
		return err
	}

	p.mu.Lock()
	oldState := p.state.State
	p.state.LastUpdated = time.Now()

	if printerData, ok := status["printer"].(map[string]interface{}); ok {
		if state, ok := printerData["state"].(string); ok {
			p.state.State = normalizePrusaState(state)
		}
	}
	p.mu.Unlock()

	// Fetch job info if printing
	if p.state.State == "printing" || p.state.State == "paused" {
		if job, err := p.fetchJob(); err == nil {
			p.mu.Lock()
			if progress, ok := job["progress"].(float64); ok {
				p.state.Progress = int(progress)
			}
			if remaining, ok := job["time_remaining"].(float64); ok {
				p.state.RemainingMins = int(remaining) / 60
			}
			if file, ok := job["file"].(map[string]interface{}); ok {
				if name, ok := file["display_name"].(string); ok {
					p.state.CurrentFile = name
				} else if name, ok := file["name"].(string); ok {
					p.state.CurrentFile = name
				}
			}
			p.mu.Unlock()
		}
	}

	// Fire state change callbacks
	p.mu.RLock()
	newState := p.state.State
	callbacks := p.stateCallbacks
	p.mu.RUnlock()

	if newState != oldState && oldState != "" {
		event := StateChangeEvent{
			OldState: oldState,
			NewState: newState,
		}
		for _, cb := range callbacks {
			go cb(event)
		}
	}

	return nil
}

func (p *PrusaAdapter) fetchStatus() (map[string]interface{}, error) {
	return p.fetch("/api/v1/status")
}

func (p *PrusaAdapter) fetchJob() (map[string]interface{}, error) {
	return p.fetch("/api/v1/job")
}

func (p *PrusaAdapter) fetch(path string) (map[string]interface{}, error) {
	url := fmt.Sprintf("http://%s%s", p.ip, path)

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &digest.Transport{
			Username: p.username,
			Password: p.password,
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func normalizePrusaState(state string) string {
	switch state {
	case "IDLE":
		return "idle"
	case "PRINTING":
		return "printing"
	case "PAUSED":
		return "paused"
	case "FINISHED":
		return "finished"
	case "ATTENTION":
		return "paused"
	case "ERROR":
		return "failed"
	default:
		return "idle"
	}
}
