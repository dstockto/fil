package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeInto(t *testing.T) {
	dst := &Config{
		Database: "default.db",
		LocationAliases: map[string]string{
			"A": "AMS A",
			"B": "AMS B",
		},
		LowThresholds: map[string]float64{
			"PLA": 100.0,
		},
		LowIgnore: []string{"OldSpool"},
	}

	src := &Config{
		Database: "new.db",
		LocationAliases: map[string]string{
			"B": "Box B",
			"C": "Cabinet C",
		},
		LowThresholds: map[string]float64{
			"PETG": 200.0,
		},
		LowIgnore: []string{"BadSpool"},
		ApiBase:   "http://localhost:8000",
	}

	mergeInto(dst, src)

	if dst.Database != "new.db" {
		t.Errorf("expected Database to be %q, got %q", "new.db", dst.Database)
	}

	if dst.ApiBase != "http://localhost:8000" {
		t.Errorf("expected ApiBase to be %q, got %q", "http://localhost:8000", dst.ApiBase)
	}

	if dst.LocationAliases["A"] != "AMS A" {
		t.Errorf("expected LocationAliases[A] to be %q, got %q", "AMS A", dst.LocationAliases["A"])
	}
	if dst.LocationAliases["B"] != "Box B" {
		t.Errorf("expected LocationAliases[B] to be %q, got %q", "Box B", dst.LocationAliases["B"])
	}
	if dst.LocationAliases["C"] != "Cabinet C" {
		t.Errorf("expected LocationAliases[C] to be %q, got %q", "Cabinet C", dst.LocationAliases["C"])
	}

	if dst.LowThresholds["PLA"] != 100.0 {
		t.Errorf("expected LowThresholds[PLA] to be 100.0, got %f", dst.LowThresholds["PLA"])
	}
	if dst.LowThresholds["PETG"] != 200.0 {
		t.Errorf("expected LowThresholds[PETG] to be 200.0, got %f", dst.LowThresholds["PETG"])
	}

	expectedIgnore := []string{"OldSpool", "BadSpool"}
	if len(dst.LowIgnore) != 2 {
		t.Errorf("expected LowIgnore to have 2 elements, got %d", len(dst.LowIgnore))
	} else {
		for i, v := range expectedIgnore {
			if dst.LowIgnore[i] != v {
				t.Errorf("expected LowIgnore[%d] to be %q, got %q", i, v, dst.LowIgnore[i])
			}
		}
	}
}

func TestLoadConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fil-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{
		"database": "test.db",
		"api_base": "http://api.test",
		"location_aliases": {
			"T": "Test Location"
		}
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Database != "test.db" {
		t.Errorf("expected Database %q, got %q", "test.db", cfg.Database)
	}
	if cfg.ApiBase != "http://api.test" {
		t.Errorf("expected ApiBase %q, got %q", "http://api.test", cfg.ApiBase)
	}
	if cfg.LocationAliases["T"] != "Test Location" {
		t.Errorf("expected LocationAlias[T] %q, got %q", "Test Location", cfg.LocationAliases["T"])
	}
}

func TestExists(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "fil-exists-test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(tmpFile.Name())
	}()

	if !exists(tmpFile.Name()) {
		t.Errorf("exists(%q) returned false, want true", tmpFile.Name())
	}

	if exists(tmpFile.Name() + "nonexistent") {
		t.Errorf("exists() returned true for nonexistent file, want false")
	}

	tmpDir, _ := os.MkdirTemp("", "fil-exists-dir")
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()
	if exists(tmpDir) {
		t.Errorf("exists(%q) returned true for directory, want false", tmpDir)
	}
}
