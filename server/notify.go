package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NotificationConfig mirrors cmd.NotificationConfig for use in the server package.
type NotificationConfig struct {
	PushoverAPIKey    string
	PushoverUserKey   string
	NtfyTopic         string
	NtfyServer        string // defaults to https://ntfy.sh
	VoiceMonkeyToken  string
	VoiceMonkeyDevice string
	VoiceMonkeyURL    string // defaults to https://api-v2.voicemonkey.io
	QuietStart        string // e.g. "22:00"
	QuietEnd          string // e.g. "07:00"
}

// Notifier sends notifications via configured channels (Pushover, ntfy, Voice Monkey).
type Notifier struct {
	config NotificationConfig
}

// NewNotifier creates a Notifier from the given config.
func NewNotifier(cfg NotificationConfig) *Notifier {
	return &Notifier{config: cfg}
}

// Enabled returns true if at least one notification channel is configured.
func (n *Notifier) Enabled() bool {
	return (n.config.PushoverAPIKey != "" && n.config.PushoverUserKey != "") ||
		n.config.NtfyTopic != "" ||
		n.voiceMonkeyEnabled()
}

// voiceMonkeyEnabled returns true if Voice Monkey has both token and device configured.
func (n *Notifier) voiceMonkeyEnabled() bool {
	return n.config.VoiceMonkeyToken != "" && n.config.VoiceMonkeyDevice != ""
}

// Send dispatches a notification to all configured channels.
// Errors are logged but do not stop delivery to other channels.
func (n *Notifier) Send(title, message string) []error {
	var errs []error

	if n.config.PushoverAPIKey != "" && n.config.PushoverUserKey != "" {
		if err := n.sendPushover(title, message); err != nil {
			errs = append(errs, fmt.Errorf("pushover: %w", err))
		}
	}

	if n.config.NtfyTopic != "" {
		if err := n.sendNtfy(title, message); err != nil {
			errs = append(errs, fmt.Errorf("ntfy: %w", err))
		}
	}

	return errs
}

// IsQuietHours returns true if the given time falls within the configured quiet window.
func (n *Notifier) IsQuietHours(t time.Time) bool {
	if n.config.QuietStart == "" || n.config.QuietEnd == "" {
		return false
	}
	start, err := time.Parse("15:04", n.config.QuietStart)
	if err != nil {
		return false
	}
	end, err := time.Parse("15:04", n.config.QuietEnd)
	if err != nil {
		return false
	}

	now := t.Hour()*60 + t.Minute()
	s := start.Hour()*60 + start.Minute()
	e := end.Hour()*60 + end.Minute()

	if s <= e {
		// Same-day window (e.g., 09:00 - 17:00)
		return now >= s && now < e
	}
	// Overnight window (e.g., 22:00 - 07:00)
	return now >= s || now < e
}

// QuietEndTime returns the next time quiet hours end, relative to the given time.
func (n *Notifier) QuietEndTime(t time.Time) time.Time {
	end, _ := time.Parse("15:04", n.config.QuietEnd)
	endToday := time.Date(t.Year(), t.Month(), t.Day(), end.Hour(), end.Minute(), 0, 0, t.Location())
	if endToday.After(t) {
		return endToday
	}
	return endToday.Add(24 * time.Hour)
}

// ValidatePushover verifies the configured Pushover token + user keys by calling
// the validate endpoint. Returns nil if credentials are valid, or an error if
// they are missing, rejected, or the request fails.
func (n *Notifier) ValidatePushover() error {
	if n.config.PushoverAPIKey == "" || n.config.PushoverUserKey == "" {
		return fmt.Errorf("pushover credentials not configured")
	}

	data := url.Values{
		"token": {n.config.PushoverAPIKey},
		"user":  {n.config.PushoverUserKey},
	}

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.PostForm("https://api.pushover.net/1/users/validate.json", data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pushover rejected credentials: status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendPushover(title, message string) error {
	data := url.Values{
		"token":   {n.config.PushoverAPIKey},
		"user":    {n.config.PushoverUserKey},
		"title":   {title},
		"message": {message},
	}

	resp, err := http.PostForm("https://api.pushover.net/1/messages.json", data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// TestAll fires every configured channel with a canned message and records
// per-channel outcomes into results ("sent" | "skipped: <reason>" |
// "error: <msg>"). Used by the notify-test endpoint; bypasses the normal
// "all-or-nothing" Send semantics so the caller sees which channels worked.
func (n *Notifier) TestAll(message string, results map[string]string) {
	if n.config.PushoverAPIKey != "" && n.config.PushoverUserKey != "" {
		if err := n.sendPushover("Fil test", message); err != nil {
			results["pushover"] = "error: " + err.Error()
		} else {
			results["pushover"] = "sent"
		}
	} else {
		results["pushover"] = "skipped: not configured"
	}

	if n.config.NtfyTopic != "" {
		if err := n.sendNtfy("Fil test", message); err != nil {
			results["ntfy"] = "error: " + err.Error()
		} else {
			results["ntfy"] = "sent"
		}
	} else {
		results["ntfy"] = "skipped: not configured"
	}

	if n.voiceMonkeyEnabled() {
		if err := n.sendVoiceMonkey(message); err != nil {
			results["voicemonkey"] = "error: " + err.Error()
		} else {
			results["voicemonkey"] = "sent"
		}
	} else {
		results["voicemonkey"] = "skipped: not configured"
	}
}

// Speak sends a text-only announcement to Voice Monkey (spoken on the
// configured Echo). No-op when Voice Monkey isn't configured. Kept separate
// from Send because speech doesn't want a title, and we only speak a subset
// of events (finish/fail/non-user pause) to avoid announcement fatigue.
func (n *Notifier) Speak(text string) error {
	if !n.voiceMonkeyEnabled() {
		return nil
	}
	return n.sendVoiceMonkey(text)
}

// ValidateVoiceMonkey verifies Voice Monkey config is present. There's no
// silent validate endpoint, so this only checks that token+device are set —
// actual credential correctness surfaces on the first Speak attempt.
func (n *Notifier) ValidateVoiceMonkey() error {
	if n.config.VoiceMonkeyToken == "" || n.config.VoiceMonkeyDevice == "" {
		return fmt.Errorf("voice monkey credentials not configured")
	}
	return nil
}

func (n *Notifier) sendVoiceMonkey(text string) error {
	base := n.config.VoiceMonkeyURL
	if base == "" {
		base = "https://api-v2.voicemonkey.io"
	}

	q := url.Values{
		"token":  {n.config.VoiceMonkeyToken},
		"device": {n.config.VoiceMonkeyDevice},
		"text":   {text},
	}
	u := strings.TrimRight(base, "/") + "/announcement?" + q.Encode()

	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendNtfy(title, message string) error {
	ntfyServer := n.config.NtfyServer
	if ntfyServer == "" {
		ntfyServer = "https://ntfy.sh"
	}

	ntfyURL := fmt.Sprintf("%s/%s", strings.TrimRight(ntfyServer, "/"), n.config.NtfyTopic)

	req, err := http.NewRequest("POST", ntfyURL, strings.NewReader(message))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
