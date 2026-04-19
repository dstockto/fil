package devices

import (
	"errors"
	"testing"
)

type fakeEnumerator struct {
	ports []PortInfo
	err   error
}

func (f fakeEnumerator) Find() ([]PortInfo, error) {
	return f.ports, f.err
}

func TestProbe(t *testing.T) {
	tests := []struct {
		name     string
		enum     Enumerator
		wantPath string
		wantErr  error
	}{
		{
			name:    "no devices",
			enum:    fakeEnumerator{},
			wantErr: ErrNoDevice,
		},
		{
			name: "single device",
			enum: fakeEnumerator{ports: []PortInfo{
				{Path: "/dev/cu.usbmodem14101", VID: TD1VID, PID: TD1PID, Serial: "abc"},
			}},
			wantPath: "/dev/cu.usbmodem14101",
		},
		{
			name: "multiple devices selects first",
			enum: fakeEnumerator{ports: []PortInfo{
				{Path: "/dev/cu.usbmodem14101"},
				{Path: "/dev/cu.usbmodem14201"},
			}},
			wantPath: "/dev/cu.usbmodem14101",
		},
		{
			name:    "enumerator error propagates",
			enum:    fakeEnumerator{err: errors.New("boom")},
			wantErr: errors.New("boom"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := Probe(tt.enum)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if errors.Is(tt.wantErr, ErrNoDevice) && !errors.Is(err, ErrNoDevice) {
					t.Fatalf("expected ErrNoDevice, got %v", err)
				}
				if !errors.Is(tt.wantErr, ErrNoDevice) && err.Error() != tt.wantErr.Error() {
					t.Fatalf("expected err %q, got %q", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Path != tt.wantPath {
				t.Fatalf("expected path %q, got %q", tt.wantPath, info.Path)
			}
		})
	}
}

func TestProbeNilUsesDefault(t *testing.T) {
	// Smoke test: nil enumerator must not panic; it just uses the default
	// (production) enumerator which will either find devices or return ErrNoDevice.
	_, err := Probe(nil)
	if err != nil && !errors.Is(err, ErrNoDevice) {
		// A real enumerator error (permission, etc.) is acceptable here; we
		// only fail on unexpected panic-equivalents.
		t.Logf("Probe(nil) returned %v (acceptable if no device attached)", err)
	}
}
