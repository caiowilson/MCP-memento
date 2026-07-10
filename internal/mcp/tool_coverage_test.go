package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"memento-mcp/internal/indexing"
)

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(b)
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestServerExposesExpectedTools(t *testing.T) {
	root := t.TempDir()
	s := newBrokerServerForTest(t, root)

	resp := s.handleRPC(context.Background(), rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	})

	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}

	got := make(map[string]struct{}, len(decoded.Tools))
	for _, tool := range decoded.Tools {
		got[tool.Name] = struct{}{}
	}

	want := []string{
		"repo_list_files",
		"repo_read_file",
		"repo_search",
		"repo_related_files",
		"repo_context",
		"repo_switch_workspace",
		"repo_index_status",
		"repo_reindex",
		"repo_clear_index",
		"repo_index_debug",
		"memory_upsert",
		"memory_search",
		"memory_list",
		"memory_delete",
		"memory_clear",
	}
	for _, name := range want {
		if _, ok := got[name]; !ok {
			t.Fatalf("expected tool %q to be exposed, got %#v", name, got)
		}
	}
}

func TestRepoListFilesTool(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "beta.go"), []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "gamma.md"), []byte("# gamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoListFilesTool(root)
	got, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}

	result, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", got)
	}
	files, ok := result["files"].([]string)
	if !ok {
		t.Fatalf("expected files slice, got %T", result["files"])
	}
	if result["root"] != root {
		t.Fatalf("expected root %q, got %#v", root, result["root"])
	}
	if count, _ := result["count"].(int); count != 3 {
		t.Fatalf("expected count=3, got %#v", result["count"])
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %#v", files)
	}
	for _, want := range []string{"alpha.txt", "sub/beta.go", "sub/gamma.md"} {
		if !containsString(files, want) {
			t.Fatalf("expected files to contain %q, got %#v", want, files)
		}
	}

	globbed, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{"glob": "sub/*.go"}))
	if err != nil {
		t.Fatal(err)
	}
	globbedResult, ok := globbed.(map[string]any)
	if !ok {
		t.Fatalf("expected globbed map result, got %T", globbed)
	}
	globbedFiles, ok := globbedResult["files"].([]string)
	if !ok {
		t.Fatalf("expected globbed files slice, got %T", globbedResult["files"])
	}
	if len(globbedFiles) != 1 || globbedFiles[0] != "sub/beta.go" {
		t.Fatalf("expected globbed result to contain only sub/beta.go, got %#v", globbedFiles)
	}
}

func TestRepoReadFileDefaultMaxBytes(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("x", defaultRepoReadFileMaxBytes*2)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoReadFileTool(root)
	got, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{"path": "large.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", got)
	}
	read, _ := result["content"].(string)
	if len(read) > defaultRepoReadFileMaxBytes {
		t.Fatalf("expected default read size <= %d bytes, got %d", defaultRepoReadFileMaxBytes, len(read))
	}
	if len(read) >= len(content) {
		t.Fatal("expected large file read to be clamped by default maxBytes")
	}
}

func TestRepoReadFileLineBoundsSkipLongLine(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("x", defaultRepoReadFileMaxBytes*2) + "\nsecond\n"
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoReadFileTool(root)
	got, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{
		"path":      "large.txt",
		"startLine": 2,
		"endLine":   2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", got)
	}
	if content, _ := result["content"].(string); content != "second\n" {
		t.Fatalf("expected second line after skipped long line, got %q", content)
	}
}

