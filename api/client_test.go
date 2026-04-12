package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetInfo_UsesInfoEndpointWhenAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			_, _ = w.Write([]byte(`{"version":"0.42.0"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, false)
	info, err := c.GetInfo(context.Background())
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if info.Version != "0.42.0" {
		t.Errorf("expected version 0.42.0, got %q", info.Version)
	}
}

func TestGetInfo_FallsBackOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/v1/spool" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, false)
	info, err := c.GetInfo(context.Background())
	if err != nil {
		t.Fatalf("GetInfo fallback failed: %v", err)
	}
	if info.Version != "" {
		t.Errorf("expected empty version on fallback, got %q", info.Version)
	}
}

func TestGetInfo_NonInfoErrorDoesNotFallBack(t *testing.T) {
	// Return 500 on /info; if GetInfo falls back we'll see a request to /spool.
	spoolCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/api/v1/spool" {
			spoolCalled = true
			_, _ = w.Write([]byte(`[]`))
			return
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, false)
	if _, err := c.GetInfo(context.Background()); err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	if spoolCalled {
		t.Error("fallback should not fire on non-404 errors")
	}
}

func TestGetInfo_BothEndpointsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, false)
	if _, err := c.GetInfo(context.Background()); err == nil {
		t.Fatal("expected error when both endpoints 404, got nil")
	}
}
