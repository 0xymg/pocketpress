package main

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// runCLI executes the pocketpress root command with args against a
// PP_DATA_DIR pinned to dataDir (via t.Setenv, so it is automatically
// restored and never touches a real data directory), returning everything
// written to stdout/stderr and the command's error, if any.
func runCLI(t *testing.T, dataDir string, args []string, stdin io.Reader) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("PP_DATA_DIR", dataDir)

	root := newRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	if stdin != nil {
		root.SetIn(stdin)
	}
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// migrateFreshStore opens (creating) and migrates a store at dataDir, then
// closes it, so a backup/user command under test has a schema to work
// against without going through the CLI's own implicit migration step.
func migrateFreshStore(t *testing.T, dataDir string) {
	t.Helper()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}
}
