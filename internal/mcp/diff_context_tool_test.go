package mcp

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"memento-mcp/internal/indexing"
)

type decodedDiffContext struct {
	Paths   []string `json:"paths"`
	Summary struct {
		RequestedPaths int    `json:"requestedPaths"`
		IndexedPaths   int    `json:"indexedPaths"`
		IncludedPaths  int    `json:"includedPaths"`
		SkippedPaths   int    `json:"skippedPaths"`
		OmittedPaths   int    `json:"omittedPaths"`
		TotalChunks    int    `json:"totalChunks"`
		IncludedChunks int    `json:"includedChunks"`
		OmittedChunks  int    `json:"omittedChunks"`
		Text           string `json:"text"`
	} `json:"summary"`
	Files []struct {
		Path           string           `json:"path"`
		TotalChunks    int              `json:"totalChunks"`
		IncludedChunks int              `json:"includedChunks"`
		Chunks         []indexing.Chunk `json:"chunks"`
	} `json:"files"`
	SkippedPaths []diffContextSkippedPath `json:"skippedPaths"`
	OmittedPaths []diffContextSkippedPath `json:"omittedPaths"`
	Limits       struct {
		MaxFiles         int  `json:"maxFiles"`
		MaxChunksPerFile int  `json:"maxChunksPerFile"`
		MaxTokens        int  `json:"maxTokens"`
		MaxTotalBytes    int  `json:"maxTotalBytes"`
		Clamped          bool `json:"clamped"`
	} `json:"limits"`
}

func setupDiffContextTestRepo(t *testing.T) (string, *indexing.Indexer, context.Context) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"pkg/a.go": `package pkg

func AOne() {
	println("one")
}
func ATwo() {
	println("two")
}
func AThree() {
	println("three")
}
`,
		"pkg/b.go": `package pkg

func BOne() {
	println("one")
}
`,
		"pkg/c.go":  "package pkg\n\nfunc RelatedButUnrequested() {}\n",
		"notes.bin": "not indexed by the default extension policy\n",
		"empty.go":  "   \n",
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := indexing.New(indexing.Config{
		RootAbs:       root,
		StoreDir:      t.TempDir(),
		MaxChunkLines: 4,
		MaxChunkBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)
	return root, idx, ctx
}

