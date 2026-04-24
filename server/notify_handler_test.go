package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotifyTestNoChannelsConfigured(t *testing.T) {
	s, _ := setupTestServer(t)
	s.Notifier = NewNotifier(NotificationConfig{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify/test", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got NotifyTestResult
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Channels["_"] == "" || !strings.Contains(got.Channels["_"], "no channels configured") {
		t.Errorf("expected 'no channels configured' result, got %+v", got.Channels)
	}
}

func TestNotifyTestFiresVoiceMonkey(t *testing.T) {
	// Stand up a fake Voice Monkey endpoint so the test exercises the full path.
	var hit bool
	vm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if got := r.URL.Query().Get("text"); got != "hello echo" {
			t.Errorf("text = %q, want 'hello echo'", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer vm.Close()

	s, _ := setupTestServer(t)
	s.Notifier = NewNotifier(NotificationConfig{
		VoiceMonkeyToken:  "tok",
		VoiceMonkeyDevice: "dev",
		VoiceMonkeyURL:    vm.URL,
	})

	body := strings.NewReader(`{"message":"hello echo"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify/test", body)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !hit {
		t.Fatal("voice monkey endpoint was not called")
	}
	var got NotifyTestResult
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Channels["voicemonkey"] != "sent" {
		t.Errorf("voicemonkey = %q, want 'sent'", got.Channels["voicemonkey"])
	}
	if !strings.HasPrefix(got.Channels["pushover"], "skipped:") {
		t.Errorf("pushover = %q, want skipped", got.Channels["pushover"])
	}
}

func TestNotifyTestRespectsQuietHours(t *testing.T) {
	s, _ := setupTestServer(t)
	// Quiet hours covering the entire 24h window — easiest way to guarantee "now" is quiet.
	s.Notifier = NewNotifier(NotificationConfig{
		VoiceMonkeyToken:  "tok",
		VoiceMonkeyDevice: "dev",
		VoiceMonkeyURL:    "http://should-not-be-called.invalid",
		QuietStart:        "00:00",
		QuietEnd:          "23:59",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify/test", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)

	var got NotifyTestResult
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.QuietHours {
		t.Error("expected QuietHours=true")
	}
	if got.Channels["_"] == "" || !strings.Contains(got.Channels["_"], "quiet hours") {
		t.Errorf("expected 'quiet hours' skip, got %+v", got.Channels)
	}
}

func TestNotifyTestForceOverridesQuietHours(t *testing.T) {
	var hit bool
	vm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer vm.Close()

	s, _ := setupTestServer(t)
	s.Notifier = NewNotifier(NotificationConfig{
		VoiceMonkeyToken:  "tok",
		VoiceMonkeyDevice: "dev",
		VoiceMonkeyURL:    vm.URL,
		QuietStart:        "00:00",
		QuietEnd:          "23:59",
	})

	body := strings.NewReader(`{"force":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notify/test", body)
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, req)

	if !hit {
		t.Fatal("voice monkey should have been called when force=true even during quiet hours")
	}
}
