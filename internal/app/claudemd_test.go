package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertClaudeLocalMD(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		block    string
		want     string
	}{
		{
			name:     "creates block when file is empty",
			existing: "",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "appends block with blank line separator when no markers present",
			existing: "Some content\n",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "replaces block in place when markers already present",
			existing: "Some content\n\n" + claudeMDMarkerStart + "\nOLD BLOCK\n" + claudeMDMarkerEnd + "\n",
			block:    claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "rerunning with the same block is idempotent",
			existing: "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "preserves content after the end marker",
			existing: claudeMDMarkerStart + "\nOLD BLOCK\n" + claudeMDMarkerEnd + "\nSome trailing note\n",
			block:    claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\nSome trailing note\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := upsertClaudeLocalMD([]byte(tt.existing), tt.block)
			if string(got) != tt.want {
				t.Errorf("upsertClaudeLocalMD() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestRunClaudeMDPrintOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runClaudeMD([]string{"--print-only"}, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if stdout.String() != recommendedWorkflowBlock {
		t.Fatalf("stdout = %q, want %q", stdout.String(), recommendedWorkflowBlock)
	}
}

func TestRunClaudeMDWritesFile(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	var stdout, stderr bytes.Buffer
	if err := runClaudeMD(nil, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md"))
	if err != nil {
		t.Fatalf("expected file to be written: %v", err)
	}
	if string(got) != recommendedWorkflowBlock {
		t.Fatalf("file contents = %q, want %q", string(got), recommendedWorkflowBlock)
	}
	if !strings.Contains(stdout.String(), "CLAUDE.local.md") {
		t.Fatalf("expected confirmation message, got %q", stdout.String())
	}
}

func TestRunClaudeMDIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	var stdout, stderr bytes.Buffer
	if err := runClaudeMD(nil, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error on first run: %v", err)
	}
	stdout.Reset()
	if err := runClaudeMD(nil, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != recommendedWorkflowBlock {
		t.Fatalf("file contents after rerun = %q, want %q (no duplication)", string(got), recommendedWorkflowBlock)
	}
}
