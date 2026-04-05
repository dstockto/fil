package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var printerAddCmd = &cobra.Command{
	Use:   "add-printer",
	Short: "Interactively add a new printer to the config",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			Cfg = &Config{}
		}
		if Cfg.Printers == nil {
			Cfg.Printers = map[string]PrinterConfig{}
		}

		// Printer name
		namePrompt := promptui.Prompt{
			Label:  "Printer name",
			Stdout: NoBellStdout,
		}
		name, err := namePrompt.Run()
		if err != nil {
			return err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("printer name cannot be empty")
		}
		if _, exists := Cfg.Printers[name]; exists {
			return fmt.Errorf("printer %q already exists", name)
		}

		// Printer type
		typePrompt := promptui.Select{
			Label:  "Printer type",
			Items:  []string{"bambu", "prusa"},
			Stdout: NoBellStdout,
		}
		_, printerType, err := typePrompt.Run()
		if err != nil {
			return err
		}

		// IP address
		ipPrompt := promptui.Prompt{
			Label:  "Printer IP address",
			Stdout: NoBellStdout,
		}
		ip, err := ipPrompt.Run()
		if err != nil {
			return err
		}
		ip = strings.TrimSpace(ip)

		pCfg := PrinterConfig{
			Type: printerType,
			IP:   ip,
		}

		// Type-specific fields
		switch printerType {
		case "bambu":
			serialPrompt := promptui.Prompt{
				Label:  "Serial number (from printer LCD or slicer)",
				Stdout: NoBellStdout,
			}
			serial, err := serialPrompt.Run()
			if err != nil {
				return err
			}
			pCfg.Serial = strings.TrimSpace(serial)

			codePrompt := promptui.Prompt{
				Label:  "Access code (from printer LCD: Settings → Network)",
				Stdout: NoBellStdout,
			}
			code, err := codePrompt.Run()
			if err != nil {
				return err
			}
			pCfg.AccessCode = strings.TrimSpace(code)

		case "prusa":
			userPrompt := promptui.Prompt{
				Label:   "Username",
				Default: "maker",
				Stdout:  NoBellStdout,
			}
			username, err := userPrompt.Run()
			if err != nil {
				return err
			}
			pCfg.Username = strings.TrimSpace(username)

			passPrompt := promptui.Prompt{
				Label:  "Password",
				Stdout: NoBellStdout,
			}
			password, err := passPrompt.Run()
			if err != nil {
				return err
			}
			pCfg.Password = strings.TrimSpace(password)
		}

		// Locations
		fmt.Println("\nAdd locations for this printer (e.g. AMS A, AMS B, Prusa).")
		fmt.Println("Enter locations one at a time. Leave blank when done.")
		var locations []string
		for {
			locPrompt := promptui.Prompt{
				Label:  fmt.Sprintf("Location %d (blank to finish)", len(locations)+1),
				Stdout: NoBellStdout,
			}
			loc, err := locPrompt.Run()
			if err != nil {
				return err
			}
			loc = strings.TrimSpace(loc)
			if loc == "" {
				break
			}
			locations = append(locations, loc)
		}

		if len(locations) == 0 {
			return fmt.Errorf("at least one location is required")
		}
		pCfg.Locations = locations

		// Show summary
		fmt.Printf("\nPrinter to add:\n")
		fmt.Printf("  Name:      %s\n", name)
		fmt.Printf("  Type:      %s\n", pCfg.Type)
		fmt.Printf("  IP:        %s\n", pCfg.IP)
		switch pCfg.Type {
		case "bambu":
			fmt.Printf("  Serial:    %s\n", pCfg.Serial)
			fmt.Printf("  Access:    %s\n", pCfg.AccessCode)
		case "prusa":
			fmt.Printf("  Username:  %s\n", pCfg.Username)
			fmt.Printf("  Password:  %s\n", pCfg.Password)
		}
		fmt.Printf("  Locations: %s\n", strings.Join(pCfg.Locations, ", "))

		confirmPrompt := promptui.Select{
			Label:  "Save this printer?",
			Items:  []string{"Yes", "No"},
			Stdout: NoBellStdout,
		}
		idx, _, err := confirmPrompt.Run()
		if err != nil || idx != 0 {
			fmt.Println("Canceled.")
			return nil
		}

		// Save to config
		Cfg.Printers[name] = pCfg

		// Write shared config
		shared := Cfg.ToSharedConfig()
		data, err := json.MarshalIndent(shared, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		home, _ := os.UserHomeDir()
		configDir := filepath.Join(home, ".config", "fil")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config dir: %w", err)
		}

		configPath := filepath.Join(configDir, "shared-config.json")
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}

		fmt.Printf("\nPrinter %q added to %s\n", name, configPath)
		if Cfg.PlansServer != "" {
			fmt.Println("Run 'fil config push' to sync to the server, then restart the server.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(printerAddCmd)
}
