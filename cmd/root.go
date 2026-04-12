/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// Config represents the structure of the config.json file
// Example at project root: config.json
//
//	{
//	  "location_aliases": {"A": "AMS A", ...}
//	}
//
// Add fields here as config grows.
type LocationCapacity struct {
	Capacity int `json:"capacity"`
}

type NotificationConfig struct {
	PushoverAPIKey  string `json:"pushover_api_key,omitempty"`
	PushoverUserKey string `json:"pushover_user_key,omitempty"`
	NtfyTopic       string `json:"ntfy_topic,omitempty"`
	NtfyServer      string `json:"ntfy_server,omitempty"` // defaults to https://ntfy.sh
	QuietStart      string `json:"quiet_start,omitempty"` // e.g. "22:00"
	QuietEnd        string `json:"quiet_end,omitempty"`   // e.g. "07:00"
}

type PrinterConfig struct {
	Locations  []string `json:"locations"`
	Type       string   `json:"type,omitempty"`        // "bambu" or "prusa"
	IP         string   `json:"ip,omitempty"`
	Serial     string   `json:"serial,omitempty"`      // Bambu only
	AccessCode string   `json:"access_code,omitempty"` // Bambu only
	Username   string   `json:"username,omitempty"`    // Prusa only
	Password   string   `json:"password,omitempty"`    // Prusa only
}

type Config struct {
	LocationAliases  map[string]string           `json:"location_aliases"`
	LocationCapacity map[string]LocationCapacity `json:"location_capacity"`
	ApiBase          string                      `json:"api_base"`
	// ApiBaseInternal is an optional Spoolman URL the plan server uses for its
	// own probes. Useful when the server's own hostname isn't resolvable from
	// inside its network (e.g. mDNS .local names inside Docker). Local-only
	// (not part of shared config) so each host can override independently.
	ApiBaseInternal string                      `json:"api_base_internal,omitempty"`
	LowThresholds   map[string]float64          `json:"low_thresholds"`
	LowIgnore       []string                    `json:"low_ignore"`
	Printers        map[string]PrinterConfig    `json:"printers"`
	Notifications   *NotificationConfig         `json:"notifications,omitempty"`
	PlansDir        string                      `json:"plans_dir"`
	ArchiveDir      string                      `json:"archive_dir"`
	PauseDir        string                      `json:"pause_dir"`
	PlansServer     string                      `json:"plans_server"`
	TLSSkipVerify   bool                        `json:"tls_skip_verify"`
	SharedConfigDir string                      `json:"shared_config_dir"`
	AssembliesDir   string                      `json:"assemblies_dir"`
}

// SharedConfig contains only the fields that are synced between machines via the server.
type SharedConfig struct {
	ApiBase          string                      `json:"api_base,omitempty"`
	LocationAliases  map[string]string           `json:"location_aliases,omitempty"`
	LocationCapacity map[string]LocationCapacity `json:"location_capacity,omitempty"`
	LowThresholds    map[string]float64          `json:"low_thresholds,omitempty"`
	LowIgnore        []string                    `json:"low_ignore,omitempty"`
	Printers         map[string]PrinterConfig    `json:"printers,omitempty"`
	Notifications    *NotificationConfig         `json:"notifications,omitempty"`
}

// ToSharedConfig extracts the shared fields from a full Config.
func (c *Config) ToSharedConfig() SharedConfig {
	return SharedConfig{
		ApiBase:          c.ApiBase,
		LocationAliases:  c.LocationAliases,
		LocationCapacity: c.LocationCapacity,
		LowThresholds:    c.LowThresholds,
		LowIgnore:        c.LowIgnore,
		Printers:         c.Printers,
		Notifications:    c.Notifications,
	}
}

// ApplyTo merges shared config fields into a full Config using the same merge semantics.
func (s SharedConfig) ApplyTo(dst *Config) {
	src := &Config{
		ApiBase:          s.ApiBase,
		LocationAliases:  s.LocationAliases,
		LocationCapacity: s.LocationCapacity,
		LowThresholds:    s.LowThresholds,
		LowIgnore:        s.LowIgnore,
		Printers:         s.Printers,
		Notifications:    s.Notifications,
	}
	mergeInto(dst, src)
}

// Cfg holds the loaded configuration and is available to all commands.
var Cfg *Config

// cfgFile is set from -c/--config flag.
var cfgFile string

