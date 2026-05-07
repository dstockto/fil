package server

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// bambuPushTrayMinGap is the minimum interval between consecutive PushTray
// MQTT publishes per Bambu printer. Pushes are QoS 0 fire-and-forget, so
// without pacing the printer firmware drops some when many trays are pushed
// back-to-back (e.g. a multi-move or full `fil sync`). Empirically the
// printer keeps up at ~2/sec — 500ms gives a safety margin.
const bambuPushTrayMinGap = 500 * time.Millisecond

// BambuAdapter communicates with a Bambu Lab printer via MQTT.
type BambuAdapter struct {
	name       string
	ip         string
	serial     string
	accessCode string

	mu             sync.RWMutex
	client         mqtt.Client
	state          PrinterState
	hmsCodes       []HMSCode
	stateCallbacks []func(StateChangeEvent)

	// pushThrottle paces ams_filament_setting publishes; see bambuPushTrayMinGap.
	pushThrottle *paceThrottler
}

// NewBambuAdapter creates a new Bambu printer adapter.
func NewBambuAdapter(name, ip, serial, accessCode string) *BambuAdapter {
	return &BambuAdapter{
		name:       name,
		ip:         ip,
		serial:     serial,
		accessCode: accessCode,
		state: PrinterState{
			Name:       name,
			Type:       "bambu",
			State:      "offline",
			ActiveTray: -1,
		},
		pushThrottle: newPaceThrottler(bambuPushTrayMinGap),
	}
}

// Connect establishes the MQTT connection and subscribes to status reports.
func (b *BambuAdapter) Connect() error {
	broker := fmt.Sprintf("tls://%s:8883", b.ip)
	topic := fmt.Sprintf("device/%s/report", b.serial)

	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetUsername("bblp").
		SetPassword(b.accessCode).
		SetClientID(fmt.Sprintf("fil-%s", strings.ReplaceAll(b.name, " ", "-"))).
		SetTLSConfig(&tls.Config{InsecureSkipVerify: true}).
		SetAutoReconnect(true).
		SetConnectionLostHandler(func(client mqtt.Client, err error) {
			b.mu.Lock()
			b.state.State = "offline"
			b.mu.Unlock()
		}).
		SetOnConnectHandler(func(client mqtt.Client) {
			// Re-subscribe on reconnect
			client.Subscribe(topic, 0, nil)
			// Request full status
			reqTopic := fmt.Sprintf("device/%s/request", b.serial)
			client.Publish(reqTopic, 0, false, `{"pushing":{"command":"pushall","sequence_id":"0"}}`)
		})

	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		b.handleReport(msg.Payload())
	})

	b.client = mqtt.NewClient(opts)
	if token := b.client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("bambu %s: connection failed: %w", b.name, token.Error())
	}

	return nil
}

// Close disconnects from the printer.
func (b *BambuAdapter) Close() error {
	if b.client != nil && b.client.IsConnected() {
		b.client.Disconnect(250)
	}
	return nil
}

// Status returns the current printer state.
func (b *BambuAdapter) Status() PrinterState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// PushTray updates filament metadata for a specific AMS tray. Calls are
// serialized and paced per-printer via pushThrottle so a multi-move or
// full sync doesn't fire MQTT publishes faster than the printer firmware
// can apply them. Callers in the move/sync paths therefore don't need to
// add their own delays.
func (b *BambuAdapter) PushTray(update TrayUpdate) error {
	if b.client == nil || !b.client.IsConnected() {
		return fmt.Errorf("not connected to %s", b.name)
	}

	infoIdx := update.InfoIdx
	if infoIdx == "" {
		infoIdx = "GFL99" // fallback to generic PLA
	}

	cmd := map[string]interface{}{
		"print": map[string]interface{}{
			"command":         "ams_filament_setting",
			"sequence_id":     "0",
			"ams_id":          update.AmsID,
			"tray_id":         update.TrayID,
			"tray_color":      update.Color,
			"tray_type":       update.Type,
			"nozzle_temp_min": update.TempMin,
			"nozzle_temp_max": update.TempMax,
			"tray_info_idx":   infoIdx,
		},
	}

	payload, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	b.pushThrottle.Wait()

	reqTopic := fmt.Sprintf("device/%s/request", b.serial)
	token := b.client.Publish(reqTopic, 0, false, payload)
	token.Wait()
	return token.Error()
}

