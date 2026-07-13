package indexing

import (
	"reflect"
	"strings"
	"testing"
)

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

	assertChunkBounds(t, chunks[0], 1, 2)
	assertChunkBounds(t, chunks[1], 3, 6)
	assertChunkBounds(t, chunks[2], 7, 10)

	if chunks[0].Path != "fixture.go" || chunks[0].Language != "go" {
		t.Fatalf("unexpected chunk metadata: path=%q language=%q", chunks[0].Path, chunks[0].Language)
	}
	if chunks[0].Content != "package fixture\n" {
		t.Fatalf("unexpected first chunk content: %q", chunks[0].Content)
	}
	if chunks[1].Content != "// AddOne returns a stable value.\nfunc AddOne() int {\n\treturn 1\n}\n" {
		t.Fatalf("unexpected second chunk content: %q", chunks[1].Content)
	}
	if chunks[2].Content != "// AddTwo returns another stable value.\nfunc AddTwo() int {\n\treturn 2\n}\n" {
		t.Fatalf("unexpected third chunk content: %q", chunks[2].Content)
	}
}

func TestChunkFile_GoGreedilyPacksWholeDeclarations(t *testing.T) {
	content := `package fixture

func One() {
	println("one")
}

func Two() {
	println("two")
}
`
	chunks := ChunkFile("fixture.go", "go", content, 6, 1<<20)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %#v", chunks)
	}
	assertChunkBounds(t, chunks[0], 1, 6)
	assertChunkBounds(t, chunks[1], 7, 9)
}

func TestChunkFile_GoOversizedDeclarationFallsBackLocally(t *testing.T) {
	content := `package fixture

func Large() {
	println(1)
	println(2)
	println(3)
	println(4)
}
func Small() {}
`
	chunks := ChunkFile("fixture.go", "go", content, 4, 1<<20)
	want := [][2]int{{1, 2}, {3, 6}, {7, 8}, {9, 9}}
	if len(chunks) != len(want) {
		t.Fatalf("expected %d chunks, got %#v", len(want), chunks)
	}
	for index, bounds := range want {
		assertChunkBounds(t, chunks[index], bounds[0], bounds[1])
	}
}

func TestChunkFile_GoUsesPhysicalLinesWithLineDirectives(t *testing.T) {
	content := "package fixture\n\n//line generated.go:1\nfunc One() {}\nfunc Two() {}\n"
	chunks := ChunkFile("fixture.go", "go", content, 2, 1<<20)
	want := [][2]int{{1, 2}, {3, 4}, {5, 5}}
	if len(chunks) != len(want) {
		t.Fatalf("expected physical declaration ranges, got %#v", chunks)
	}
	for index, bounds := range want {
		assertChunkBounds(t, chunks[index], bounds[0], bounds[1])
	}
}

