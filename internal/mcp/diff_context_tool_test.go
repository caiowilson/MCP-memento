package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"memento-mcp/internal/gitstate"
	"memento-mcp/internal/indexing"
)

type decodedDiffContext struct {
	PathSource   string                    `json:"pathSource"`
	Paths        []string                  `json:"paths"`
	Changes      []gitstate.WorktreeChange `json:"changes"`
	DeletedPaths []string                  `json:"deletedPaths"`
	DiffSummary  diffContextDiffSummary    `json:"diffSummary"`
	Summary      struct {
		RequestedPaths     int    `json:"requestedPaths"`
		DetectedPaths      int    `json:"detectedPaths"`
		SelectedChanges    int    `json:"selectedChanges"`
		DeletedPaths       int    `json:"deletedPaths"`
		FilteredPaths      int    `json:"filteredPaths"`
		PathLimitOmissions int    `json:"pathLimitOmissions"`
		IndexedPaths       int    `json:"indexedPaths"`
		IncludedPaths      int    `json:"includedPaths"`
		SkippedPaths       int    `json:"skippedPaths"`
		OmittedPaths       int    `json:"omittedPaths"`
		TotalChunks        int    `json:"totalChunks"`
		IncludedChunks     int    `json:"includedChunks"`
		OmittedChunks      int    `json:"omittedChunks"`
		Text               string `json:"text"`
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

func commitDiffContextFixture(t *testing.T, root string) {
	t.Helper()
	runDiffContextGit(t, root, "init", "-q")
	runDiffContextGit(t, root, "config", "user.email", "diff-context@example.com")
	runDiffContextGit(t, root, "config", "user.name", "Diff Context Test")
	runDiffContextGit(t, root, "add", "--", ".")
	runDiffContextGit(t, root, "commit", "-q", "-m", "base")
}

func runDiffContextGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func writeDiffContextFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func diffContextFilePaths(files []struct {
	Path           string           `json:"path"`
	TotalChunks    int              `json:"totalChunks"`
	IncludedChunks int              `json:"includedChunks"`
	Chunks         []indexing.Chunk `json:"chunks"`
}) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func diffContextSectionText(summary diffContextDiffSummary, scope string) string {
	for _, section := range summary.Sections {
		if section.Scope == scope {
			return section.Text
		}
	}
	return ""
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
		{name: "empty", path: "", want: "non-empty"},
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

func TestRepoDiffContextAutoPurgesCachedPathHiddenByNewGitIgnore(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	commitDiffContextFixture(t, root)
	writeDiffContextFixture(t, root, "secret.go", "package secret\n\nconst HiddenAfterIgnoreNeedle = true\n")
	tool := newRepoDiffContextTool(root, idx)
	if _, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"secret.go"}})); err != nil {
		t.Fatal(err)
	}
	writeDiffContextFixture(t, root, ".gitignore", "secret.go\n")

	result, err := tool.Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "HiddenAfterIgnoreNeedle") {
		t.Fatalf("auto result leaked newly ignored cached content: %s", encoded)
	}
	if _, err := idx.FileChunks("secret.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("newly ignored path retained cached chunks: %v", err)
	}
	search, err := idx.SearchContext(ctx, "HiddenAfterIgnoreNeedle", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 0 {
		t.Fatalf("newly ignored content remained searchable: %#v", search)
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

func TestRepoDiffContextAutoDetectsCompleteDirtyWorktree(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	writeDiffContextFixture(t, root, "both.go", "package dirty\n\nconst Both = \"base\"\n")
	writeDiffContextFixture(t, root, "old name.go", "package dirty\n\nconst RenameNeedle = true\n")
	writeDiffContextFixture(t, root, "deleted.go", "package dirty\n\nconst DeletedNeedle = true\n")
	commitDiffContextFixture(t, root)
	tool := newRepoDiffContextTool(root, idx)
	if _, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"old name.go"}})); err != nil {
		t.Fatal(err)
	}

	writeDiffContextFixture(t, root, "pkg/a.go", "package pkg\n\nconst IndexOnlyNeedle = true\n")
	runDiffContextGit(t, root, "add", "--", "pkg/a.go")
	writeDiffContextFixture(t, root, "pkg/b.go", "package pkg\n\nconst WorktreeOnlyNeedle = true\n")
	writeDiffContextFixture(t, root, "both.go", "package dirty\n\nconst Both = \"index state\"\n")
	runDiffContextGit(t, root, "add", "--", "both.go")
	writeDiffContextFixture(t, root, "both.go", "package dirty\n\nconst Both = \"worktree state\"\n")
	runDiffContextGit(t, root, "mv", "--", "old name.go", "renamed file.go")
	writeDiffContextFixture(t, root, "renamed file.go", "package dirty\n\nconst RenamedCurrent = true\n")
	if err := os.Remove(filepath.Join(root, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	writeDiffContextFixture(t, root, "untracked.go", "package dirty\n\nconst UntrackedNeedle = true\n")

	result, err := tool.Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	wantPaths := "both.go,pkg/a.go,pkg/b.go,renamed file.go,untracked.go"
	if got.PathSource != "git_status" || strings.Join(got.Paths, ",") != wantPaths {
		t.Fatalf("auto paths = %q from %q, want %q", strings.Join(got.Paths, ","), got.PathSource, wantPaths)
	}
	if strings.Join(got.DeletedPaths, ",") != "deleted.go" {
		t.Fatalf("deleted paths = %#v", got.DeletedPaths)
	}
	if got.Summary.RequestedPaths != 0 || got.Summary.DetectedPaths != 6 || got.Summary.SelectedChanges != 6 || got.Summary.DeletedPaths != 1 {
		t.Fatalf("dirty-worktree summary = %#v", got.Summary)
	}
	if len(got.Changes) != 6 {
		t.Fatalf("changes = %#v", got.Changes)
	}
	if strings.Join(diffContextFilePaths(got.Files), ",") != wantPaths {
		t.Fatalf("chunk-loaded files = %#v", diffContextFilePaths(got.Files))
	}

	staged := diffContextSectionText(got.DiffSummary, diffContextScopeStaged)
	unstaged := diffContextSectionText(got.DiffSummary, diffContextScopeUnstaged)
	untracked := diffContextSectionText(got.DiffSummary, diffContextScopeUntracked)
	for needle, section := range map[string]string{
		"IndexOnlyNeedle":    staged,
		"index state":        staged,
		"RenameNeedle":       staged,
		"WorktreeOnlyNeedle": unstaged,
		"worktree state":     unstaged,
		"DeletedNeedle":      unstaged,
		"UntrackedNeedle":    untracked,
	} {
		if !strings.Contains(section, needle) {
			t.Errorf("%q missing from its diff scope: %s", needle, section)
		}
	}
	if strings.Contains(staged, "WorktreeOnlyNeedle") || strings.Contains(unstaged, "IndexOnlyNeedle") {
		t.Fatalf("staged/unstaged scopes leaked across boundaries: staged=%s\nunstaged=%s", staged, unstaged)
	}
	if _, err := idx.FileChunks("old name.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rename source retained cached chunks: %v", err)
	}
	search, err := idx.SearchContext(ctx, "RenameNeedle", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 0 {
		t.Fatalf("rename-source content remained searchable: %#v", search)
	}
}

func TestRepoDiffContextExplicitGitDiffIsConstrainedToRequestedPaths(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	commitDiffContextFixture(t, root)
	writeDiffContextFixture(t, root, "pkg/a.go", "package pkg\n\nconst ExplicitOnlyNeedle = true\n")
	writeDiffContextFixture(t, root, "pkg/b.go", "package pkg\n\nconst UnrequestedDirtyNeedle = true\n")

	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"pkg/a.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if got.PathSource != "explicit" || strings.Join(got.Paths, ",") != "pkg/a.go" || len(got.Changes) != 1 || got.Changes[0].Path != "pkg/a.go" {
		t.Fatalf("explicit selection expanded beyond requested path: %#v", got)
	}
	if !strings.Contains(string(encoded), "ExplicitOnlyNeedle") || strings.Contains(string(encoded), "UnrequestedDirtyNeedle") {
		t.Fatalf("explicit diff was not path-constrained: %s", encoded)
	}
}