// OnStateChange registers a callback for printer state transitions.
func (b *BambuAdapter) OnStateChange(cb func(event StateChangeEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stateCallbacks = append(b.stateCallbacks, cb)
}

// handleReport parses an MQTT status report and updates internal state.
func (b *BambuAdapter) handleReport(payload []byte) {
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return
	}

	printData, ok := data["print"].(map[string]interface{})
	if !ok {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	oldState := b.state.State
	b.state.LastUpdated = time.Now()

	if gcodeState, ok := printData["gcode_state"].(string); ok {
		b.state.State = normalizeState(gcodeState)
		if b.state.State == "finished" && oldState != "finished" {
			b.state.LastFinishedAt = time.Now()
		}
	}

	if pct, ok := printData["mc_percent"].(float64); ok {
		b.state.Progress = int(pct)
	}

	if remaining, ok := printData["mc_remaining_time"].(float64); ok {
		b.state.RemainingMins = int(remaining)
	}

	if subtask, ok := printData["subtask_name"].(string); ok {
		b.state.CurrentFile = subtask
	}

	if layer, ok := printData["layer_num"].(float64); ok {
		b.state.Layer = int(layer)
	}

	if totalLayers, ok := printData["total_layer_num"].(float64); ok {
		b.state.TotalLayers = int(totalLayers)
	}

	// Parse AMS tray info
	if amsData, ok := printData["ams"].(map[string]interface{}); ok {
		if trayNow, ok := amsData["tray_now"].(string); ok {
			var trayIdx int
			fmt.Sscanf(trayNow, "%d", &trayIdx)
			b.state.ActiveTray = trayIdx
		}

		if amsList, ok := amsData["ams"].([]interface{}); ok {
			var trays []TrayInfo
			for _, ams := range amsList {
				a, ok := ams.(map[string]interface{})
				if !ok {
					continue
				}
				amsID := 0
				if id, ok := a["id"].(string); ok {
					fmt.Sscanf(id, "%d", &amsID)
				}

				trayList, ok := a["tray"].([]interface{})
				if !ok {
					continue
				}
				for _, tray := range trayList {
					t, ok := tray.(map[string]interface{})
					if !ok {
						continue
					}
					trayID := 0
					if id, ok := t["id"].(string); ok {
						fmt.Sscanf(id, "%d", &trayID)
					}

					color := ""
					if c, ok := t["tray_color"].(string); ok && len(c) >= 6 {
						color = c[:6] // strip alpha
					}

					trayType := ""
					if tt, ok := t["tray_type"].(string); ok {
						trayType = tt
					}

					tempMin, tempMax := 0, 0
					if v, ok := t["nozzle_temp_min"].(string); ok {
						fmt.Sscanf(v, "%d", &tempMin)
					}
					if v, ok := t["nozzle_temp_max"].(string); ok {
						fmt.Sscanf(v, "%d", &tempMax)
					}

					infoIdx := ""
					if v, ok := t["tray_info_idx"].(string); ok {
						infoIdx = v
					}

					trays = append(trays, TrayInfo{
						AmsID:   amsID,
						TrayID:  trayID,
						Color:   color,
						Type:    trayType,
						TempMin: tempMin,
						TempMax: tempMax,
						InfoIdx: infoIdx,
					})
				}
			}
			b.state.Trays = trays
		}
	}

	// Parse HMS codes
	prevHMS := make([]HMSCode, len(b.hmsCodes))
	copy(prevHMS, b.hmsCodes)
	if hmsList, ok := printData["hms"].([]interface{}); ok {
		var codes []HMSCode
		for _, h := range hmsList {
			hm, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			attr, _ := hm["attr"].(float64)
			code, _ := hm["code"].(float64)
			codes = append(codes, HMSCode{Attr: int(attr), Code: int(code)})
		}
		b.hmsCodes = codes
	}

	// Fire state change callbacks
	fireCallbacks := false
	if b.state.State != oldState && oldState != "" {
		// State changed — always fire
		fireCallbacks = true
	} else if b.state.State == "paused" && hasNewHMSCodes(prevHMS, b.hmsCodes) {
		// Already paused but new HMS codes appeared — fire to notify about additional faults
		fireCallbacks = true
	}

	if fireCallbacks {
		event := StateChangeEvent{
			OldState:     oldState,
			NewState:     b.state.State,
			HMSCodes:     b.hmsCodes,
			PrevHMSCodes: prevHMS,
		}
		for _, cb := range b.stateCallbacks {
			go cb(event)
		}
	}
}

// hasNewHMSCodes returns true if current contains any codes not in prev.
func hasNewHMSCodes(prev, current []HMSCode) bool {
	prevSet := make(map[string]bool)
	for _, h := range prev {
		prevSet[h.HMSCodeString()] = true
	}
	for _, h := range current {
		if !prevSet[h.HMSCodeString()] {
			return true
		}
	}
	return false
}

// normalizeState converts Bambu gcode_state values to normalized states.
func normalizeState(gcodeState string) string {
	switch gcodeState {
	case "IDLE":
		return "idle"
	case "RUNNING":
		return "printing"
	case "PAUSE":
		return "paused"
	case "FINISH":
		return "finished"
	case "FAILED":
		return "failed"
	default:
		return "idle"
	}
}
