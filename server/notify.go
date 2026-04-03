package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// NotificationConfig mirrors cmd.NotificationConfig for use in the server package.
type NotificationConfig struct {
	PushoverAPIKey  string
	PushoverUserKey string
	NtfyTopic       string
	NtfyServer      string // defaults to https://ntfy.sh
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