func TestRepoDiffContextExplicitRenameEvictsCachedSource(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	writeDiffContextFixture(t, root, "old.go", "package renamed\n\nconst ExplicitRenameSourceNeedle = true\n")
	commitDiffContextFixture(t, root)
	tool := newRepoDiffContextTool(root, idx)
	if _, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"old.go"}})); err != nil {
		t.Fatal(err)
	}
	runDiffContextGit(t, root, "mv", "--", "old.go", "new.go")
	writeDiffContextFixture(t, root, "new.go", "package renamed\n\nconst ExplicitRenameCurrent = true\n")

	result, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"new.go"}}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if len(got.Changes) != 1 || !got.Changes[0].Renamed || got.Changes[0].PreviousPath != "old.go" {
		t.Fatalf("explicit rename change = %#v", got.Changes)
	}
	if _, err := idx.FileChunks("old.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit rename source retained cached chunks: %v", err)
	}
	search, err := idx.SearchContext(ctx, "ExplicitRenameSourceNeedle", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 0 {
		t.Fatalf("explicit rename-source content remained searchable: %#v", search)
	}
}

func TestRepoDiffContextPreservesLeadingWhitespacePathInAutoAndExplicitModes(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	commitDiffContextFixture(t, root)
	const rel = " leading.go"
	writeDiffContextFixture(t, root, rel, "package leading\n\nconst LeadingWhitespaceNeedle = true\n")
	tool := newRepoDiffContextTool(root, idx)

	autoResult, err := tool.Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	auto := decodeDiffContext(t, autoResult)
	if len(auto.Paths) != 1 || auto.Paths[0] != rel || len(auto.Changes) != 1 || auto.Changes[0].Path != rel {
		t.Fatalf("auto whitespace path = %#v changes=%#v", auto.Paths, auto.Changes)
	}

	explicitResult, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": []string{rel}}))
	if err != nil {
		t.Fatal(err)
	}
	explicit := decodeDiffContext(t, explicitResult)
	if len(explicit.Paths) != 1 || explicit.Paths[0] != rel || len(explicit.Files) != 1 || explicit.Files[0].Path != rel {
		t.Fatalf("explicit whitespace path did not round-trip: %#v", explicit)
	}
}

