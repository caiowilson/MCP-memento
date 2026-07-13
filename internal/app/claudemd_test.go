package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeMDMarkerLiterals(t *testing.T) {
	const wantStart = "<!-- memento-mcp:recommended-workflow:start -->"
	const wantEnd = "<!-- memento-mcp:recommended-workflow:end -->"

	if claudeMDMarkerStart != wantStart {
		t.Errorf("claudeMDMarkerStart = %q, want %q", claudeMDMarkerStart, wantStart)
	}
	if claudeMDMarkerEnd != wantEnd {
		t.Errorf("claudeMDMarkerEnd = %q, want %q", claudeMDMarkerEnd, wantEnd)
	}
}

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
			got, err := upsertClaudeLocalMD([]byte(tt.existing), tt.block)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("upsertClaudeLocalMD() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

// TestUpsertClaudeLocalMDUnpairedMarkers covers Finding 1: markers found
// independently of each other (rather than the end marker being searched for
// only after the start marker) can silently drop user content or duplicate
// the block forever. Both cases must now be rejected with an error instead
// of writing anything.
func TestUpsertClaudeLocalMDUnpairedMarkers(t *testing.T) {
	block := claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n"

	tests := []struct {
		name     string
		existing string
	}{
		{
			name:     "lone start marker with content after it",
			existing: claudeMDMarkerStart + "\nSome user notes\n",
		},
		{
			name:     "lone end marker with no start marker at all",
			existing: "Some user notes\n" + claudeMDMarkerEnd + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := upsertClaudeLocalMD([]byte(tt.existing), block)
			if err == nil {
				t.Fatalf("expected error for unpaired marker, got nil (result: %q)", string(got))
			}
			if got != nil {
				t.Errorf("expected nil result on error, got %q", string(got))
			}
		})
	}
}

func TestRunClaudeMDPrintOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

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
	for _, want := range []string{"without `paths`", "staged, unstaged, and untracked", "bounded, redacted unified diff summary", "never chunk-loaded"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected generated guidance to contain %q, got %q", want, stdout.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.local.md")); !os.IsNotExist(err) {
		t.Fatalf("expected --print-only to write nothing, stat err = %v", err)
	}
}

func TestRunClaudeMDWritesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

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
	t.Chdir(dir)

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

// TestRunClaudeMDUnpairedMarkerLeavesFileUnchanged covers Finding 1 at the
// CLI level: when CLAUDE.local.md already has a lone start marker (no
// matching end marker), runClaudeMD must fail loudly instead of silently
// eating the user's content, and must not touch the file at all.
func TestRunClaudeMDUnpairedMarkerLeavesFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	original := claudeMDMarkerStart + "\nSome user notes with no end marker\n"
	path := filepath.Join(dir, "CLAUDE.local.md")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := runClaudeMD(nil, &stdout, &stderr); err == nil {
		t.Fatal("expected error for unpaired marker, got nil")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to still exist: %v", err)
	}
	if string(got) != original {
		t.Fatalf("file contents changed on error: got %q, want unchanged %q", string(got), original)
	}
}
