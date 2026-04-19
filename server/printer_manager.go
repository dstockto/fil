package server

import (
	"fmt"
	"sync"
	"time"
)

// PrinterManager manages connections to all configured printers.
type PrinterManager struct {
	mu       sync.RWMutex
	adapters map[string]PrinterAdapter // keyed by printer name
}

// NewPrinterManager creates a new printer manager.
func NewPrinterManager() *PrinterManager {
	return &PrinterManager{
		adapters: make(map[string]PrinterAdapter),
	}
}

// AddAdapter registers a printer adapter and connects to the printer.
func (pm *PrinterManager) AddAdapter(name string, adapter PrinterAdapter) error {
	if err := adapter.Connect(); err != nil {
		return err
	}
	pm.mu.Lock()
	pm.adapters[name] = adapter
	pm.mu.Unlock()
	return nil
}

// Close disconnects all printers.
func (pm *PrinterManager) Close() {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, adapter := range pm.adapters {
		_ = adapter.Close()
	}
}

// AllStatus returns the current state of all printers.
func (pm *PrinterManager) AllStatus() []PrinterState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var states []PrinterState
	for _, adapter := range pm.adapters {
		states = append(states, adapter.Status())
	}
	return states
}

// Status returns the state of a specific printer.
func (pm *PrinterManager) Status(name string) (PrinterState, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	adapter, ok := pm.adapters[name]
	if !ok {
		return PrinterState{}, fmt.Errorf("printer %q not found", name)
	}
	return adapter.Status(), nil
}

// PushTray pushes filament metadata to a specific printer's tray.
func (pm *PrinterManager) PushTray(printerName string, update TrayUpdate) error {
	pm.mu.RLock()
	adapter, ok := pm.adapters[printerName]
	pm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("printer %q not found", printerName)
	}
	return adapter.PushTray(update)
}

// Adapter returns the adapter for a specific printer.
func (pm *PrinterManager) Adapter(name string) (PrinterAdapter, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	a, ok := pm.adapters[name]
	return a, ok
}

// LastFinishedAt returns the most recent time the named printer transitioned
// into the "finished" state. The boolean is false if the printer is unknown
// or has no recorded finish time yet.
func (pm *PrinterManager) LastFinishedAt(name string) (time.Time, bool) {
	pm.mu.RLock()
	adapter, ok := pm.adapters[name]
	pm.mu.RUnlock()
	if !ok {
		return time.Time{}, false
	}
	t := adapter.Status().LastFinishedAt
	if t.IsZero() {
		return time.Time{}, false
	}
	return t, true
}
