package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestWorkflowsNoDeprecatedActionVersions guards against accidental
// regression to Node-20-based action versions. GitHub forces Node.js 24
// defaults on 2026-06-02 and removes Node.js 20 on 2026-09-16; pinning
// `actions/checkout@v1..@v4` triggers a deprecation warning on every run
// and will break outright after the hard removal. The current safe major
// for actions/checkout is v5 (Node 24, stable since 2025-11-17) or later.
func TestWorkflowsNoDeprecatedActionVersions(t *testing.T) {
	deprecated := regexp.MustCompile(`actions/checkout@v[1-4](\b|\.)`)

	matches, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no workflow files found under .github/workflows/")
	}

	for _, path := range matches {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if deprecated.MatchString(line) {
				t.Errorf("%s:%d pins deprecated Node-20 action: %s", path, i+1, strings.TrimSpace(line))
			}
		}
	}
}
