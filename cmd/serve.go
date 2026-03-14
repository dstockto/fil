package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
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

		// Ensure directories exist
		for _, dir := range []string{Cfg.PlansDir, Cfg.PauseDir, Cfg.ArchiveDir} {
			if dir == "" {
				continue
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}

		s := &server.PlanServer{
			PlansDir:   Cfg.PlansDir,
			PauseDir:   Cfg.PauseDir,
			ArchiveDir: Cfg.ArchiveDir,
		}

		addr := fmt.Sprintf("%s:%d", bind, port)
		srv := &http.Server{
			Addr:    addr,
			Handler: s.Routes(),
		}

		// Graceful shutdown on SIGINT/SIGTERM
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

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
