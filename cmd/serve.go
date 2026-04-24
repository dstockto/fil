package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
			PlansDir:        Cfg.PlansDir,
			PauseDir:        Cfg.PauseDir,
			ArchiveDir:      Cfg.ArchiveDir,
			ConfigDir:       configDir,
			AssembliesDir:   assembliesDir,
			Version:         version,
			ApiBase:         Cfg.ApiBase,
			ApiBaseInternal: Cfg.ApiBaseInternal,
			TLSSkipVerify:   Cfg.TLSSkipVerify,
			StartedAt:       time.Now(),
		}

		// Start printer connections
		if len(Cfg.Printers) > 0 {
			pm := server.NewPrinterManager()
			s.Printers = pm
			defer pm.Close()

			for name, pCfg := range Cfg.Printers {
				if pCfg.Type == "" || pCfg.IP == "" {
					continue
				}

				var adapter server.PrinterAdapter
				switch pCfg.Type {
				case "bambu":
					adapter = server.NewBambuAdapter(name, pCfg.IP, pCfg.Serial, pCfg.AccessCode)
				case "prusa":
					adapter = server.NewPrusaAdapter(name, pCfg.IP, pCfg.Username, pCfg.Password)
				default:
					fmt.Printf("  Printer %s: unknown type %q, skipping\n", name, pCfg.Type)
					continue
				}

				if err := pm.AddAdapter(name, adapter); err != nil {
					fmt.Printf("  Printer %s: connection failed: %v\n", name, err)
				} else {
					fmt.Printf("  Printer %s: connected (%s)\n", name, pCfg.Type)
				}
			}
		}

		// Build set of live-connected printer names for the ETA watcher
		livePrinters := make(map[string]bool)
		if s.Printers != nil {
			for name := range Cfg.Printers {
				if _, ok := s.Printers.Adapter(name); ok {
					livePrinters[name] = true
				}
			}
		}

		// Start ETA notification watcher if notifications are configured
		if Cfg.Notifications != nil {
			notifyCfg := server.NotificationConfig{
				PushoverAPIKey:    Cfg.Notifications.PushoverAPIKey,
				PushoverUserKey:   Cfg.Notifications.PushoverUserKey,
				NtfyTopic:         Cfg.Notifications.NtfyTopic,
				NtfyServer:        Cfg.Notifications.NtfyServer,
				VoiceMonkeyToken:  Cfg.Notifications.VoiceMonkeyToken,
				VoiceMonkeyDevice: Cfg.Notifications.VoiceMonkeyDevice,
				QuietStart:        Cfg.Notifications.QuietStart,
				QuietEnd:          Cfg.Notifications.QuietEnd,
			}
			notifier := server.NewNotifier(notifyCfg)
			s.Notifier = notifier
			if notifier.Enabled() {
				watcher := server.NewETAWatcher(ctx, Cfg.PlansDir, notifier, livePrinters)
				s.Watcher = watcher
				defer watcher.Stop()
				fmt.Println("  Notifications: enabled")

				// Wire up printer state change notifications
				if s.Printers != nil {
					plansDir := Cfg.PlansDir
					for name := range Cfg.Printers {
						adapter, ok := s.Printers.Adapter(name)
						if !ok {
							continue
						}
						printerName := name
						adapter.OnStateChange(func(event server.StateChangeEvent) {
							// Look up what's printing on this printer
							projName, plateName := server.LookupInProgressPlate(plansDir, printerName)
							plateInfo := ""
							if projName != "" && plateName != "" {
								plateInfo = fmt.Sprintf("%s / %s", projName, plateName)
							}

							var title, msg, speech string
							switch event.NewState {
							case "finished":
								title = "Print finished"
								if plateInfo != "" {
									msg = fmt.Sprintf("%s: %s — print finished", printerName, plateInfo)
									speech = fmt.Sprintf("%s finished %s", printerName, plateInfo)
								} else {
									msg = fmt.Sprintf("%s: print finished", printerName)
									speech = fmt.Sprintf("%s finished a print", printerName)
								}
							case "paused":
								isUpdate := event.OldState == "paused"
								if event.IsLikelyUserPause() {
									title = "Print paused (user)"
									if plateInfo != "" {
										msg = fmt.Sprintf("%s: %s — paused by user", printerName, plateInfo)
									} else {
										msg = fmt.Sprintf("%s: paused by user", printerName)
									}
								} else if isUpdate {
									title = "Additional printer fault"
									if plateInfo != "" {
										msg = fmt.Sprintf("%s: %s — additional fault detected", printerName, plateInfo)
									} else {
										msg = fmt.Sprintf("%s: additional fault detected", printerName)
									}
								} else {
									title = "Print paused (printer)"
									if plateInfo != "" {
										msg = fmt.Sprintf("%s: %s — paused by printer, check it", printerName, plateInfo)
									} else {
										msg = fmt.Sprintf("%s: paused by printer, check it", printerName)
									}
									speech = fmt.Sprintf("%s paused, check the printer", printerName)
								}
								// Log HMS codes and include description in notification
								if len(event.HMSCodes) > 0 {
									var codes []string
									var reasons []string
									for _, h := range event.HMSCodes {
										codes = append(codes, h.HMSCodeString())
										if desc := h.HMSDescription(); desc != "" {
											reasons = append(reasons, desc)
										} else {
											reasons = append(reasons, h.HMSCodeString())
										}
									}
									fmt.Printf("[notify] %s paused — HMS: %s\n", printerName, strings.Join(codes, ", "))
									if len(reasons) > 0 {
										msg += "\n" + strings.Join(reasons, ", ")
									}
								}
							case "failed":
								title = "Print failed"
								if plateInfo != "" {
									msg = fmt.Sprintf("%s: %s — print failed", printerName, plateInfo)
									speech = fmt.Sprintf("%s failed while printing %s", printerName, plateInfo)
								} else {
									msg = fmt.Sprintf("%s: print failed", printerName)
									speech = fmt.Sprintf("A print failed on %s", printerName)
								}
								if len(event.HMSCodes) > 0 {
									var codes []string
									var reasons []string
									for _, h := range event.HMSCodes {
										codes = append(codes, h.HMSCodeString())
										if desc := h.HMSDescription(); desc != "" {
											reasons = append(reasons, desc)
										} else {
											reasons = append(reasons, h.HMSCodeString())
										}
									}
									fmt.Printf("[notify] %s failed — HMS: %s\n", printerName, strings.Join(codes, ", "))
									if len(reasons) > 0 {
										msg += "\n" + strings.Join(reasons, ", ")
									}
								}
							default:
								return
							}
							if notifier.IsQuietHours(time.Now()) {
								return
							}
							notifier.Send(title, msg)
							if speech != "" {
								if err := notifier.Speak(speech); err != nil {
									fmt.Printf("[notify] voice monkey: %v\n", err)
								}
							}
						})
					}
				}
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
