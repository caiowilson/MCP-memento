//go:build darwin || linux

package mcp

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOpenDiffRegularFileRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "pipe.go"), 0o600); err != nil {
		t.Skipf("FIFO unavailable: %v", err)
	}
	file, err := openDiffRegularFile(root, "pipe.go")
	if file != nil {
		_ = file.Close()
		t.Fatal("FIFO unexpectedly opened as a regular file")
	}
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("FIFO error = %v", err)
	}
}
