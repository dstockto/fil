package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotifierEnabledIncludesVoiceMonkey(t *testing.T) {
	tests := []struct {
		name string
		cfg  NotificationConfig
		want bool
	}{
		{"nothing configured", NotificationConfig{}, false},
		{"only pushover", NotificationConfig{PushoverAPIKey: "k", PushoverUserKey: "u"}, true},
		{"only ntfy", NotificationConfig{NtfyTopic: "t"}, true},
		{"only voice monkey", NotificationConfig{VoiceMonkeyToken: "tok", VoiceMonkeyDevice: "dev"}, true},
		{"voice monkey token without device is disabled", NotificationConfig{VoiceMonkeyToken: "tok"}, false},
		{"voice monkey device without token is disabled", NotificationConfig{VoiceMonkeyDevice: "dev"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewNotifier(tt.cfg).Enabled()
			if got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpeakNoopWhenNotConfigured(t *testing.T) {
	// No fake server — if Speak dialed out we'd know it tried.
	n := NewNotifier(NotificationConfig{})
	if err := n.Speak("hello"); err != nil {
		t.Fatalf("unexpected error speaking with no config: %v", err)
	}
}

func TestSpeakHitsVoiceMonkeyWithCorrectQuery(t *testing.T) {
	var gotPath, gotToken, gotDevice, gotText string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		q := r.URL.Query()
		gotToken = q.Get("token")
		gotDevice = q.Get("device")
		gotText = q.Get("text")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	n := NewNotifier(NotificationConfig{
		VoiceMonkeyToken:  "secret-token",
		VoiceMonkeyDevice: "Office Echo",
		VoiceMonkeyURL:    ts.URL,
	})

	if err := n.Speak("Bambu X1C finished a print"); err != nil {
		t.Fatalf("Speak: %v", err)
	}

	if gotPath != "/announcement" {
		t.Errorf("path = %q, want /announcement", gotPath)
	}
	if gotToken != "secret-token" {
		t.Errorf("token = %q, want secret-token", gotToken)
	}
	if gotDevice != "Office Echo" {
		t.Errorf("device = %q, want Office Echo", gotDevice)
	}
	if gotText != "Bambu X1C finished a print" {
		t.Errorf("text = %q, want the speech string", gotText)
	}
}

func TestSpeakReturnsErrorOnBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer ts.Close()

	n := NewNotifier(NotificationConfig{
		VoiceMonkeyToken:  "bad",
		VoiceMonkeyDevice: "d",
		VoiceMonkeyURL:    ts.URL,
	})

	if err := n.Speak("x"); err == nil {
		t.Fatal("expected error on 401, got nil")
	}
}

func TestValidateVoiceMonkey(t *testing.T) {
	if err := NewNotifier(NotificationConfig{}).ValidateVoiceMonkey(); err == nil {
		t.Error("expected error when not configured")
	}
	if err := NewNotifier(NotificationConfig{VoiceMonkeyToken: "t"}).ValidateVoiceMonkey(); err == nil {
		t.Error("expected error when only token set")
	}
	if err := NewNotifier(NotificationConfig{VoiceMonkeyDevice: "d"}).ValidateVoiceMonkey(); err == nil {
		t.Error("expected error when only device set")
	}
	if err := NewNotifier(NotificationConfig{VoiceMonkeyToken: "t", VoiceMonkeyDevice: "d"}).ValidateVoiceMonkey(); err != nil {
		t.Errorf("expected nil when both configured, got %v", err)
	}
}