func TestChunkFile_InvalidGoMatchesLineFallback(t *testing.T) {
	content := "package fixture\nfunc Broken( {\nline 3\nline 4\nline 5\n"
	got := ChunkFile("fixture.go", "go", content, 2, 1<<20)
	want := ChunkFile("fixture.txt", "text", content, 2, 1<<20)
	for index := range want {
		want[index].Path = "fixture.go"
		want[index].Language = "go"
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid Go did not preserve fallback:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestChunkFile_UsesOriginalSyntaxForRedactedGo(t *testing.T) {
	original := `package fixture

// Build returns a token.
func Build() string {
	token := jwt.New()
	return token
}
func Next() {}
`
	redacted := strings.Replace(original, "jwt.New()", "[REDACTED]", 1)
	chunks := chunkFileWithSyntaxSource("fixture.go", "go", redacted, original, 5, 1<<20)
	if len(chunks) != 3 {
		t.Fatalf("expected structural chunks from original syntax, got %#v", chunks)
	}
	assertChunkBounds(t, chunks[0], 1, 2)
	assertChunkBounds(t, chunks[1], 3, 7)
	assertChunkBounds(t, chunks[2], 8, 8)
	if !strings.Contains(chunks[1].Content, "[REDACTED]") || strings.Contains(chunks[1].Content, "jwt.New()") {
		t.Fatalf("chunk did not preserve redaction: %q", chunks[1].Content)
	}
}

func TestChunkFile_JSAlignsTopLevelDeclarations(t *testing.T) {
	content := `import { value } from "./value";

/** First docs. */
export function first() {
	const nested = value;
}
// Worker docs.
export class Worker {
	run() {
		return true;
	}
}
export const arrow = () => {
	return 1;
};
`
	chunks := ChunkFile("fixture.ts", "ts/js", content, 6, 1<<20)
	want := [][2]int{{1, 6}, {7, 12}, {13, 15}}
	if len(chunks) != len(want) {
		t.Fatalf("expected declaration-aligned chunks, got %#v", chunks)
	}
	for index, bounds := range want {
		assertChunkBounds(t, chunks[index], bounds[0], bounds[1])
	}
	if !strings.HasPrefix(chunks[1].Content, "// Worker docs.") {
		t.Fatalf("expected class documentation to stay attached: %q", chunks[1].Content)
	}
}

func TestChunkFile_JSIgnoresNestedAndRegexBoundaries(t *testing.T) {
	content := `export function outer() {
	const nested = () => {
		return "export class Fake {}";
	};
}
const pattern = /[{}]/;
export class Real {}
`
	chunks := ChunkFile("fixture.js", "ts/js", content, 5, 1<<20)
	if len(chunks) != 2 {
		t.Fatalf("expected nested declarations to remain inside outer chunk, got %#v", chunks)
	}
	assertChunkBounds(t, chunks[0], 1, 5)
	assertChunkBounds(t, chunks[1], 6, 7)
}

func TestChunkFile_JSIgnoresDeclarationsInsideCommentsAndTemplates(t *testing.T) {
	content := "/*\nexport class CommentFake {}\n*/\nconst template = `\nexport function TemplateFake() {}\n`;\nexport class Real {}\n"
	chunks := ChunkFile("fixture.js", "ts/js", content, 3, 1<<20)
	want := [][2]int{{1, 3}, {4, 6}, {7, 7}}
	if len(chunks) != len(want) {
		t.Fatalf("masked declarations created false boundaries: %#v", chunks)
	}
	for index, bounds := range want {
		assertChunkBounds(t, chunks[index], bounds[0], bounds[1])
	}
}

func TestChunkFile_JSKeepsModifiersDecoratorsAndDocumentationAttached(t *testing.T) {
	content := `/** Worker docs. */
@sealed({
	enabled: true,
})
export default
class Worker {}
export const next = 1;
`
	lines := splitChunkLines(content)
	starts, ok := jsChunkStarts([]byte(content), lines)
	if !ok || !reflect.DeepEqual(starts, []int{1, 7}) {
		t.Fatalf("declaration starts = %v, ok=%t, want [1 7]", starts, ok)
	}
	chunks := ChunkFile("fixture.ts", "ts/js", content, 6, 1<<20)
	if len(chunks) != 2 {
		t.Fatalf("expected decorated declaration and following export, got %#v", chunks)
	}
	assertChunkBounds(t, chunks[0], 1, 6)
	assertChunkBounds(t, chunks[1], 7, 7)
}

func TestChunkFile_JSFoldsInterleavedPrefixes(t *testing.T) {
	fixtures := []string{
		"export default\n/** docs */\nclass Worker {}\nexport const next = 1;\n",
		"@sealed\n/** docs */\nexport class Worker {}\nexport const next = 1;\n",
	}
	for _, content := range fixtures {
		starts, ok := jsChunkStarts([]byte(content), splitChunkLines(content))
		if !ok || !reflect.DeepEqual(starts, []int{1, 4}) {
			t.Fatalf("interleaved prefix starts = %v, ok=%t for %q", starts, ok, content)
		}
	}
}

func TestChunkFile_JSTrailingBlockCommentDoesNotAbsorbPriorDeclaration(t *testing.T) {
	content := `export function previous() {} /** docs
 * for next
 */
export function next() {}
`
	starts, ok := jsChunkStarts([]byte(content), splitChunkLines(content))
	if !ok || !reflect.DeepEqual(starts, []int{1, 4}) {
		t.Fatalf("trailing block comment changed declaration starts = %v, ok=%t", starts, ok)
	}
}

func TestChunkFile_JSHandlesRegexQuotesCommentsAndArrowReturns(t *testing.T) {
	content := `const quotePattern = /['"]/;
const markerPattern = /[/*]/;
export const arrowPattern = () => /}/;
export class Real {}
`
	starts, ok := jsChunkStarts([]byte(content), splitChunkLines(content))
	if !ok || !reflect.DeepEqual(starts, []int{1, 2, 3, 4}) {
		t.Fatalf("regex-bearing declaration starts = %v, ok=%t", starts, ok)
	}
}

func TestChunkFile_TSXUsesSyntaxBoundaries(t *testing.T) {
	content := "export function First() {\n\treturn <div>don't split JSX</div>;\n}\nexport function Second() {}\n"
	got := ChunkFile("fixture.tsx", "ts/js", content, 2, 1<<20)
	want := [][2]int{{1, 2}, {3, 3}, {4, 4}}
	if len(got) != len(want) {
		t.Fatalf("expected TSX declaration boundaries, got %#v", got)
	}
	for index, bounds := range want {
		assertChunkBounds(t, got[index], bounds[0], bounds[1])
	}
}

func TestChunkFile_PythonAndRustUseSyntaxBoundaries(t *testing.T) {
	tests := []struct {
		path     string
		language string
		content  string
		want     [][2]int
	}{
		{
			path:     "fixture.py",
			language: "python",
			content:  "import os\n\n# First docs.\ndef first():\n    return os.getcwd()\n\nclass Worker:\n    def run(self):\n        return True\n",
			want:     [][2]int{{1, 2}, {3, 6}, {7, 9}},
		},
		{
			path:     "fixture.rs",
			language: "rust",
			content:  "use std::fmt;\n\n/// Worker docs.\npub struct Worker {\n    value: usize,\n}\n\nimpl Worker {\n    pub fn run(&self) {}\n}\n",
			want:     [][2]int{{1, 2}, {3, 7}, {8, 10}},
		},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			chunks := ChunkFile(test.path, test.language, test.content, 5, 1<<20)
			if len(chunks) != len(test.want) {
				t.Fatalf("expected %d chunks, got %#v", len(test.want), chunks)
			}
			for index, bounds := range test.want {
				assertChunkBounds(t, chunks[index], bounds[0], bounds[1])
			}
		})
	}
}

