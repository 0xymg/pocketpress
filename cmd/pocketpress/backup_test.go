package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBackupListEmptyCLI confirms `backup list` against a data directory
// with no backups yet prints a friendly message and succeeds, rather than
// erroring or printing an empty table.
func TestBackupListEmptyCLI(t *testing.T) {
	dataDir := t.TempDir()

	stdout, _, err := runCLI(t, dataDir, []string{"backup", "list"}, nil)
	if err != nil {
		t.Fatalf("backup list error = %v", err)
	}
	if !strings.Contains(stdout, "No backups found") {
		t.Errorf("stdout = %q, want a friendly empty message", stdout)
	}
}

// TestBackupCreateAndListCLI wires `backup create` and `backup list`
// together: create writes an archive under the default
// <PP_DATA_DIR>/backups/ location and reports its path, and list then finds
// it.
func TestBackupCreateAndListCLI(t *testing.T) {
	dataDir := t.TempDir()
	migrateFreshStore(t, dataDir)

	createOut, _, err := runCLI(t, dataDir, []string{"backup", "create"}, nil)
	if err != nil {
		t.Fatalf("backup create error = %v", err)
	}
	if !strings.Contains(createOut, "Backup created:") {
		t.Errorf("backup create stdout = %q, want it to report the created archive", createOut)
	}
	if !strings.Contains(createOut, filepath.Join(dataDir, "backups")) {
		t.Errorf("backup create stdout = %q, want it under the default backups directory", createOut)
	}

	listOut, _, err := runCLI(t, dataDir, []string{"backup", "list"}, nil)
	if err != nil {
		t.Fatalf("backup list error = %v", err)
	}
	if !strings.Contains(listOut, "NAME") || !strings.Contains(listOut, ".zip") {
		t.Errorf("backup list stdout = %q, want a table listing the archive", listOut)
	}
}

// TestBackupRestoreCLI covers the restore safety gate through the CLI:
// restoring onto a data directory that already has a database is refused
// without --force, and succeeds with it, printing a summary that mentions
// the previous data was preserved.
func TestBackupRestoreCLI(t *testing.T) {
	// Build one archive from a throwaway, already-migrated source instance.
	sourceDir := t.TempDir()
	migrateFreshStore(t, sourceDir)

	archivePath := filepath.Join(t.TempDir(), "test-backup.zip")
	if _, _, err := runCLI(t, sourceDir, []string{"backup", "create", "--out=" + archivePath}, nil); err != nil {
		t.Fatalf("backup create error = %v", err)
	}

	// The restore target already has a database (from its own migration),
	// so a plain restore must be refused.
	targetDir := t.TempDir()
	migrateFreshStore(t, targetDir)

	_, _, err := runCLI(t, targetDir, []string{"backup", "restore", archivePath}, nil)
	if err == nil {
		t.Fatal("backup restore onto an existing database succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %q, want it to mention --force", err.Error())
	}

	restoreOut, _, err := runCLI(t, targetDir, []string{"backup", "restore", archivePath, "--force"}, nil)
	if err != nil {
		t.Fatalf("backup restore --force error = %v", err)
	}
	if !strings.Contains(restoreOut, "Restored") {
		t.Errorf("restore stdout = %q, want a summary", restoreOut)
	}
	if !strings.Contains(restoreOut, "preserved at") {
		t.Errorf("restore stdout = %q, want it to mention the preserved pre-restore data", restoreOut)
	}
	if !strings.Contains(restoreOut, "stopped before restoring") {
		t.Errorf("restore stdout = %q, want the server-restart reminder", restoreOut)
	}
}
