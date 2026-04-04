package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

// plateKey uniquely identifies a plate across plans.
type plateKey struct {
	plan    string
	project string
	plate   string
}

// notifyState tracks notification progress for a plate with a specific ETA.
type notifyState struct {
	eta   time.Time // the ETA we notified about; if it changes, we reset
	count int       // 0=none, 1=ETA sent, 2=reminder sent
}

// ETAWatcher monitors in-progress plates and sends notifications when ETAs pass.
type ETAWatcher struct {
	plansDir     string
	notifier     *Notifier
	livePrinters map[string]bool // printers with live connections (skip ETA-based notifications)

	mu        sync.Mutex
	notified  map[plateKey]notifyState
	timer     *time.Timer
	cancel    context.CancelFunc
	ctx       context.Context
}

const reminderDelay = 5 * time.Minute

// NewETAWatcher creates and starts an ETA watcher.
// livePrinters is a set of printer names that have active live connections;
// plates on these printers are skipped (notifications come from state changes instead).
func NewETAWatcher(ctx context.Context, plansDir string, notifier *Notifier, livePrinters map[string]bool) *ETAWatcher {
	ctx, cancel := context.WithCancel(ctx)
	w := &ETAWatcher{
		plansDir:     plansDir,
		notifier:     notifier,
		livePrinters: livePrinters,
		notified:     make(map[plateKey]notifyState),
		cancel:       cancel,
		ctx:          ctx,
	}
	w.Reschedule()
	return w
}

// Reschedule re-scans plans and schedules the next notification check.
// Call this whenever a plan is saved.
func (w *ETAWatcher) Reschedule() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timer != nil {
		w.timer.Stop()
	}

	w.scheduleNextLocked()
}

// Stop cancels the watcher.
func (w *ETAWatcher) Stop() {
	w.cancel()
	w.mu.Lock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
}

// scheduleNextLocked scans plans and sets a timer for the next ETA event.
// Must be called with w.mu held.
func (w *ETAWatcher) scheduleNextLocked() {
	plates := w.scanPlates()

	// Clean up notified entries for plates no longer in-progress,
	// and reset notifications if ETA changed (e.g. fil plan time was re-run).
	activeKeys := make(map[plateKey]time.Time)
	for _, p := range plates {
		activeKeys[p.key] = p.eta
	}
	for k, state := range w.notified {
		eta, active := activeKeys[k]
		if !active {
			delete(w.notified, k)
		} else if !eta.Equal(state.eta) {
			// ETA changed — reset so notifications fire for new ETA
			delete(w.notified, k)
		}
	}

	// Find the next event time
	now := time.Now()
	var nextEvent time.Time

	for _, p := range plates {
		state := w.notified[p.key]

		var eventTime time.Time
		switch state.count {
		case 0:
			eventTime = p.eta
		case 1:
			eventTime = p.eta.Add(reminderDelay)
		default:
			continue // already fully notified
		}

		if eventTime.Before(now) {
			// Overdue — fire immediately
			nextEvent = now
			break
		}

		if nextEvent.IsZero() || eventTime.Before(nextEvent) {
			nextEvent = eventTime
		}
	}

	if nextEvent.IsZero() {
		return // nothing to schedule
	}

	// If the next event falls during quiet hours, defer to when quiet ends
	if w.notifier.IsQuietHours(nextEvent) {
		nextEvent = w.notifier.QuietEndTime(nextEvent)
	}

	delay := time.Until(nextEvent)
	if delay < 0 {
		delay = 0
	}
	w.timer = time.AfterFunc(delay, func() {
		w.fireNotifications()
	})
}

type plateETA struct {
	key     plateKey
	printer string
	eta     time.Time
}

func (w *ETAWatcher) scanPlates() []plateETA {
	entries, err := os.ReadDir(w.plansDir)
	if err != nil {
		return nil
	}

	var results []plateETA
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(w.plansDir, e.Name()))
		if err != nil {
			continue
		}
		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			continue
		}
		plan.DefaultStatus()

		for _, proj := range plan.Projects {
			for _, plate := range proj.Plates {
				if plate.Status != "in-progress" || plate.StartedAt == "" || plate.EstimatedDuration == "" {
					continue
				}

				// Skip plates on printers with live connections —
				// those get notifications from printer state changes instead
				if plate.Printer != "" && w.livePrinters[plate.Printer] {
					continue
				}

				started, err := time.Parse(time.RFC3339, plate.StartedAt)
				if err != nil {
					continue
				}
				dur, err := time.ParseDuration(plate.EstimatedDuration)
				if err != nil {
					continue
				}

				results = append(results, plateETA{
					key: plateKey{
						plan:    e.Name(),
						project: proj.Name,
						plate:   plate.Name,
					},
					printer: plate.Printer,
					eta:     started.Add(dur),
				})
			}
		}
	}

	return results
}

func (w *ETAWatcher) fireNotifications() {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check context
	select {
	case <-w.ctx.Done():
		return
	default:
	}

	plates := w.scanPlates()
	now := time.Now()

	for _, p := range plates {
		state := w.notified[p.key]

		// If ETA changed since we last notified, reset
		if state.count > 0 && !p.eta.Equal(state.eta) {
			state = notifyState{}
		}

		switch state.count {
		case 0:
			if now.After(p.eta) || now.Equal(p.eta) {
				title := "Print should be done"
				msg := fmt.Sprintf("%s: %s / %s should be done", p.printer, p.key.project, p.key.plate)
				if p.printer == "" {
					msg = fmt.Sprintf("%s / %s should be done", p.key.project, p.key.plate)
				}
				w.notifier.Send(title, msg)
				w.notified[p.key] = notifyState{eta: p.eta, count: 1}
			}
		case 1:
			reminderTime := p.eta.Add(reminderDelay)
			if now.After(reminderTime) || now.Equal(reminderTime) {
				title := "Print still not marked complete"
				msg := fmt.Sprintf("%s: %s / %s still not marked complete", p.printer, p.key.project, p.key.plate)
				if p.printer == "" {
					msg = fmt.Sprintf("%s / %s still not marked complete", p.key.project, p.key.plate)
				}
				w.notifier.Send(title, msg)
				w.notified[p.key] = notifyState{eta: p.eta, count: 2}
			}
		}
	}

	// Schedule next event
	w.scheduleNextLocked()
}

// LookupInProgressPlate finds the in-progress plate assigned to a given printer
// by scanning plan files. Returns project name and plate name, or empty strings if not found.
func LookupInProgressPlate(plansDir, printerName string) (projectName, plateName string) {
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		return "", ""
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(plansDir, e.Name()))
		if err != nil {
			continue
		}
		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			continue
		}
		plan.DefaultStatus()

		for _, proj := range plan.Projects {
			for _, plate := range proj.Plates {
				if plate.Status == "in-progress" && plate.Printer == printerName {
					return proj.Name, plate.Name
				}
			}
		}
	}

	return "", ""
}