func TestRepoDiffContextAutoSummarizesBinaryWithoutChunkLoading(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	commitDiffContextFixture(t, root)
	if err := os.WriteFile(filepath.Join(root, "notes.bin"), []byte{0, 1, 2, 3, 4}, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if len(got.Changes) != 1 || got.Changes[0].Path != "notes.bin" || len(got.Paths) != 0 || len(got.Files) != 0 {
		t.Fatalf("binary auto selection = %#v", got)
	}
	text := diffContextSectionText(got.DiffSummary, diffContextScopeUnstaged)
	if !strings.Contains(text, "Binary files") || strings.IndexByte(text, 0) >= 0 {
		t.Fatalf("binary diff summary = %q", text)
	}
}

func TestRepoDiffContextNestedWorkspaceUsesWorkspaceRelativeDiffHeaders(t *testing.T) {
	repo := t.TempDir()
	writeDiffContextFixture(t, repo, "workspace/in.go", "package workspace\nconst Value = \"base\"\n")
	writeDiffContextFixture(t, repo, "sibling/out.go", "package sibling\nconst Value = \"base\"\n")
	commitDiffContextFixture(t, repo)
	writeDiffContextFixture(t, repo, "workspace/in.go", "package workspace\nconst Value = \"INSIDE_NEEDLE\"\n")
	writeDiffContextFixture(t, repo, "sibling/out.go", "package sibling\nconst Value = \"SIBLING_NEEDLE\"\n")
	root := filepath.Join(repo, "workspace")
	idx, err := indexing.New(indexing.Config{RootAbs: root, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)

	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	text := diffContextSectionText(got.DiffSummary, diffContextScopeUnstaged)
	if strings.Join(got.Paths, ",") != "in.go" || !strings.Contains(text, "a/in.go") || strings.Contains(text, "workspace/in.go") || strings.Contains(text, "SIBLING_NEEDLE") {
		t.Fatalf("nested workspace result paths=%#v diff=%s", got.Paths, text)
	}
}

func TestRepoDiffContextAutoEvictsDeletedCachedPath(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	writeDiffContextFixture(t, root, "deleted.go", "package deleted\n\nconst AutoDeletedNeedle = true\n")
	commitDiffContextFixture(t, root)
	tool := newRepoDiffContextTool(root, idx)
	if _, err := tool.Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"deleted.go"}})); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "deleted.go")); err != nil {
		t.Fatal(err)
	}

	result, err := tool.Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if len(got.Paths) != 0 || strings.Join(got.DeletedPaths, ",") != "deleted.go" || got.Summary.DeletedPaths != 1 {
		t.Fatalf("deleted auto result = %#v", got)
	}
	if _, err := idx.FileChunks("deleted.go"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted path retained cached chunks: %v", err)
	}
	search, err := idx.SearchContext(ctx, "AutoDeletedNeedle", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 0 {
		t.Fatalf("deleted content remained searchable: %#v", search)
	}
}

