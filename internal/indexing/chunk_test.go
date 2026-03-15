package indexing

import "testing"

func TestChunkFile_GoFixtureAdjacentDeclsWithDocComments(t *testing.T) {
	content := `package fixture

// AddOne returns a stable value.
func AddOne() int {
	return 1
}
// AddTwo returns another stable value.
func AddTwo() int {
	return 2
}
`

	chunks := ChunkFile("fixture.go", "go", content, 4, 1<<20)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	assertChunkBounds(t, chunks[0], 1, 4)
	assertChunkBounds(t, chunks[1], 5, 8)
	assertChunkBounds(t, chunks[2], 9, 10)

	if chunks[0].Path != "fixture.go" || chunks[0].Language != "go" {
		t.Fatalf("unexpected chunk metadata: path=%q language=%q", chunks[0].Path, chunks[0].Language)
	}
	if chunks[0].Content != "package fixture\n\n// AddOne returns a stable value.\nfunc AddOne() int {\n" {
		t.Fatalf("unexpected first chunk content: %q", chunks[0].Content)
	}
	if chunks[1].Content != "\treturn 1\n}\n// AddTwo returns another stable value.\nfunc AddTwo() int {\n" {
		t.Fatalf("unexpected second chunk content: %q", chunks[1].Content)
	}
	if chunks[2].Content != "\treturn 2\n}\n" {
		t.Fatalf("unexpected third chunk content: %q", chunks[2].Content)
	}
}

func TestChunkFile_NonGoFallbackRemainsLineBased(t *testing.T) {
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"

	chunks := ChunkFile("fixture.txt", "text", content, 2, 1<<20)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	assertChunkBounds(t, chunks[0], 1, 2)
	assertChunkBounds(t, chunks[1], 3, 4)
	assertChunkBounds(t, chunks[2], 5, 5)

	if chunks[0].Content != "line 1\nline 2\n" {
		t.Fatalf("unexpected first fallback chunk content: %q", chunks[0].Content)
	}
	if chunks[1].Content != "line 3\nline 4\n" {
		t.Fatalf("unexpected second fallback chunk content: %q", chunks[1].Content)
	}
	if chunks[2].Content != "line 5\n" {
		t.Fatalf("unexpected third fallback chunk content: %q", chunks[2].Content)
	}
}

func assertChunkBounds(t *testing.T, ch Chunk, wantStart, wantEnd int) {
	t.Helper()
	if ch.StartLine != wantStart || ch.EndLine != wantEnd {
		t.Fatalf("unexpected chunk bounds: got [%d,%d], want [%d,%d]", ch.StartLine, ch.EndLine, wantStart, wantEnd)
	}
}