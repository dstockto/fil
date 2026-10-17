package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
)

// fakeSpoolman keeps locations_spoolorders and spool locations in memory and
// records the order of writes.
type fakeSpoolman struct {
	orders    map[string][]int
	locations map[int]string
	calls     []string
	postErr   error
}

func (f *fakeSpoolman) GetSettings(context.Context) (map[string]api.SettingEntry, error) {
	inner, _ := json.Marshal(f.orders)
	wrapped, _ := json.Marshal(string(inner))
	return map[string]api.SettingEntry{"locations_spoolorders": {Value: wrapped, IsSet: true}}, nil
}

func (f *fakeSpoolman) PostSettingObject(_ context.Context, key string, obj any) error {
	f.calls = append(f.calls, "post "+key)
	if f.postErr != nil {
		return f.postErr
	}
	f.orders = obj.(map[string][]int)
	return nil
}

func (f *fakeSpoolman) FindSpoolsById(_ context.Context, id int) (*models.FindSpool, error) {
	loc, ok := f.locations[id]
	if !ok {
		return nil, api.ErrSpoolNotFound
	}
	s := &models.FindSpool{Id: id, Location: loc}
	s.Filament.ColorHex = "e8e4dc"
	s.Filament.Material = "PETG"
	return s, nil
}

func (f *fakeSpoolman) MoveSpool(_ context.Context, id int, to string) error {
	f.calls = append(f.calls, "move")
	f.locations[id] = to
	return nil
}

func newFake() *fakeSpoolman {
	return &fakeSpoolman{
		orders:    map[string][]int{"Shelf 1A": {52, 53, 194}, "AMS A": {159, 114, 267, 197}},
		locations: map[int]string{52: "Shelf 1A", 53: "Shelf 1A", 194: "Shelf 1A", 159: "AMS A", 114: "AMS A", 267: "AMS A", 197: "AMS A"},
	}
}

func TestMoveIntoOccupiedAMSSlot(t *testing.T) {
	sm := newFake()
	res, err := Move(context.Background(), sm, testLayout(), MoveRequest{SpoolID: 194, Location: "AMS A", Slot: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.From != "Shelf 1A" || res.To != "AMS A" || res.Slot != 3 || res.Displaced != 267 {
		t.Errorf("result = %+v", res)
	}
	if want := []int{159, 114, 194, 197, 267}; !reflect.DeepEqual(sm.orders["AMS A"], want) {
		t.Errorf("AMS A = %v, want %v", sm.orders["AMS A"], want)
	}
	if want := []int{52, 53}; !reflect.DeepEqual(sm.orders["Shelf 1A"], want) {
		t.Errorf("Shelf 1A = %v, want %v", sm.orders["Shelf 1A"], want)
	}
	if sm.locations[194] != "AMS A" {
		t.Errorf("spool location = %q", sm.locations[194])
	}
	// Order first, then location, like the CLI, so Spoolman's UI order is
	// already right when the location change lands.
	if want := []string{"post locations_spoolorders", "move"}; !reflect.DeepEqual(sm.calls, want) {
		t.Errorf("calls = %v, want %v", sm.calls, want)
	}
}

func TestMoveToShelfLeavesAnEmptyPrinterSlot(t *testing.T) {
	sm := newFake()
	res, err := Move(context.Background(), sm, testLayout(), MoveRequest{SpoolID: 267, Location: "Top Shelf"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{159, 114, -1, 197}; !reflect.DeepEqual(sm.orders["AMS A"], want) {
		t.Errorf("AMS A = %v, want %v", sm.orders["AMS A"], want)
	}
	if res.Slot != 1 || res.Displaced != 0 || !reflect.DeepEqual(sm.orders["Top Shelf"], []int{267}) {
		t.Errorf("result = %+v, Top Shelf = %v", res, sm.orders["Top Shelf"])
	}
}

func TestMoveRejectsBadRequests(t *testing.T) {
	for _, req := range []MoveRequest{
		{SpoolID: 0, Location: "AMS A"},
		{SpoolID: 194, Location: "  "},
		{SpoolID: 194, Location: "AMS A", Slot: -1},
	} {
		sm := newFake()
		if _, err := Move(context.Background(), sm, testLayout(), req); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v: err = %v, want ErrBadRequest", req, err)
		}
		if len(sm.calls) != 0 {
			t.Errorf("%+v: wrote to Spoolman: %v", req, sm.calls)
		}
	}
}

func TestMoveUnknownSpool(t *testing.T) {
	sm := newFake()
	_, err := Move(context.Background(), sm, testLayout(), MoveRequest{SpoolID: 4242, Location: "AMS A"})
	if !errors.Is(err, api.ErrSpoolNotFound) || len(sm.calls) != 0 {
		t.Errorf("err = %v, calls = %v", err, sm.calls)
	}
}

func TestMoveStopsWhenOrderWriteFails(t *testing.T) {
	sm := newFake()
	sm.postErr = errors.New("boom")
	if _, err := Move(context.Background(), sm, testLayout(), MoveRequest{SpoolID: 194, Location: "Shelf 3C"}); err == nil {
		t.Fatal("expected an error")
	}
	if sm.locations[194] != "Shelf 1A" {
		t.Errorf("spool location changed to %q after the order write failed", sm.locations[194])
	}
}

func TestTrayPushFor(t *testing.T) {
	s := models.FindSpool{}
	s.Filament.ColorHex = "#e8e4dc"
	s.Filament.Material = "PETG"
	got := TrayPushFor(s)
	if got.Color != "E8E4DCFF" || got.Type != "PETG" || got.TempMin != 190 || got.TempMax != 240 {
		t.Errorf("tray push = %+v", got)
	}
}