func TestRepoDiffContextAutoCleanAndNonGitContracts(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		root, idx, ctx := setupDiffContextTestRepo(t)
		commitDiffContextFixture(t, root)
		result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{}))
		if err != nil {
			t.Fatal(err)
		}
		got := decodeDiffContext(t, result)
		if got.PathSource != "git_status" || len(got.Paths) != 0 || len(got.Changes) != 0 || len(got.DeletedPaths) != 0 {
			t.Fatalf("clean result = %#v", got)
		}
		if !got.DiffSummary.Available || len(got.DiffSummary.Sections) != 0 || got.Summary.DetectedPaths != 0 {
			t.Fatalf("clean diff summary = %#v, counts = %#v", got.DiffSummary, got.Summary)
		}
		if got.DiffSummary.MaxBytes != defaultRepoDiffContextDiffBytes || got.DiffSummary.ContextLines != defaultRepoDiffContextDiffLines {
			t.Fatalf("default diff limits = %#v", got.DiffSummary)
		}
	})

	t.Run("auto outside Git", func(t *testing.T) {
		root, idx, ctx := setupDiffContextTestRepo(t)
		_, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{}))
		if err == nil || !strings.Contains(err.Error(), "not a Git worktree") {
			t.Fatalf("error = %v, want non-Git auto-detection failure", err)
		}
	})

	t.Run("explicit outside Git", func(t *testing.T) {
		root, idx, ctx := setupDiffContextTestRepo(t)
		result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{"paths": []string{"pkg/a.go"}}))
		if err != nil {
			t.Fatal(err)
		}
		got := decodeDiffContext(t, result)
		if got.PathSource != "explicit" || strings.Join(got.Paths, ",") != "pkg/a.go" || got.DiffSummary.Available {
			t.Fatalf("explicit non-Git result = %#v", got)
		}
	})

	t.Run("present empty paths", func(t *testing.T) {
		root, idx, ctx := setupDiffContextTestRepo(t)
		_, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{"paths": []string{}}))
		if err == nil || !strings.Contains(err.Error(), "non-empty array") {
			t.Fatalf("error = %v, want present-empty rejection", err)
		}
	})
}

func TestRepoDiffContextAutoCapsDetectedPathsDeterministically(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	paths := make([]string, defaultRepoDiffContextMaxPaths+2)
	for index := range paths {
		paths[index] = fmt.Sprintf("changed/%02d.go", index)
		writeDiffContextFixture(t, root, paths[index], fmt.Sprintf("package changed\n\nconst Value%02d = \"base\"\n", index))
	}
	commitDiffContextFixture(t, root)
	for index, rel := range paths {
		writeDiffContextFixture(t, root, rel, fmt.Sprintf("package changed\n\nconst Value%02d = \"dirty\"\n", index))
	}

	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeDiffContext(t, result)
	if len(got.Paths) != defaultRepoDiffContextMaxPaths || len(got.Changes) != defaultRepoDiffContextMaxPaths {
		t.Fatalf("selected path counts = paths:%d changes:%d", len(got.Paths), len(got.Changes))
	}
	if strings.Join(got.Paths, ",") != strings.Join(paths[:defaultRepoDiffContextMaxPaths], ",") {
		t.Fatalf("capped selection = %#v, want first lexical paths %#v", got.Paths, paths[:defaultRepoDiffContextMaxPaths])
	}
	if got.Summary.DetectedPaths != len(paths) || got.Summary.SelectedChanges != defaultRepoDiffContextMaxPaths || got.Summary.PathLimitOmissions != 2 {
		t.Fatalf("path-cap summary = %#v", got.Summary)
	}
}

