package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/pocketpress/pocketpress/internal/app"
	"github.com/pocketpress/pocketpress/internal/backup"
)

// newBackupCmd builds the `backup` subcommand tree: `backup create`,
// `backup list` and `backup restore`.
func newBackupCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "backup",
		Short: "Create, list and restore PocketPress backups",
	}

	root.AddCommand(newBackupCreateCmd())
	root.AddCommand(newBackupListCmd())
	root.AddCommand(newBackupRestoreCmd())

	return root
}

// newBackupCreateCmd builds the `backup create` subcommand: it vacuums a
// safe live copy of the database, bundles the local uploads directory and a
// manifest into a zip archive, and prints the resulting path and size.
func newBackupCreateCmd() *cobra.Command {
	var out string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a backup archive of the database and local uploads",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := app.Load()

			result, err := backup.Create(cmd.Context(), backup.CreateOptions{
				DataDir:       cfg.DataDir,
				Out:           out,
				Version:       app.Version,
				StorageDriver: cfg.StorageDriver,
			})
			if err != nil {
				return err
			}

			cmd.Printf("Backup created: %s (%s)\n", result.Path, formatByteSize(result.Size))
			return nil
		},
	}

	cmd.Flags().StringVar(&out, "out", "", "Output directory or .zip file path (default: <PP_DATA_DIR>/backups/)")
	return cmd
}

// newBackupListCmd builds the `backup list` subcommand: a table of the
// archives found in <PP_DATA_DIR>/backups, newest first.
func newBackupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available backup archives",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := app.Load()

			entries, err := backup.List(cfg.DataDir)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				cmd.Println("No backups found in", filepath.Join(cfg.DataDir, backup.BackupsDirName))
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSIZE\tCREATED")
			for _, e := range entries {
				fmt.Fprintf(w, "%s\t%s\t%s\n", e.Name, formatByteSize(e.Size), e.CreatedAt.Format(time.RFC3339))
			}
			return w.Flush()
		},
	}
}

// newBackupRestoreCmd builds the `backup restore <file>` subcommand: it
// validates and extracts an archive into PP_DATA_DIR, refusing to overwrite
// an existing database unless --force is given.
func newBackupRestoreCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "restore <file>",
		Short: "Restore a backup archive into PP_DATA_DIR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := app.Load()

			result, err := backup.Restore(backup.RestoreOptions{
				DataDir:     cfg.DataDir,
				ArchivePath: args[0],
				Force:       force,
			})
			if errors.Is(err, backup.ErrExistingData) {
				return fmt.Errorf("%w — re-run with --force to move the current database and uploads aside under pre-restore-<timestamp>/ and restore over them", err)
			}
			if err != nil {
				return err
			}

			cmd.Printf("Restored %s into %s\n", args[0], cfg.DataDir)
			cmd.Printf("  uploaded files restored: %d\n", result.UploadFiles)
			if result.PreRestoreDir != "" {
				cmd.Printf("  previous database and uploads preserved at: %s\n", result.PreRestoreDir)
			}
			cmd.Printf("  archive created by pocketpress %s at %s\n", result.Manifest.PocketPressVersion, result.Manifest.CreatedAt)
			cmd.Println("The server must be stopped before restoring and (re)started afterwards to use the restored data.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing database, preserving the current one under pre-restore-<timestamp>/")
	return cmd
}

// formatByteSize renders n bytes as a human-readable size (e.g. "12.3 MiB"),
// following the same binary-prefix convention as most *nix tools.
func formatByteSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
