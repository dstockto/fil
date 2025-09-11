/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// Config represents the structure of the config.json file
// Example at project root: config.json
//
//	{
//	  "database": "spoolman.db",
//	  "location_aliases": {"A": "AMS A", ...}
//	}
//
// Add fields here as config grows.
type Config struct {
	Database        string             `json:"database"`
	LocationAliases map[string]string  `json:"location_aliases"`
	ApiBase         string             `json:"api_base"`
	LowThresholds   map[string]float64 `json:"low_thresholds"`
}

// Cfg holds the loaded configuration and is available to all commands
var Cfg *Config

// cfgFile is set from -c/--config flag
var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "fil",
	Short: "Fil is a command line tool for managing spoolman information",
	Long:  `Fil is a command line tool for managing spoolman information.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
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
			return fmt.Errorf("unable to load config: %v", err)
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

// LoadConfig reads and parses JSON config from the given path
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("json config parsing error: %v", err)
	}
	return &c, nil
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

func init() {
	// Global config flag for all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config file (config.json)")
}

// LoadMergedConfig attempts to load and merge configs from standard locations when no explicit --config is provided.
// Precedence (later overrides earlier):
//  1. $HOME/.config/fil/config.json
//  2. $XDG_CONFIG_HOME/fil/config.json
//  3. ./config.json (current working directory)
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
	var out []string
	// 1) HOME
	if home, _ := os.UserHomeDir(); home != "" {
		p := filepath.Join(home, ".config", "fil", "config.json")
		if exists(p) {
			out = append(out, p)
		}
	}
	// 2) XDG
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		p := filepath.Join(xdg, "fil", "config.json")
		if exists(p) {
			out = append(out, p)
		}
	}
	// 3) CWD
	if cwd, _ := os.Getwd(); cwd != "" {
		p := filepath.Join(cwd, "config.json")
		if exists(p) {
			out = append(out, p)
		}
	}
	return out
}

// mergeInto copies non-zero values and maps from src into dst.
// Maps are merged by keys; src keys override dst.
func mergeInto(dst, src *Config) {
	if src == nil || dst == nil {
		return
	}
	if src.Database != "" {
		dst.Database = src.Database
	}
	if src.ApiBase != "" {
		dst.ApiBase = src.ApiBase
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
}
