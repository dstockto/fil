package inventory

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dstockto/fil/api"
)

func TestArchiveOrders(t *testing.T) {
	l := testLayout()
	orders := map[string][]int{
		"AMS A":    {159, 114, 194, 197, 267}, // 267 bumped past capacity
		"AMS B":    {1, 2, -1, -1},
		"Shelf 1A": {52, 53},
	}
	got := l.ArchiveOrders(orders, 267, 52)
	want := map[string][]int{
		"AMS A":    {159, 114, 194, 197},
		"AMS B":    {1, 2, -1, -1}, // empty slots within capacity stay
		"Shelf 1A": {53},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("orders = %v, want %v", got, want)
	}
}

func TestArchiveOrdersKeepsAnEmptiedSlotWithinCapacity(t *testing.T) {
	got := testLayout().ArchiveOrders(map[string][]int{"AMS A": {159, 114, 267, 197}}, 197)
	if want := []int{159, 114, 267, -1}; !reflect.DeepEqual(got["AMS A"], want) {
		t.Errorf("AMS A = %v, want %v", got["AMS A"], want)
	}
}

func TestArchiveRanOutSpool(t *testing.T) {
	sm := newFake()
	sm.orders["AMS A"] = []int{159, 114, 194, 197, 267}
	sm.locations[194] = "AMS A"
	spool, err := Archive(context.Background(), sm, testLayout(), 267)
	if err != nil {
		t.Fatal(err)
	}
	if spool.Location != "AMS A" {
		t.Errorf("returned spool location = %q, want where it was", spool.Location)
	}
	if want := []int{159, 114, 194, 197}; !reflect.DeepEqual(sm.orders["AMS A"], want) {
		t.Errorf("AMS A = %v, want %v", sm.orders["AMS A"], want)
	}
	// Order first, like fil archive, so Spoolman's UI order is right when
	// the spool disappears.
	if want := []string{"post locations_spoolorders", "archive"}; !reflect.DeepEqual(sm.calls, want) {
		t.Errorf("calls = %v, want %v", sm.calls, want)
	}
}

func TestArchiveErrors(t *testing.T) {
	sm := newFake()
	if _, err := Archive(context.Background(), sm, testLayout(), 0); !errors.Is(err, ErrBadRequest) {
		t.Errorf("id 0: err = %v, want ErrBadRequest", err)
	}
	if _, err := Archive(context.Background(), sm, testLayout(), 999); !errors.Is(err, api.ErrSpoolNotFound) {
		t.Errorf("unknown spool: err = %v, want ErrSpoolNotFound", err)
	}
	sm.postErr = errors.New("spoolman down")
	if _, err := Archive(context.Background(), sm, testLayout(), 52); err == nil {
		t.Error("order write failure: want an error")
	}
	if want := []string{"post locations_spoolorders"}; !reflect.DeepEqual(sm.calls, want) {
		t.Errorf("calls = %v, want no archive after the failed order write", sm.calls)
	}
}
