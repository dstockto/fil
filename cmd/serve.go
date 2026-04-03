package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/dstockto/fil/server"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the plan storage HTTP server",
	Long:  `Start a lightweight HTTP server for centralized plan storage. Run this on the same machine as Spoolman so plans are accessible from any client.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansDir == "" {
			return fmt.Errorf("plans_dir must be configured in config.json")
		}

		port, _ := cmd.Flags().GetInt("port")
		bind, _ := cmd.Flags().GetString("bind")

		// Determine config directory for shared config storage
		configDir := Cfg.SharedConfigDir
		if configDir == "" {
			if home, _ := os.UserHomeDir(); home != "" {
				configDir = filepath.Join(home, ".config", "fil")
			}
		}

		// Determine assemblies directory
		assembliesDir := Cfg.AssembliesDir
		if assembliesDir == "" {
			assembliesDir = filepath.Join(Cfg.PlansDir, "..", "assemblies")
		}

		// Ensure directories exist
		for _, dir := range []string{Cfg.PlansDir, Cfg.PauseDir, Cfg.ArchiveDir, configDir, assembliesDir} {
			if dir == "" {
				continue
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}

		// Graceful shutdown on SIGINT/SIGTERM
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		s := &server.PlanServer{
			PlansDir:      Cfg.PlansDir,
			PauseDir:      Cfg.PauseDir,
			ArchiveDir:    Cfg.ArchiveDir,
			ConfigDir:     configDir,
			AssembliesDir: assembliesDir,
			Version:       version,
		}

		// Start ETA notification watcher if notifications are configured
		if Cfg.Notifications != nil {
			notifyCfg := server.NotificationConfig{
				PushoverAPIKey:  Cfg.Notifications.PushoverAPIKey,
				PushoverUserKey: Cfg.Notifications.PushoverUserKey,
				NtfyTopic:       Cfg.Notifications.NtfyTopic,
				NtfyServer:      Cfg.Notifications.NtfyServer,
				QuietStart:      Cfg.Notifications.QuietStart,
				QuietEnd:        Cfg.Notifications.QuietEnd,
			}
			notifier := server.NewNotifier(notifyCfg)
			if notifier.Enabled() {
				watcher := server.NewETAWatcher(ctx, Cfg.PlansDir, notifier)
				s.Watcher = watcher
				defer watcher.Stop()
				fmt.Println("  Notifications: enabled")
			}
		} else {
			fmt.Println("  Notifications: disabled")
		}

		addr := fmt.Sprintf("%s:%d", bind, port)
		srv := &http.Server{
			Addr:    addr,
			Handler: s.Routes(),
		}

		go func() {
			<-ctx.Done()
			fmt.Println("\nShutting down server...")
			_ = srv.Shutdown(context.Background())
		}()

		fmt.Printf("Plan server listening on %s\n", addr)
		fmt.Printf("  Plans dir:   %s\n", Cfg.PlansDir)
		if Cfg.PauseDir != "" {
			fmt.Printf("  Pause dir:   %s\n", Cfg.PauseDir)
		}
		if Cfg.ArchiveDir != "" {
			fmt.Printf("  Archive dir: %s\n", Cfg.ArchiveDir)
		}
		if configDir != "" {
			fmt.Printf("  Config dir:  %s\n", configDir)
		}
		if assembliesDir != "" {
			fmt.Printf("  Assemblies:  %s\n", assembliesDir)
		}

		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}

		return nil
	},
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().Int("port", 7654, "port to listen on")
	serveCmd.Flags().String("bind", "0.0.0.0", "address to bind to")
}
