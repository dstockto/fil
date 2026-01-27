package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildUseQuery(t *testing.T) {
	Cfg = &Config{
		LocationAliases: map[string]string{
			"A": "AMS A",
		},
	}

	tests := []struct {
		name     string
		location string
		expected string
	}{
		{"no location", "", ""},
		{"with alias", "A", "AMS A"},
		{"lowercase alias", "a", "AMS A"},
		{"no alias", "Shelf 1", "Shelf 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			query, _ := buildUseQuery(cmd, tt.location)
			if tt.location == "" {
				if _, ok := query["location"]; ok {
					t.Errorf("expected no location in query, got %v", query["location"])
				}
			} else {
				if query["location"] != tt.expected {
					t.Errorf("expected location %s, got %s", tt.expected, query["location"])
				}
			}
		})
	}
}

func TestBuildFindQuery(t *testing.T) {
	Cfg = &Config{
		LocationAliases: map[string]string{
			"A": "AMS A",
		},
	}

	tests := []struct {
		name     string
		flags    map[string]string
		expected map[string]string
	}{
		{
			"manufacturer",
			map[string]string{"manufacturer": "PolyMaker"},
			map[string]string{"manufacturer": "PolyMaker"},
		},
		{
			"location alias",
			map[string]string{"location": "a"},
			map[string]string{"location": "AMS A"},
		},
		{
			"allowed archived",
			map[string]string{"allowed-archived": "true"},
			map[string]string{"allow_archived": "true"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("diameter", "1.75", "")
			cmd.Flags().String("manufacturer", "", "")
			cmd.Flags().Bool("allowed-archived", false, "")
			cmd.Flags().Bool("archived-only", false, "")
			cmd.Flags().Bool("has-comment", false, "")
			cmd.Flags().String("comment", "", "")
			cmd.Flags().Bool("used", false, "")
			cmd.Flags().Bool("pristine", false, "")
			cmd.Flags().String("location", "", "")

			for k, v := range tt.flags {
				_ = cmd.Flags().Set(k, v)
			}

			query, _, _ := buildFindQuery(cmd)
			for k, v := range tt.expected {
				if query[k] != v {
					t.Errorf("expected query[%s] to be %s, got %s", k, v, query[k])
				}
			}
		})
	}
}

func TestBuildMoveQuery(t *testing.T) {
	Cfg = &Config{
		LocationAliases: map[string]string{
			"A": "AMS A",
		},
	}

	query := buildMoveQuery("a")
	if query["location"] != "AMS A" {
		t.Errorf("expected location AMS A, got %s", query["location"])
	}
}

func TestBuildArchiveQuery(t *testing.T) {
	Cfg = &Config{
		LocationAliases: map[string]string{
			"A": "AMS A",
		},
	}

	query := buildArchiveQuery("a")
	if query["location"] != "AMS A" {
		t.Errorf("expected location AMS A, got %s", query["location"])
	}
}
