package mcp

import (
	"strings"
	"testing"
)

func TestTruncateStringBytes(t *testing.T) {
	got, truncated := truncateStringBytes("abcdef", 5)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if got != "ab..." {
		t.Fatalf("expected suffix inside byte cap, got %q", got)
	}
	if len(got) > 5 {
		t.Fatalf("expected output within byte cap, got %d bytes", len(got))
	}

	got, truncated = truncateStringBytes("éclair", 5)
	if !truncated {
		t.Fatal("expected multibyte truncation")
	}
	if got != "é..." {
		t.Fatalf("expected UTF-8-safe truncation, got %q", got)
	}
	if len(got) > 5 {
		t.Fatalf("expected multibyte output within byte cap, got %d bytes", len(got))
	}
}

func TestTruncateStringAroundBytesKeepsMatch(t *testing.T) {
	input := "prefix-" + strings.Repeat("x", 50) + "-needle-" + strings.Repeat("y", 50) + "-suffix"

	got, truncated := truncateStringAroundBytes(input, strings.Index(input, "needle"), len("needle"), 40)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(got) > 40 {
		t.Fatalf("expected output within byte cap, got %d bytes", len(got))
	}
	if !strings.Contains(got, "needle") {
		t.Fatalf("expected truncated output to keep match, got %q", got)
	}
	if !strings.HasPrefix(got, truncationMarker) || !strings.HasSuffix(got, truncationMarker) {
		t.Fatalf("expected markers on both sides, got %q", got)
	}
}
