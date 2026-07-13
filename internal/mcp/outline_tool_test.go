package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoOutlineToolIsCompactStructuredAndRedacted(t *testing.T) {
	root := t.TempDir()
	secret := "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
	body := strings.Repeat("\t// implementation detail that should never be returned\n", 200)
	source := "package fixture\n\nimport \"context\"\n\n// Build uses " + secret + ".\nfunc Build(ctx context.Context, name string) (string, error) {\n" + body + "\treturn name, ctx.Err()\n}\n"
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	resultAny, err := newRepoOutlineTool(root).Handler(context.Background(), rawJSON(t, map[string]any{"path": "service.go"}))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := resultAny.(repoOutlineResult)
	if !ok {
		t.Fatalf("expected repoOutlineResult, got %T", resultAny)
	}
	if result.Path != "service.go" || result.Language != "go" || result.PackageName != "fixture" || result.Fallback {
		t.Fatalf("unexpected outline metadata: %#v", result)
	}
	if result.SymbolCount != 1 || result.TotalSymbols != 1 || result.Truncated {
		t.Fatalf("unexpected symbol counts: %#v", result)
	}
	if !containsString(result.Imports, "context") {
		t.Fatalf("expected imports, got %#v", result.Imports)
	}
	if result.OutlineBytes >= int(result.SourceBytes)/4 {
		t.Fatalf("expected outline materially smaller than source: outline=%d source=%d", result.OutlineBytes, result.SourceBytes)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("expected documentation secret to be redacted: %s", encoded)
	}
	if strings.Contains(string(encoded), "implementation detail") || strings.Contains(string(encoded), "return name") {
		t.Fatalf("expected implementation body omitted: %s", encoded)
	}
}

func TestRepoOutlineToolOptionsAndSymbolLimit(t *testing.T) {
	root := t.TempDir()
	source := `package fixture

import "context"

// First is documented.
func First(context.Context) {}
func Second() {}
func Third() {}
`
	if err := os.WriteFile(filepath.Join(root, "fixture.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	resultAny, err := newRepoOutlineTool(root).Handler(context.Background(), rawJSON(t, map[string]any{
		"path":                 "fixture.go",
		"includeDocumentation": false,
		"includeImports":       false,
		"maxSymbols":           2,
	}))
	if err != nil {
		t.Fatal(err)
	}
	result := resultAny.(repoOutlineResult)
	if !result.Truncated || result.SymbolCount != 2 || result.TotalSymbols != 3 {
		t.Fatalf("expected source-order truncation, got %#v", result)
	}
	if len(result.Imports) != 0 {
		t.Fatalf("expected imports excluded, got %#v", result.Imports)
	}
	for _, symbol := range result.Symbols {
		if symbol.Documentation != "" {
			t.Fatalf("expected documentation excluded, got %#v", symbol)
		}
	}
	if result.Symbols[0].Name != "First" || result.Symbols[1].Name != "Second" {
		t.Fatalf("expected source-order symbols, got %#v", result.Symbols)
	}
}

func TestRepoOutlineToolTruncatesLongDocumentation(t *testing.T) {
	root := t.TempDir()
	source := "package fixture\n\n// LongDoc " + strings.Repeat("x", 3_000) + "\nfunc LongDoc() {}\n"
	if err := os.WriteFile(filepath.Join(root, "fixture.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	resultAny, err := newRepoOutlineTool(root).Handler(context.Background(), rawJSON(t, map[string]any{"path": "fixture.go"}))
	if err != nil {
		t.Fatal(err)
	}
	result := resultAny.(repoOutlineResult)
	symbol := requireOutlineSymbol(t, result.Symbols, "LongDoc", "function")
	if !symbol.DocumentationTruncated || len(symbol.Documentation) > 2_048 {
		t.Fatalf("expected bounded documentation, got %d bytes and truncated=%v", len(symbol.Documentation), symbol.DocumentationTruncated)
	}
}

func TestRepoOutlineToolUnsupportedLanguageFallback(t *testing.T) {
	root := t.TempDir()
	source := "#!/usr/bin/env ruby\nclass Worker\n  def run\n    'body'\n  end\nend\n"
	if err := os.WriteFile(filepath.Join(root, "worker.rb"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	resultAny, err := newRepoOutlineTool(root).Handler(context.Background(), rawJSON(t, map[string]any{"path": "worker.rb"}))
	if err != nil {
		t.Fatal(err)
	}
	result := resultAny.(repoOutlineResult)
	if !result.Fallback || result.Language != "ruby" || result.SymbolCount == 0 {
		t.Fatalf("expected graceful Ruby fallback, got %#v", result)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "return 'body'") {
		t.Fatalf("fallback returned a function body: %s", encoded)
	}
}

func TestRepoOutlineToolUsesTreeSitterForPython(t *testing.T) {
	root := t.TempDir()
	source := "class Worker:\n    def run(self):\n        return 'python-outline-body'\n"
	if err := os.WriteFile(filepath.Join(root, "worker.py"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	resultAny, err := newRepoOutlineTool(root).Handler(context.Background(), rawJSON(t, map[string]any{"path": "worker.py"}))
	if err != nil {
		t.Fatal(err)
	}
	result := resultAny.(repoOutlineResult)
	if result.Fallback || result.Language != "python" {
		t.Fatalf("expected tree-sitter Python outline, got %#v", result)
	}
	run := requireOutlineSymbol(t, result.Symbols, "run", "method")
	if run.Container != "Worker" {
		t.Fatalf("expected Python method container, got %#v", run)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "python-outline-body") {
		t.Fatalf("outline returned a function body: %s", encoded)
	}
}

func TestRepoOutlineToolRejectsDirectoryAndOversizedFile(t *testing.T) {
	root := t.TempDir()
	tool := newRepoOutlineTool(root)
	if _, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{"path": "."})); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected directory error, got %v", err)
	}
	t.Setenv("MEMENTO_OUTLINE_MAX_FILE_BYTES", "8")
	if err := os.WriteFile(filepath.Join(root, "large.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Handler(context.Background(), rawJSON(t, map[string]any{"path": "large.go"})); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestRepoOutlineToolHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newRepoOutlineTool(t.TempDir()).Handler(ctx, json.RawMessage(`{"path":"missing.go"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
}

func TestRepoOutlineDefinitionHasStructuredSchema(t *testing.T) {
	tool := repoOutlineToolDefinition()
	if tool.Name != "repo_outline" || len(tool.OutputSchema) == 0 || tool.Annotations == nil {
		t.Fatalf("unexpected repo_outline definition: %#v", tool)
	}
	properties, _ := tool.OutputSchema["properties"].(map[string]any)
	if _, ok := properties["symbols"]; !ok {
		t.Fatalf("expected symbols output schema, got %#v", tool.OutputSchema)
	}
}

func TestRepoOutlineToolCallReturnsStructuredContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte("package fixture\n\nfunc Start() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{Root: root, Child: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := server.callTool(context.Background(), toolCallParams{
		Name:      "repo_outline",
		Arguments: json.RawMessage(`{"path":"service.go"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured outline result, got %T", result.StructuredContent)
	}
	if structured["path"] != "service.go" || structured["language"] != "go" {
		t.Fatalf("unexpected structured outline metadata: %#v", structured)
	}
	symbols, ok := structured["symbols"].([]any)
	if !ok || len(symbols) != 1 {
		t.Fatalf("expected one structured symbol, got %#v", structured["symbols"])
	}
}
