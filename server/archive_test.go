package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func postArchive(t *testing.T, s *PlanServer, id string) (*httptest.ResponseRecorder, archiveResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/fil/spools/"+id+"/archive", nil)
	s.Routes().ServeHTTP(rec, req)
	var resp archiveResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
	}
	return rec, resp
}

func TestArchiveSpoolFromOverflow(t *testing.T) {
	s, sm, _ := moveServer(t)
	sm.orders["AMS A"] = []int{159, 114, 194, 197, 267}
	sm.locations[194] = "AMS A"
	rec, resp := postArchive(t, s, "267")
	if rec.Code != http.StatusOK || resp.SpoolID != 267 || resp.From != "AMS A" {
		t.Fatalf("status %d, response %+v: %s", rec.Code, resp, rec.Body.String())
	}
	if want := []int{159, 114, 194, 197}; !reflect.DeepEqual(sm.orders["AMS A"], want) {
		t.Errorf("AMS A = %v, want %v", sm.orders["AMS A"], want)
	}
	if !reflect.DeepEqual(sm.archived, []int{267}) || sm.locations[267] != "" {
		t.Errorf("archived = %v, location = %q", sm.archived, sm.locations[267])
	}
}

func TestArchiveSpoolErrors(t *testing.T) {
	tests := []struct {
		name, id string
		want     int
	}{
		{"not a number", "abc", http.StatusBadRequest},
		{"zero", "0", http.StatusBadRequest},
		{"unknown spool", "999", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, sm, _ := moveServer(t)
			if rec, _ := postArchive(t, s, tt.id); rec.Code != tt.want {
				t.Errorf("status %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
			if len(sm.archived) != 0 {
				t.Errorf("archived %v", sm.archived)
			}
		})
	}
}

func TestArchiveSpoolFinishesAfterClientDisconnect(t *testing.T) {
	s, sm, _ := moveServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/fil/spools/52/archive", nil).WithContext(ctx)
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(sm.archived, []int{52}) {
		t.Errorf("status %d, archived %v: the order write and the archive must both land", rec.Code, sm.archived)
	}
}

func TestArchiveSpoolDisabledWithoutSpoolman(t *testing.T) {
	s := &PlanServer{}
	if rec, _ := postArchive(t, s, "52"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", rec.Code)
	}
}