func TestRepoReadFileRedactsSecrets(t *testing.T) {
	root := t.TempDir()
	secret := "A1b2C3d4E5f6G7h8I9j0K1l2M3n4"
	if err := os.WriteFile(filepath.Join(root, "config.txt"), []byte("API_KEY="+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := newRepoReadFileTool(root).Handler(context.Background(), rawJSON(t, map[string]any{"path": "config.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	result := got.(map[string]any)
	content := result["content"].(string)
	if strings.Contains(content, secret) || !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("expected file content to be redacted, got %q", content)
	}
}

func TestRepoReadFileRedactsQuotedSecretBeyondLookahead(t *testing.T) {
	root := t.TempDir()
	secret := strings.Repeat("a", 128*1024)
	if err := os.WriteFile(filepath.Join(root, "config.txt"), []byte(`DATABASE_PASSWORD="`+secret+`"`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := newRepoReadFileTool(root).Handler(context.Background(), rawJSON(t, map[string]any{
		"path":     "config.txt",
		"maxBytes": 128,
	}))
	if err != nil {
		t.Fatal(err)
	}
	content := got.(map[string]any)["content"].(string)
	if strings.Contains(content, strings.Repeat("a", 20)) || !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("expected bounded read to redact an incomplete quoted value, got %q", content)
	}
}

func TestRepoSearchDefaultSnippetBytes(t *testing.T) {
	root := t.TempDir()
	line := "needle " + strings.Repeat("x", defaultRepoSearchMaxSnippetBytes*2)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoSearchTool(root)
	got, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{"query": "needle"}))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", got)
	}
	matches, ok := result["matches"].([]map[string]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("expected one match, got %#v", result["matches"])
	}
	snippet, _ := matches[0]["snippet"].(string)
	if len(snippet) > defaultRepoSearchMaxSnippetBytes {
		t.Fatalf("expected snippet to be clamped around %d bytes, got %d", defaultRepoSearchMaxSnippetBytes, len(snippet))
	}
	if truncated, _ := matches[0]["snippetTruncated"].(bool); !truncated {
		t.Fatalf("expected snippetTruncated=true, got %#v", matches[0])
	}
}

func TestRepoSearchTruncatedSnippetKeepsLateMatch(t *testing.T) {
	root := t.TempDir()
	line := strings.Repeat("x", defaultRepoSearchMaxSnippetBytes*2) + " needle " + strings.Repeat("y", defaultRepoSearchMaxSnippetBytes*2)
	if err := os.WriteFile(filepath.Join(root, "late.txt"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := newRepoSearchTool(root)
	got, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{"query": "needle"}))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", got)
	}
	matches, ok := result["matches"].([]map[string]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("expected one match, got %#v", result["matches"])
	}
	snippet, _ := matches[0]["snippet"].(string)
	if len(snippet) > defaultRepoSearchMaxSnippetBytes {
		t.Fatalf("expected snippet to be clamped around %d bytes, got %d", defaultRepoSearchMaxSnippetBytes, len(snippet))
	}
	if !strings.Contains(snippet, "needle") {
		t.Fatalf("expected truncated snippet to keep late match, got %q", snippet)
	}
	if truncated, _ := matches[0]["snippetTruncated"].(bool); !truncated {
		t.Fatalf("expected snippetTruncated=true, got %#v", matches[0])
	}
}

func TestRepoSearchRedactsSecretsAndSkipsEnvFiles(t *testing.T) {
	root := t.TempDir()
	secret := "A1b2C3d4E5f6G7h8I9j0K1l2M3n4"
	for name, content := range map[string]string{
		"config.go":  "package config\n// API_KEY=" + secret + "\n",
		".env.local": "API_KEY=" + secret + "\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := newRepoSearchTool(root).Handler(context.Background(), rawJSON(t, map[string]any{"query": "API_KEY"}))
	if err != nil {
		t.Fatal(err)
	}
	result := got.(map[string]any)
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["path"] != "config.go" {
		t.Fatalf("expected only the source file match, got %#v", matches)
	}
	snippet := matches[0]["snippet"].(string)
	if strings.Contains(snippet, secret) || !strings.Contains(snippet, "[REDACTED]") {
		t.Fatalf("expected search snippet to be redacted, got %q", snippet)
	}
}

func TestRepoSearchRedactsTruncatedQuotedSecret(t *testing.T) {
	root := t.TempDir()
	line := `DATABASE_PASSWORD="` + strings.Repeat("a", 2000) + `"`
	if err := os.WriteFile(filepath.Join(root, "config.go"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := newRepoSearchTool(root).Handler(context.Background(), rawJSON(t, map[string]any{
		"query":           "DATABASE_PASSWORD",
		"maxSnippetBytes": 100,
	}))
	if err != nil {
		t.Fatal(err)
	}
	matches := got.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Fatalf("expected one match, got %#v", matches)
	}
	snippet := matches[0]["snippet"].(string)
	if strings.Contains(snippet, strings.Repeat("a", 20)) || !strings.Contains(snippet, "[REDACTED]") {
		t.Fatalf("expected truncated search snippet to be redacted, got %q", snippet)
	}
}

func TestRepoIndexMaintenanceTools(t *testing.T) {
	_, idx := setupContextTestRepo(t)
	clearTool := newRepoClearIndexTool(idx)
	reindexTool := newRepoReindexTool(idx)

	before := idx.Status()
	if before.FilesIndexed == 0 {
		t.Fatal("expected initial indexed files before clear")
	}

	cleared, err := clearTool.Handler(context.Background(), rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	clearStatus, ok := cleared.(indexing.Status)
	if !ok {
		t.Fatalf("expected indexing.Status from clear, got %T", cleared)
	}
	if clearStatus.Ready || clearStatus.FilesIndexed != 0 || clearStatus.BytesIndexed != 0 {
		t.Fatalf("expected cleared status to be empty, got %#v", clearStatus)
	}
	if _, err := idx.FileChunks("pkg/a.go"); err == nil {
		t.Fatal("expected cleared index to drop file chunks")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reindexed, err := reindexTool.Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	reindexStatus, ok := reindexed.(indexing.Status)
	if !ok {
		t.Fatalf("expected indexing.Status from reindex, got %T", reindexed)
	}
	if !reindexStatus.Ready {
		t.Fatalf("expected reindexed status to be ready, got %#v", reindexStatus)
	}
	if reindexStatus.FilesIndexed == 0 {
		t.Fatalf("expected files to be reindexed, got %#v", reindexStatus)
	}
	if reindexStatus.LastIndexedAt == "" {
		t.Fatalf("expected lastIndexedAt to be populated, got %#v", reindexStatus)
	}
}

func TestMemoryUpsertDeduplicatesByKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	store, err := NewNoteStore(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	upsertTool := newMemoryUpsertTool(store)
	searchTool := newMemorySearchTool(store)
	ctx := context.Background()

	if _, err := upsertTool.Handler(ctx, rawJSON(t, map[string]any{"key": "k1", "text": "version 1"})); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertTool.Handler(ctx, rawJSON(t, map[string]any{"key": "k1", "text": "version 2"})); err != nil {
		t.Fatal(err)
	}

	res, err := searchTool.Handler(ctx, rawJSON(t, map[string]any{"query": "version"}))
	if err != nil {
		t.Fatal(err)
	}
	notes := res.(map[string]any)["notes"].([]Note)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note after dedup upsert, got %d", len(notes))
	}
	if notes[0].Text != "version 2" {
		t.Fatalf("expected updated text 'version 2', got %q", notes[0].Text)
	}
}

func TestMemoryListTool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := NewNoteStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	upsertTool := newMemoryUpsertTool(store)
	listTool := newMemoryListTool(store)
	ctx := context.Background()

	if _, err := upsertTool.Handler(ctx, rawJSON(t, map[string]any{"key": "note1", "text": "first"})); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertTool.Handler(ctx, rawJSON(t, map[string]any{"key": "note2", "text": "second"})); err != nil {
		t.Fatal(err)
	}

	res, err := listTool.Handler(ctx, rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	listed := res.(map[string]any)["notes"].([]Note)
	if len(listed) != 2 {
		t.Fatalf("expected 2 notes from memory_list, got %d", len(listed))
	}
}

func TestMemoryDeleteTool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := NewNoteStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	upsertTool := newMemoryUpsertTool(store)
	listTool := newMemoryListTool(store)
	deleteTool := newMemoryDeleteTool(store)
	ctx := context.Background()

	if _, err := upsertTool.Handler(ctx, rawJSON(t, map[string]any{"key": "keep", "text": "keep me"})); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertTool.Handler(ctx, rawJSON(t, map[string]any{"key": "gone", "text": "delete me"})); err != nil {
		t.Fatal(err)
	}

	res, err := deleteTool.Handler(ctx, rawJSON(t, map[string]any{"key": "gone"}))
	if err != nil {
		t.Fatal(err)
	}
	deleted, _ := res.(map[string]any)["deleted"].(bool)
	if !deleted {
		t.Fatal("expected deleted=true in response")
	}

	listRes, _ := listTool.Handler(ctx, rawJSON(t, map[string]any{}))
	remaining := listRes.(map[string]any)["notes"].([]Note)
	if len(remaining) != 1 || remaining[0].Key != "keep" {
		t.Fatalf("expected only 'keep' to remain, got %v", remaining)
	}
}

func TestMemoryDeleteToolNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := NewNoteStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deleteTool := newMemoryDeleteTool(store)
	_, err = deleteTool.Handler(context.Background(), rawJSON(t, map[string]any{"key": "does-not-exist"}))
	if err == nil {
		t.Fatal("expected an error when deleting a nonexistent note")
	}
}

func TestNoteStoreListAndDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := NewNoteStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Upsert(Note{Key: "alpha", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(Note{Key: "beta", Text: "second"}); err != nil {
		t.Fatal(err)
	}

	// List
	notes, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}

	// Delete one
	if err := store.Delete("alpha"); err != nil {
		t.Fatal(err)
	}
	notes, err = store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Key != "beta" {
		t.Fatalf("expected only 'beta' after delete, got %v", notes)
	}

	// Delete nonexistent
	if err := store.Delete("nonexistent"); err == nil {
		t.Fatal("expected error deleting nonexistent note")
	}
}

func TestMemoryToolsUpsertSearchAndClear(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := t.TempDir()
	store, err := NewNoteStore(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	upsertTool := newMemoryUpsertTool(store)
	searchTool := newMemorySearchTool(store)
	clearTool := newMemoryClearTool(store)

	upserted, err := upsertTool.Handler(context.Background(), rawJSON(t, map[string]any{
		"key":  "repo-overview",
		"text": "Remember the runtime switching contract",
		"tags": []string{"MCP", "Repo", "mcp"},
		"path": "./docs/../README.md",
		"meta": map[string]any{
			"owner":  "codex",
			"ticket": "10",
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	upsertedMap, ok := upserted.(map[string]any)
	if !ok {
		t.Fatalf("expected map result from upsert, got %T", upserted)
	}
	stored, ok := upsertedMap["stored"].(Note)
	if !ok {
		t.Fatalf("expected stored note, got %T", upsertedMap["stored"])
	}
	if stored.Key != "repo-overview" {
		t.Fatalf("expected stored key repo-overview, got %#v", stored.Key)
	}
	if stored.Path != "README.md" {
		t.Fatalf("expected cleaned path README.md, got %#v", stored.Path)
	}
	if got, want := stored.Tags, []string{"mcp", "repo"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected normalized tags %v, got %#v", want, got)
	}
	if stored.Meta["owner"] != "codex" || stored.Meta["ticket"] != "10" {
		t.Fatalf("expected stored metadata, got %#v", stored.Meta)
	}
	if stored.UpdatedAt == "" {
		t.Fatal("expected UpdatedAt to be populated")
	}

	matchedByTag, err := searchTool.Handler(context.Background(), rawJSON(t, map[string]any{
		"tags": []string{"mcp"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	matchedByTagMap, ok := matchedByTag.(map[string]any)
	if !ok {
		t.Fatalf("expected map result from search, got %T", matchedByTag)
	}
	notesByTag, ok := matchedByTagMap["notes"].([]Note)
	if !ok {
		t.Fatalf("expected notes slice from search, got %T", matchedByTagMap["notes"])
	}
	if len(notesByTag) != 1 || notesByTag[0].Key != "repo-overview" {
		t.Fatalf("expected tag search to return repo-overview, got %#v", notesByTag)
	}

	matchedByQuery, err := searchTool.Handler(context.Background(), rawJSON(t, map[string]any{
		"query": "runtime switching",
	}))
	if err != nil {
		t.Fatal(err)
	}
	matchedByQueryMap, ok := matchedByQuery.(map[string]any)
	if !ok {
		t.Fatalf("expected map result from search, got %T", matchedByQuery)
	}
	notesByQuery := matchedByQueryMap["notes"].([]Note)
	if len(notesByQuery) != 1 || notesByQuery[0].Key != "repo-overview" {
		t.Fatalf("expected query search to return repo-overview, got %#v", notesByQuery)
	}

	cleared, err := clearTool.Handler(context.Background(), rawJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	clearedMap, ok := cleared.(map[string]any)
	if !ok {
		t.Fatalf("expected map result from clear, got %T", cleared)
	}
	if got, _ := clearedMap["cleared"].(bool); !got {
		t.Fatalf("expected cleared=true, got %#v", clearedMap["cleared"])
	}

	afterClear, err := searchTool.Handler(context.Background(), rawJSON(t, map[string]any{
		"query": "runtime switching",
	}))
	if err != nil {
		t.Fatal(err)
	}
	afterClearMap, ok := afterClear.(map[string]any)
	if !ok {
		t.Fatalf("expected map result from search, got %T", afterClear)
	}
	afterClearNotes := afterClearMap["notes"].([]Note)
	if len(afterClearNotes) != 0 {
		t.Fatalf("expected no notes after clear, got %#v", afterClearNotes)
	}
}
