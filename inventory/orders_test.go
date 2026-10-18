package inventory

import (
	"reflect"
	"testing"
)

// testLayout mirrors a real setup: an X1C with three 4-slot AMS units and a
// 5-tool Prusa XL; shelves are plain locations.
func testLayout() Layout {
	return Layout{
		Printers: []Printer{
			{Name: "X1C", Type: "bambu", Locations: []string{"AMS A", "AMS B", "AMS C"}},
			{Name: "XL", Type: "prusa", Locations: []string{"Prusa"}},
		},
		Capacity: map[string]int{"AMS A": 4, "AMS B": 4, "AMS C": 4, "Prusa": 5, "Shelf 1A": 6},
	}
}

func TestPlace(t *testing.T) {
	tests := []struct {
		name          string
		list          []int
		dest          string
		pos           int
		want          []int
		wantDisplaced int
	}{
		{"printer: requested empty slot", []int{1, -1, 3, -1}, "AMS A", 2, []int{1, 99, 3, -1}, 0},
		// A spool that runs out mid-print is replaced in place; the old one
		// stays listed (after the last slot) so plan complete can charge both.
		{"printer: occupied slot bumps occupant to the end", []int{1, 2, 3, 4}, "AMS A", 3, []int{1, 2, 99, 4, 3}, 3},
		{"printer: no slot takes first empty", []int{1, -1, -1, 4}, "AMS A", 0, []int{1, 99, -1, 4}, 0},
		{"printer: no slot and full appends past capacity", []int{1, 2, 3, 4}, "AMS A", 0, []int{1, 2, 3, 4, 99}, 0},
		{"printer: slot past the list extends with empties", []int{1}, "Prusa", 4, []int{1, -1, -1, 99}, 0},
		{"shelf: append", []int{1, 2}, "Shelf 1A", 0, []int{1, 2, 99}, 0},
		{"shelf: insert at position", []int{1, 2, 3}, "Shelf 1A", 2, []int{1, 99, 2, 3}, 0},
		{"shelf: position past the end appends", []int{1, 2}, "Shelf 1A", 9, []int{1, 2, 99}, 0},
		{"new location", nil, "Top Shelf", 0, []int{99}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orders := map[string][]int{tt.dest: append([]int(nil), tt.list...)}
			displaced := testLayout().Place(orders, 99, tt.dest, tt.pos)
			if !reflect.DeepEqual(orders[tt.dest], tt.want) {
				t.Errorf("list = %v, want %v", orders[tt.dest], tt.want)
			}
			if displaced != tt.wantDisplaced {
				t.Errorf("displaced = %d, want %d", displaced, tt.wantDisplaced)
			}
		})
	}
}

func TestRemoveFromAllOrdersKeepsPrinterSlots(t *testing.T) {
	orders := map[string][]int{"AMS A": {1, 7, 3, 4}, "Shelf 1A": {5, 7, 6}}
	testLayout().RemoveFromAllOrders(orders, 7)
	if want := []int{1, -1, 3, 4}; !reflect.DeepEqual(orders["AMS A"], want) {
		t.Errorf("AMS A = %v, want %v (slot kept as empty)", orders["AMS A"], want)
	}
	if want := []int{5, 6}; !reflect.DeepEqual(orders["Shelf 1A"], want) {
		t.Errorf("Shelf 1A = %v, want %v (collapsed)", orders["Shelf 1A"], want)
	}
}

func TestPadToCapacity(t *testing.T) {
	l := testLayout()
	if got := l.PadToCapacity("Prusa", []int{1, 2}); !reflect.DeepEqual(got, []int{1, 2, -1, -1, -1}) {
		t.Errorf("Prusa = %v", got)
	}
	if got := l.PadToCapacity("Shelf 1A", []int{1}); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("shelves are never padded, got %v", got)
	}
}

func TestMapLocationToTray(t *testing.T) {
	l := testLayout()
	m := l.MapLocationToTray("AMS C", 2)
	if m == nil || m.PrinterName != "X1C" || m.AmsID != 2 || m.TrayID != 1 || !m.SupportsTrayPush() {
		t.Errorf("AMS C slot 2 = %+v", m)
	}
	if m := l.MapLocationToTray("Prusa", 1); m == nil || m.SupportsTrayPush() {
		t.Errorf("Prusa = %+v, want a mapping without tray push", m)
	}
	if m := l.MapLocationToTray("Shelf 1A", 1); m != nil {
		t.Errorf("shelf mapped to a tray: %+v", m)
	}
}

func TestTrayFor(t *testing.T) {
	l := testLayout()
	if m := l.TrayFor("AMS B", 4); m == nil || m.AmsID != 1 || m.TrayID != 3 {
		t.Errorf("AMS B slot 4 = %+v", m)
	}
	for _, tt := range []struct {
		loc  string
		slot int
	}{
		{"AMS A", 5},    // appended past a full 4-slot AMS: no such tray
		{"AMS A", 0},    // not in the list
		{"Prusa", 2},    // Prusa doesn't take tray pushes
		{"Shelf 1A", 1}, // not a printer
	} {
		if m := l.TrayFor(tt.loc, tt.slot); m != nil {
			t.Errorf("TrayFor(%q, %d) = %+v, want nil", tt.loc, tt.slot, m)
		}
	}
}
