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
	Database        string            `json:"database"`
	LocationAliases map[string]string `json:"location_aliases"`
	ApiBase         string            `json:"api_base"`
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
		// Determine path: explicit flag takes precedence; else try default path
		path := cfgFile
		if path == "" {
			if def, ok := defaultConfigPath(); ok {
				path = def
			} else {
				// No config available; not an error per requirements ("if possible")
				return nil
			}
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			// If user explicitly set a path, and it fails, surface the error
			if cfgFile != "" {
				return fmt.Errorf("failed to load config from %s: %w", path, err)
			}

			return fmt.Errorf("unable to load config: %v", err)
		}

		if cfg == nil {
			return fmt.Errorf("failed to load config from %s: empty config", path)
		}
		Cfg = cfg
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

// defaultConfigPath returns a default config.json path if it exists in common locations
func defaultConfigPath() (string, bool) {
	// Prefer current working directory config.json
	cwd, _ := os.Getwd()
	if cwd != "" {
		p := filepath.Join(cwd, "config.json")
		if exists(p) {
			return p, true
		}
	}
	// Also check XDG config: $XDG_CONFIG_HOME/fil/config.json or $HOME/.config/fil/config.json
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		p := filepath.Join(xdg, "fil", "config.json")
		if exists(p) {
			return p, true
		}
	}
	if home, _ := os.UserHomeDir(); home != "" {
		p := filepath.Join(home, ".config", "fil", "config.json")
		if exists(p) {
			return p, true
		}
	}
	return "", false
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
