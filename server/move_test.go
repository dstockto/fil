package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/inventory"
	"github.com/dstockto/fil/models"
)

// memSpoolman is an in-memory inventory.Spoolman.
type memSpoolman struct {
	orders    map[string][]int
	locations map[int]string
	archived  []int
}

func (m *memSpoolman) GetSettings(context.Context) (map[string]api.SettingEntry, error) {
	inner, _ := json.Marshal(m.orders)
	wrapped, _ := json.Marshal(string(inner))
	return map[string]api.SettingEntry{"locations_spoolorders": {Value: wrapped, IsSet: true}}, nil
}

func (m *memSpoolman) PostSettingObject(_ context.Context, _ string, obj any) error {
	m.orders = obj.(map[string][]int)
	return nil
}

func (m *memSpoolman) FindSpoolsById(_ context.Context, id int) (*models.FindSpool, error) {
	loc, ok := m.locations[id]
	if !ok {
		return nil, api.ErrSpoolNotFound
	}
	s := &models.FindSpool{Id: id, Location: loc}
	s.Filament.ColorHex = "e8e4dc"
	s.Filament.Material = "PETG"
	return s, nil
}

func (m *memSpoolman) MoveSpool(ctx context.Context, id int, to string) error {
	if err := ctx.Err(); err != nil {
		return err // a real client's request would be canceled too
	}
	m.locations[id] = to
	return nil
}

func (m *memSpoolman) ArchiveSpool(ctx context.Context, id int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.archived = append(m.archived, id)
	m.locations[id] = ""
	return nil
}

// pushAdapter records tray pushes.
type pushAdapter struct {
	lockedAdapter
	pushes []TrayUpdate
	err    error
}

func (p *pushAdapter) PushTray(u TrayUpdate) error {
	p.pushes = append(p.pushes, u)
	return p.err
}

func moveServer(t *testing.T) (*PlanServer, *memSpoolman, *pushAdapter) {
	t.Helper()
	sm := &memSpoolman{
		orders:    map[string][]int{"Shelf 1A": {52, 194}, "AMS A": {159, 114, 267, 197}, "Prusa": {1, 2, 3, 4, 5}},
		locations: map[int]string{52: "Shelf 1A", 194: "Shelf 1A", 159: "AMS A", 114: "AMS A", 267: "AMS A", 197: "AMS A", 1: "Prusa", 2: "Prusa", 3: "Prusa", 4: "Prusa", 5: "Prusa"},
	}
	x1c := &pushAdapter{}
	pm := NewPrinterManager()
	if err := pm.AddAdapter("X1C", x1c); err != nil {
		t.Fatal(err)
	}
	s := &PlanServer{
		Spoolman: sm,
		Printers: pm,
		Layout: inventory.Layout{
			Printers: []inventory.Printer{
				{Name: "X1C", Type: "bambu", Locations: []string{"AMS A", "AMS B", "AMS C"}},
				{Name: "XL", Type: "prusa", Locations: []string{"Prusa"}},
			},
			Capacity: map[string]int{"AMS A": 4, "AMS B": 4, "AMS C": 4, "Prusa": 5},
		},
	}
	return s, sm, x1c
}

func postMove(t *testing.T, s *PlanServer, id, body string) (*httptest.ResponseRecorder, moveResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/fil/spools/"+id+"/move", strings.NewReader(body))
	s.Routes().ServeHTTP(rec, req)
	var resp moveResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
	}
	return rec, resp
}

func TestMoveSpoolIntoAMSPushesTray(t *testing.T) {
	s, sm, x1c := moveServer(t)
	rec, resp := postMove(t, s, "194", `{"location":"AMS A","slot":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if resp.From != "Shelf 1A" || resp.To != "AMS A" || resp.Slot != 3 || resp.Displaced != 267 || !resp.TrayPushed {
		t.Errorf("response = %+v", resp)
	}
	if sm.locations[194] != "AMS A" {
		t.Errorf("spool location = %q", sm.locations[194])
	}
	if len(x1c.pushes) != 1 || x1c.pushes[0].AmsID != 0 || x1c.pushes[0].TrayID != 2 || x1c.pushes[0].Color != "E8E4DCFF" {
		t.Errorf("tray pushes = %+v, want AMS 0 tray 2", x1c.pushes)
	}
}

func TestMoveSpoolTrayFailureStillReportsTheMove(t *testing.T) {
	s, sm, x1c := moveServer(t)
	x1c.err = errors.New("printer offline")
	rec, resp := postMove(t, s, "194", `{"location":"AMS A","slot":1}`)
	if rec.Code != http.StatusOK || resp.TrayPushed || resp.TrayError != "printer offline" || sm.locations[194] != "AMS A" {
		t.Errorf("status %d, response %+v", rec.Code, resp)
	}
}

func TestMoveSpoolNoTrayPush(t *testing.T) {
	tests := []struct{ name, body string }{
		{"shelf", `{"location":"Top Shelf"}`},
		{"prusa (no tray push support)", `{"location":"Prusa","slot":2}`},
		// Full AMS, no slot: the spool is listed past capacity, so it's in no tray.
		{"past capacity", `{"location":"AMS A"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, x1c := moveServer(t)
			rec, resp := postMove(t, s, "194", tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if resp.TrayPushed || len(x1c.pushes) != 0 {
				t.Errorf("pushed a tray: %+v %+v", resp, x1c.pushes)
			}
		})
	}
}

func TestMoveSpoolErrors(t *testing.T) {
	tests := []struct {
		name, id, body string
		want           int
	}{
		{"non-numeric id", "abc", `{"location":"AMS A"}`, http.StatusBadRequest},
		{"bad JSON", "194", `{`, http.StatusBadRequest},
		{"no location", "194", `{}`, http.StatusBadRequest},
		{"unknown spool", "4242", `{"location":"AMS A"}`, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := moveServer(t)
			if rec, _ := postMove(t, s, tt.id, tt.body); rec.Code != tt.want {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

// A client that hangs up after sending the body must not leave the slot
// order written but the spool's location unchanged.
func TestMoveSpoolFinishesAfterClientDisconnect(t *testing.T) {
	s, sm, _ := moveServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/fil/spools/194/move", strings.NewReader(`{"location":"Shelf 3C"}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if sm.locations[194] != "Shelf 3C" || !contains(sm.orders["Shelf 3C"], 194) {
		t.Errorf("location = %q, Shelf 3C order = %v; want both updated", sm.locations[194], sm.orders["Shelf 3C"])
	}
}

func contains(ids []int, id int) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func TestMoveSpoolSlotPastCapacity(t *testing.T) {
	s, sm, _ := moveServer(t)
	if rec, _ := postMove(t, s, "194", `{"location":"AMS A","slot":1000}`); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if len(sm.orders["AMS A"]) != 4 || sm.locations[194] != "Shelf 1A" {
		t.Errorf("wrote anyway: AMS A = %v, location = %q", sm.orders["AMS A"], sm.locations[194])
	}
}

func TestMoveSpoolDisabledWithoutSpoolman(t *testing.T) {
	rec, _ := postMove(t, &PlanServer{}, "194", `{"location":"AMS A"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