// noColor toggles ANSI color output off when set via --no-color flag.
var noColor bool

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "fil",
	Short: "Fil is a command line tool for managing spoolman information",
	Long:  `Fil is a command line tool for managing spoolman information.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Apply color preference as early as possible, but only disable if the flag is set
		if noColor {
			color.NoColor = true
		}

		// Load config only once; subsequent subcommands in the chain need not reload
		if Cfg != nil {
			return nil
		}
		// Determine path: explicit flag takes precedence; else try merge from standard locations
		if cfgFile != "" {
			cfg, err := LoadConfig(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config from %s: %w", cfgFile, err)
			}
			Cfg = cfg

			return nil
		}

		cfg, err := LoadMergedConfig()
		if err != nil {
			return fmt.Errorf("unable to load config: %w", err)
		}
		// Config is optional; only set if any file existed
		if cfg != nil {
			Cfg = cfg
		}

		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// LoadConfig reads and parses JSON config from the given path.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("json config parsing error: %w", err)
	}

	return &c, nil
}

func exists(path string) bool {
	if path == "" {
		return false
	}

	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return !info.IsDir()
}

//nolint:gochecknoinits
func init() {
	// Global config flag for all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config file (config.json)")
	// Global color toggle
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable ANSI color output")
}

// LoadMergedConfig attempts to load and merge configs from standard locations when no explicit --config is provided.
// Precedence (later overrides earlier):
//  1. $HOME/.config/fil/shared-config.json (pulled from server)
//  2. $HOME/.config/fil/config.json (local overrides)
//
// If none exist, returns (nil, nil).
func LoadMergedConfig() (*Config, error) {
	paths := discoverConfigPaths()
	if len(paths) == 0 {
		return nil, nil
	}

	merged := &Config{}

	for _, p := range paths {
		c, err := LoadConfig(p)
		if err != nil {
			return nil, fmt.Errorf("failed loading %s: %w", p, err)
		}

		mergeInto(merged, c)
	}

	return merged, nil
}

// discoverConfigPaths returns existing config paths in merge order.
func discoverConfigPaths() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}

	configDir := filepath.Join(home, ".config", "fil")
	var out []string

	// 1) Shared config pulled from server (lowest precedence)
	if p := filepath.Join(configDir, "shared-config.json"); exists(p) {
		out = append(out, p)
	}
	// 2) Local config (overrides shared)
	if p := filepath.Join(configDir, "config.json"); exists(p) {
		out = append(out, p)
	}

	return out
}

// mergeInto copies non-zero values and maps from src into dst.
// Maps are merged by keys; src keys override dst.
func mergeInto(dst, src *Config) {
	if src == nil || dst == nil {
		return
	}

	if src.ApiBase != "" {
		dst.ApiBase = src.ApiBase
	}
	if src.ApiBaseInternal != "" {
		dst.ApiBaseInternal = src.ApiBaseInternal
	}
	// maps
	if src.LocationAliases != nil {
		if dst.LocationAliases == nil {
			dst.LocationAliases = map[string]string{}
		}

		for k, v := range src.LocationAliases {
			dst.LocationAliases[k] = v
		}
	}

	if src.LowThresholds != nil {
		if dst.LowThresholds == nil {
			dst.LowThresholds = map[string]float64{}
		}

		for k, v := range src.LowThresholds {
			dst.LowThresholds[k] = v
		}
	}

	if src.LocationCapacity != nil {
		if dst.LocationCapacity == nil {
			dst.LocationCapacity = map[string]LocationCapacity{}
		}

		for k, v := range src.LocationCapacity {
			dst.LocationCapacity[k] = v
		}
	}

	if src.Printers != nil {
		if dst.Printers == nil {
			dst.Printers = map[string]PrinterConfig{}
		}

		for k, v := range src.Printers {
			dst.Printers[k] = v
		}
	}

	// slices
	if src.LowIgnore != nil {
		// append to allow layered config; duplicates are acceptable
		dst.LowIgnore = append(dst.LowIgnore, src.LowIgnore...)
	}

	if src.PlansDir != "" {
		dst.PlansDir = src.PlansDir
	}

	if src.ArchiveDir != "" {
		dst.ArchiveDir = src.ArchiveDir
	}

	if src.PauseDir != "" {
		dst.PauseDir = src.PauseDir
	}

	if src.PlansServer != "" {
		dst.PlansServer = src.PlansServer
	}

	if src.TLSSkipVerify {
		dst.TLSSkipVerify = true
	}

	if src.SharedConfigDir != "" {
		dst.SharedConfigDir = src.SharedConfigDir
	}

	if src.AssembliesDir != "" {
		dst.AssembliesDir = src.AssembliesDir
	}

	if src.Notifications != nil {
		dst.Notifications = src.Notifications
	}
}
