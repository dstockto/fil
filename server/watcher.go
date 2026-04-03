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

// ETAWatcher monitors in-progress plates and sends notifications when ETAs pass.
type ETAWatcher struct {
	plansDir string
	notifier *Notifier

	mu        sync.Mutex
	notified  map[plateKey]int // tracks how many notifications sent (0=none, 1=ETA, 2=reminder)
	timer     *time.Timer
	cancel    context.CancelFunc
	ctx       context.Context
}

const reminderDelay = 5 * time.Minute

// NewETAWatcher creates and starts an ETA watcher.
func NewETAWatcher(ctx context.Context, plansDir string, notifier *Notifier) *ETAWatcher {
	ctx, cancel := context.WithCancel(ctx)
	w := &ETAWatcher{
		plansDir: plansDir,
		notifier: notifier,
		notified: make(map[plateKey]int),
		cancel:   cancel,
		ctx:      ctx,
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

	// Clean up notified entries for plates no longer in-progress
	activeKeys := make(map[plateKey]bool)
	for _, p := range plates {
		activeKeys[p.key] = true
	}
	for k := range w.notified {
		if !activeKeys[k] {
			delete(w.notified, k)
		}
	}

	// Find the next event time
	now := time.Now()
	var nextEvent time.Time

	for _, p := range plates {
		count := w.notified[p.key]

		var eventTime time.Time
		switch count {
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
		count := w.notified[p.key]

		switch count {
		case 0:
			if now.After(p.eta) || now.Equal(p.eta) {
				title := "Print should be done"
				msg := fmt.Sprintf("%s: %s / %s should be done", p.printer, p.key.project, p.key.plate)
				if p.printer == "" {
					msg = fmt.Sprintf("%s / %s should be done", p.key.project, p.key.plate)
				}
				w.notifier.Send(title, msg)
				w.notified[p.key] = 1
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
				w.notified[p.key] = 2
			}
		}
	}

	// Schedule next event
	w.scheduleNextLocked()
}
