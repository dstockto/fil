package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPatchFilament_Success(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/filament/") {
			t.Errorf("expected /api/v1/filament/ path, got %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, false)
	err := c.PatchFilament(context.Background(), 42, map[string]any{
		"color_hex": "ead9d4",
		"extra":     map[string]any{"td": "2.47"},
	})
	if err != nil {
		t.Fatalf("PatchFilament: %v", err)
	}
	if gotBody["color_hex"] != "ead9d4" {
		t.Errorf("unexpected color_hex: %v", gotBody["color_hex"])
	}
}

func TestPatchFilament_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, false)
	err := c.PatchFilament(context.Background(), 99, map[string]any{"color_hex": "ffffff"})
	if !errors.Is(err, ErrFilamentNotFound) {
		t.Fatalf("expected ErrFilamentNotFound, got %v", err)
	}
}

func TestPatchFilament_ApiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, false)
	err := c.PatchFilament(context.Background(), 42, map[string]any{"color_hex": "ffffff"})
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
}