func decodeDiffContext(t *testing.T, result any) decodedDiffContext {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded decodedDiffContext
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestRepoDiffContextReturnsOnlyExplicitPathsWithSummary(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{
		"paths":            []string{"pkg/a.go", "pkg/b.go", "./pkg/a.go"},
		"maxChunksPerFile": 2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if strings.Join(got.Paths, ",") != "pkg/a.go,pkg/b.go" {
		t.Fatalf("normalized paths = %#v", got.Paths)
	}
	if len(got.Files) != 2 || got.Files[0].Path != "pkg/a.go" || got.Files[1].Path != "pkg/b.go" {
		t.Fatalf("expected only requested files in request order, got %#v", got.Files)
	}
	for _, file := range got.Files {
		if file.Path == "pkg/c.go" {
			t.Fatal("repo_diff_context expanded to an unrequested related file")
		}
		if file.IncludedChunks > 2 {
			t.Fatalf("per-file chunk limit not enforced: %#v", file)
		}
		for index := 1; index < len(file.Chunks); index++ {
			if file.Chunks[index].StartLine < file.Chunks[index-1].StartLine {
				t.Fatalf("chunks not returned in source order: %#v", file.Chunks)
			}
		}
	}
	if got.Summary.RequestedPaths != 2 || got.Summary.IndexedPaths != 2 || got.Summary.IncludedPaths != 2 {
		t.Fatalf("unexpected path summary: %#v", got.Summary)
	}
	if got.Summary.TotalChunks <= got.Summary.IncludedChunks || got.Summary.IncludedChunks != 4 {
		t.Fatalf("unexpected chunk summary: %#v", got.Summary)
	}
	if got.Summary.OmittedChunks != got.Summary.TotalChunks-got.Summary.IncludedChunks || got.Summary.Text == "" {
		t.Fatalf("inconsistent summary: %#v", got.Summary)
	}
	if got.Limits.MaxFiles != 2 || got.Limits.MaxChunksPerFile != 2 {
		t.Fatalf("unexpected limits: %#v", got.Limits)
	}
}

func TestRepoDiffContextSkipsExistingUnindexedPaths(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{
		"paths": []string{"notes.bin"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if len(got.Files) != 0 || len(got.SkippedPaths) != 1 || got.SkippedPaths[0].Reason != "not_indexed" {
		t.Fatalf("expected unsupported file to be reported as skipped, got %#v", got)
	}
	if got.Summary.SkippedPaths != 1 || got.Summary.IndexedPaths != 0 {
		t.Fatalf("unexpected skipped summary: %#v", got.Summary)
	}
}

func TestRepoDiffContextReportsBudgetOmissions(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{
		"paths":         []string{"pkg/a.go"},
		"maxTotalBytes": 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if len(got.Files) != 0 || got.Summary.IncludedChunks != 0 || got.Summary.OmittedChunks != got.Summary.TotalChunks {
		t.Fatalf("expected all chunks to be omitted by the byte budget, got %#v", got)
	}
	if got.Summary.OmittedPaths != 1 || len(got.OmittedPaths) != 1 || got.OmittedPaths[0].Reason != "budget" {
		t.Fatalf("expected the budget-omitted path to be classified, got %#v", got)
	}
	if !got.Limits.Clamped || got.Limits.MaxTotalBytes != 1 {
		t.Fatalf("expected clamped byte budget, got %#v", got.Limits)
	}
}

func TestRepoDiffContextPacksFocusMatchBeforeSourceOrder(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	if err := idx.EnsureIndexed(ctx, []string{"pkg/a.go"}); err != nil {
		t.Fatal(err)
	}
	chunks, err := idx.FileChunks("pkg/a.go")
	if err != nil {
		t.Fatal(err)
	}
	focusedBytes := 0
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "AThree") {
			focusedBytes = len(chunk.Content)
			break
		}
	}
	if focusedBytes == 0 {
		t.Fatal("fixture did not produce a focused chunk")
	}

	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{
		"paths":            []string{"pkg/a.go"},
		"focus":            "AThree",
		"maxChunksPerFile": 3,
		"maxTotalBytes":    focusedBytes,
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if len(got.Files) != 1 || len(got.Files[0].Chunks) != 1 || !strings.Contains(got.Files[0].Chunks[0].Content, "AThree") {
		t.Fatalf("expected the exact focus match to win the tight budget, got %#v", got.Files)
	}
}

func TestRepoDiffContextRejectsUnsafeOrInvalidPaths(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.go"), []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "empty", path: " ", want: "non-empty"},
		{name: "parent", path: "../outside.go", want: "outside workspace"},
		{name: "absolute", path: "/tmp/outside.go", want: "repo-relative"},
		{name: "windows absolute", path: `C:\\tmp\\outside.go`, want: "repo-relative"},
		{name: "root", path: ".", want: "outside workspace"},
		{name: "directory", path: "pkg", want: "directory"},
		{name: "missing", path: "missing.go", want: "no such file"},
		{name: "ignored", path: "secret.go", want: "ignored by Git"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{"paths": []string{test.path}}))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestRepoDiffContextRejectsSymlinkPath(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	outsidePath := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outsidePath, []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(root, "outside-link.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"outside-link.go"}}))
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error = %v, want symbolic-link rejection", err)
	}
}

func TestRepoDiffContextEvictsNewlyGitIgnoredChunks(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	secretPath := filepath.Join(root, "secret.go")
	if err := os.WriteFile(secretPath, []byte("package secret\n\nconst SecretNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondSecretPath := filepath.Join(root, "secret_two.go")
	if err := os.WriteFile(secondSecretPath, []byte("package secret\n\nconst SecondSecretNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := newRepoDiffContextTool(root, idx)
	secretPaths := []string{"secret.go", "secret_two.go"}
	if _, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": secretPaths})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret.go\nsecret_two.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": secretPaths})); err == nil || !strings.Contains(err.Error(), "ignored by Git") {
		t.Fatalf("error = %v, want Git-ignore rejection", err)
	}
	for _, rel := range secretPaths {
		if _, err := idx.FileChunks(rel); !os.IsNotExist(err) {
			t.Fatalf("stale ignored chunks remained for %s: %v", rel, err)
		}
	}
	results, err := idx.SearchContext(ctx, "SecretNeedle", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("ignored content remained searchable: %#v", results)
	}
}

func TestRepoDiffContextEvictsDeletedCachedPathBeforeReturningError(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	deletedPath := filepath.Join(root, "deleted.go")
	if err := os.WriteFile(deletedPath, []byte("package deleted\n\nconst DeletedNeedle = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := newRepoDiffContextTool(root, idx)
	if _, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"deleted.go"}})); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(deletedPath); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"deleted.go"}})); err == nil || !os.IsNotExist(err) {
		t.Fatalf("error = %v, want missing-file error", err)
	}
	if _, err := idx.FileChunks("deleted.go"); !os.IsNotExist(err) {
		t.Fatalf("deleted path retained stale chunks: %v", err)
	}
	results, err := idx.SearchContext(ctx, "DeletedNeedle", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("deleted content remained searchable: %#v", results)
	}
}

func TestRepoDiffContextReportsNoChunkOmission(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"empty.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if got.Summary.OmittedPaths != 1 || len(got.OmittedPaths) != 1 || got.OmittedPaths[0].Reason != "no_chunks" {
		t.Fatalf("expected no_chunks omission, got %#v", got)
	}
}

func TestRepoDiffContextRejectsTooManyRawPaths(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	paths := make([]string, defaultRepoDiffContextMaxPaths+1)
	for index := range paths {
		paths[index] = "pkg/a.go"
	}
	_, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{"paths": paths}))
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("error = %v, want maximum-path rejection", err)
	}
}

func TestRepoDiffContextRejectsNonRegularPath(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	socketPath := filepath.Join(root, "socket.go")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("Unix sockets unavailable: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	_, err = newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"socket.go"}}))
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error = %v, want non-regular-file rejection", err)
	}
}

func TestRepoDiffContextToolDefinitionContract(t *testing.T) {
	tool := repoDiffContextToolDefinition()
	if tool.Name != "repo_diff_context" || tool.OutputSchema == nil {
		t.Fatalf("unexpected tool definition: %#v", tool)
	}
	got, _ := tool.Meta["anthropic/maxResultSizeChars"].(int)
	if got != defaultAnthropicMaxResultSizeChars {
		t.Fatalf("large-result metadata = %#v", tool.Meta)
	}
	properties := tool.InputSchema["properties"].(map[string]any)
	paths := properties["paths"].(map[string]any)
	if paths["maxItems"] != defaultRepoDiffContextMaxPaths {
		t.Fatalf("paths schema = %#v", paths)
	}
	outputProperties := tool.OutputSchema["properties"].(map[string]any)
	if _, ok := outputProperties["omittedPaths"]; !ok {
		t.Fatalf("output schema omitted omittedPaths: %#v", outputProperties)
	}
	required := tool.OutputSchema["required"].([]any)
	foundRequired := false
	for _, name := range required {
		if name == "omittedPaths" {
			foundRequired = true
			break
		}
	}
	if !foundRequired {
		t.Fatalf("output schema does not require omittedPaths: %#v", required)
	}
}
