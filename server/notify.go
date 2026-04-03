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
	PushoverAPIKey  string
	PushoverUserKey string
	NtfyTopic       string
	NtfyServer      string // defaults to https://ntfy.sh
	QuietStart      string // e.g. "22:00"
	QuietEnd        string // e.g. "07:00"
}

// Notifier sends notifications via configured channels (Pushover, ntfy).
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
		n.config.NtfyTopic != ""
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
