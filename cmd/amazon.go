package cmd

import (
	"net/url"
	"strings"
)

func makeAmazonSearch(vendor string, name string) string {
	q := url.QueryEscape(strings.TrimSpace(vendor + " " + name))

	return "https://www.amazon.com/s?k=" + q
}

// Build an iTerm2-compatible OSC 8 hyperlink: label "text" pointing to "link".
// Example format: \x1b]8;;http://example.com\x1b\\This is a link\x1b]8;;\x1b\\
func termLink(text string, link string) string {
	return "\x1b]8;;" + link + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

func amazonLink(vendor string, name string) string {
	return termLink(makeAmazonSearch(vendor, name), makeAmazonSearch(vendor, name))
}
