// Command pocketpress is the PocketPress executable entrypoint.
//
// It is intentionally thin: configuration, logging, HTTP routing, and
// server lifecycle all live in internal/app and internal/http so they stay
// testable outside of main(). This file wires the cobra CLI root and its
// serve/version/migrate commands, plus process-level signal handling; the
// backup and user subcommands are wired in backup.go and user.go.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/pocketpress/pocketpress/internal/app"
	pphttp "github.com/pocketpress/pocketpress/internal/http"
	"github.com/pocketpress/pocketpress/internal/media"
	"github.com/pocketpress/pocketpress/internal/store"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCmd builds the "pocketpress" cobra command tree: `serve`,
// `version` and `migrate` (`up`/`down`).
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "pocketpress",
		Short:         "PocketPress — a tiny self-hosted blog backend",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newServeCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newMigrateCmd())
	root.AddCommand(newBackupCmd())
	root.AddCommand(newUserCmd())

	return root
}

// newServeCmd builds the `serve` subcommand: loads configuration, builds
// the logger, media storage driver and HTTP router, then runs the server
// until it receives SIGINT/SIGTERM.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the PocketPress HTTP server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := app.Load()
			logger := app.NewLogger(cfg)

			// Validate configuration and build the media storage driver
			// before doing anything else — in particular, before opening
			// the store or binding the HTTP listener — so a
			// misconfigured PP_STORAGE_DRIVER=s3 (missing endpoint,
			// bucket or credentials) fails the command immediately with a
			// clear error instead of surfacing later as a broken upload
			// endpoint.
			if err := cfg.Validate(); err != nil {
				return err
			}
			mediaDriver, err := media.NewDriver(media.Config{
				DataDir:          cfg.DataDir,
				StorageDriver:    cfg.StorageDriver,
				S3Endpoint:       cfg.S3Endpoint,
				S3Region:         cfg.S3Region,
				S3Bucket:         cfg.S3Bucket,
				S3AccessKey:      cfg.S3AccessKey,
				S3SecretKey:      cfg.S3SecretKey,
				S3ForcePathStyle: cfg.S3ForcePathStyle,
			})
			if err != nil {
				return err
			}

			// Resolve the preview-token secret. An unset PP_APP_SECRET is not
			// fatal yet (Phase 8 adds production fail-fast); we generate an
			// ephemeral one and warn that preview tokens won't survive a
			// restart.
			appSecret, ephemeral, err := app.ResolvePreviewSecret(cfg.AppSecret)
			if err != nil {
				return err
			}
			if ephemeral {
				logger.Warn("PP_APP_SECRET is not set; generated an ephemeral preview-token secret — preview tokens will not survive a restart")
			}

			// Resolve the webhook encryption key. PP_ENCRYPTION_KEY is accepted
			// verbatim (hashed to 32 bytes); when unset it is derived from the
			// resolved app secret, so a single PP_APP_SECRET keeps webhook
			// secrets decryptable across restarts. Warn only when that fallback
			// itself rests on an ephemeral app secret.
			encryptionKey, encEphemeral := app.ResolveEncryptionKey(cfg.EncryptionKey, appSecret, ephemeral)
			if encEphemeral {
				logger.Warn("PP_ENCRYPTION_KEY is not set and no PP_APP_SECRET is configured; encrypting webhook secrets with an ephemeral key — stored webhook secrets will not survive a restart")
			}

			// The router needs the open database handle, which app.Serve
			// creates after migrations run, so pass a factory it can call
			// once the store is ready rather than a prebuilt handler. Build
			// returns the shared content service and delivery worker so
			// app.Serve can run the scheduler and worker over them.
			build := func(db *sql.DB) app.Components {
				srv := pphttp.Build(pphttp.Config{
					Logger:         logger,
					Version:        app.Version,
					DB:             db,
					SecureCookies:  cfg.IsProduction(),
					MediaDriver:    mediaDriver,
					DataDir:        cfg.DataDir,
					MaxUploadMB:    cfg.MaxUploadMB,
					CORSOrigins:    cfg.CORSOrigins,
					AppSecret:      appSecret,
					EncryptionKey:  encryptionKey,
					Production:     cfg.IsProduction(),
					PublicURL:      cfg.PublicURL,
					TrustedProxies: cfg.TrustedProxies,
				})
				return app.Components{Handler: srv.Handler, Content: srv.Content, Worker: srv.Worker}
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return app.Serve(ctx, cfg, logger, build)
		},
	}
}

// newVersionCmd builds the `version` subcommand: prints the PocketPress
// version string.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the PocketPress version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("pocketpress version " + app.Version)
		},
	}
}

// newMigrateCmd builds the `migrate` subcommand tree: `migrate up` and
// `migrate down`.
func newMigrateCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "migrate",
		Short: "Manage the PocketPress database schema",
	}

	root.AddCommand(newMigrateUpCmd())
	root.AddCommand(newMigrateDownCmd())

	return root
}

// newMigrateUpCmd builds the `migrate up` subcommand: opens the store at
// the configured data directory and applies every pending migration,
// printing the version of each one applied.
func newMigrateUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending database migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := app.Load()

			st, err := store.Open(cfg.DataDir)
			if err != nil {
				return err
			}
			defer st.Close()

			applied, err := store.Up(cmd.Context(), st.DB())
			if err != nil {
				return err
			}

			if len(applied) == 0 {
				cmd.Println("Already up to date.")
				return nil
			}
			for _, version := range applied {
				cmd.Println("applied:", version)
			}
			return nil
		},
	}
}

// newMigrateDownCmd builds the `migrate down` subcommand: opens the store
// at the configured data directory and reverts exactly one migration, the
// most recently applied.
func newMigrateDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Revert the most recently applied database migration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := app.Load()

			st, err := store.Open(cfg.DataDir)
			if err != nil {
				return err
			}
			defer st.Close()

			reverted, err := store.Down(cmd.Context(), st.DB())
			if err != nil {
				return err
			}

			if reverted == "" {
				cmd.Println("Nothing to revert.")
				return nil
			}
			cmd.Println("reverted:", reverted)
			return nil
		},
	}
}