func TestRepoDiffContextAutoFiltersSensitiveAndMementoIgnoredDiffs(t *testing.T) {
	root, idx, ctx := setupDiffContextTestRepo(t)
	writeDiffContextFixture(t, root, ".mementoignore", "hidden.go\n")
	writeDiffContextFixture(t, root, ".env", "API_KEY=base\n")
	writeDiffContextFixture(t, root, "private.pem", "base private material\n")
	writeDiffContextFixture(t, root, "hidden.go", "package hidden\nconst Hidden = \"base\"\n")
	commitDiffContextFixture(t, root)

	secrets := []string{
		"sk-proj-ENVSECRETABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"PRIVATE-PEM-SECRET-ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"MEMENTO-HIDDEN-SECRET-ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	}
	writeDiffContextFixture(t, root, ".env", "API_KEY="+secrets[0]+"\n")
	writeDiffContextFixture(t, root, "private.pem", secrets[1]+"\n")
	writeDiffContextFixture(t, root, "hidden.go", "package hidden\nconst Hidden = \""+secrets[2]+"\"\n")

	result, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("auto diff leaked filtered secret %q: %s", secret, encoded)
		}
	}
	got := decodeDiffContext(t, result)
	if len(got.Paths) != 0 || len(got.Changes) != 0 || len(got.DiffSummary.Sections) != 0 || got.Summary.FilteredPaths != 3 {
		t.Fatalf("filtered privacy result = %#v", got)
	}
}

func TestRepoDiffContextAutoHonorsPreCanceledContext(t *testing.T) {
	root, idx, _ := setupDiffContextTestRepo(t)
	commitDiffContextFixture(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newRepoDiffContextTool(root, idx).Handler(ctx, rawJSON(t, map[string]any{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
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
	if paths["minItems"] != 1 || paths["maxItems"] != defaultRepoDiffContextMaxPaths {
		t.Fatalf("paths schema = %#v", paths)
	}
	maxDiffBytes := properties["maxDiffBytes"].(map[string]any)
	if maxDiffBytes["maximum"] != maximumRepoDiffContextDiffBytes {
		t.Fatalf("maxDiffBytes schema = %#v", maxDiffBytes)
	}
	diffContextLines := properties["diffContextLines"].(map[string]any)
	if diffContextLines["maximum"] != maximumRepoDiffContextDiffLines {
		t.Fatalf("diffContextLines schema = %#v", diffContextLines)
	}
	if required, exists := tool.InputSchema["required"]; exists {
		for _, name := range required.([]any) {
			if name == "paths" {
				t.Fatalf("paths must be optional for Git auto-detection: %#v", required)
			}
		}
	}
	outputProperties := tool.OutputSchema["properties"].(map[string]any)
	for _, name := range []string{"pathSource", "paths", "changes", "deletedPaths", "diffSummary", "omittedPaths"} {
		if _, ok := outputProperties[name]; !ok {
			t.Fatalf("output schema omitted %s: %#v", name, outputProperties)
		}
	}
	required := tool.OutputSchema["required"].([]any)
	for _, requiredName := range []string{"pathSource", "paths", "changes", "deletedPaths", "diffSummary", "omittedPaths"} {
		foundRequired := false
		for _, name := range required {
			if name == requiredName {
				foundRequired = true
				break
			}
		}
		if !foundRequired {
			t.Fatalf("output schema does not require %s: %#v", requiredName, required)
		}
	}
}