func TestChunkFile_MalformedTreeSitterLanguagesMatchLineFallback(t *testing.T) {
	tests := []struct {
		path     string
		language string
		content  string
	}{
		{path: "fixture.tsx", language: "ts/js", content: "export function broken( {\nreturn <div>;\nline 3\n"},
		{path: "fixture.py", language: "python", content: "def broken(:\nline 2\nline 3\n"},
		{path: "fixture.rs", language: "rust", content: "pub fn broken( {\nline 2\nline 3\n"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			got := ChunkFile(test.path, test.language, test.content, 2, 1<<20)
			want := ChunkFile("fixture.txt", "text", test.content, 2, 1<<20)
			for index := range want {
				want[index].Path = test.path
				want[index].Language = test.language
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("malformed source did not preserve fallback:\ngot  %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestChunkFile_MalformedJSMatchesLineFallback(t *testing.T) {
	content := "export function broken() {\nconst nested = 1;\nline 3\nline 4\n"
	got := ChunkFile("fixture.ts", "ts/js", content, 2, 1<<20)
	want := ChunkFile("fixture.txt", "text", content, 2, 1<<20)
	for index := range want {
		want[index].Path = "fixture.ts"
		want[index].Language = "ts/js"
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("malformed JS did not preserve fallback:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestChunkFile_LongLineDoesNotDropFollowingContent(t *testing.T) {
	long := strings.Repeat("x", 10_000)
	chunks := ChunkFile("fixture.txt", "text", long+"\nafter\n", 10, 128)
	if len(chunks) != 2 || chunks[0].Content != long+"\n" || chunks[1].Content != "after\n" {
		t.Fatalf("long line content was lost: %#v", chunks)
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
