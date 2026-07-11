package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeAdvertisesResourcesAndPrompts(t *testing.T) {
	server, _ := setupNativeUXServer(t)
	result := server.initializeResult(json.RawMessage(`{"protocolVersion":"2024-11-05"}`))
	capabilities := result["capabilities"].(map[string]any)
	for _, name := range []string{"tools", "resources", "prompts"} {
		if _, ok := capabilities[name].(map[string]any); !ok {
			t.Fatalf("expected %s capability, got %#v", name, capabilities)
		}
	}
}

func TestResourcesListAndReadNotesAndKeyFiles(t *testing.T) {
	server, root := setupNativeUXServer(t)
	store, err := NewNoteStore(root)
	if err != nil {
		t.Fatal(err)
	}
	key := "architecture/runtime switch"
	if got := noteResourceURI(key); got != "note://memory/architecture/runtime%20switch" {
		t.Fatalf("unexpected native note URI: %s", got)
	}
	if _, err := store.Upsert(Note{Key: key, Text: "Keep one child per active repository.", Tags: []string{"architecture"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(Note{Key: "obsolete", Text: "old design"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Tombstone("obsolete", "superseded"); err != nil {
		t.Fatal(err)
	}

	list := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 1, Method: "resources/list", Params: json.RawMessage(`{}`)}))
	resources := resourceDescriptors(t, list["resources"])
	if !hasResource(resources, noteResourceURI(key)) || !hasResource(resources, fileResourceURI("README.md")) || !hasResource(resources, fileResourceURI("go.mod")) {
		t.Fatalf("expected note and key file resources, got %#v", resources)
	}
	if hasResource(resources, noteResourceURI("obsolete")) {
		t.Fatalf("tombstoned note must not be exposed for @-mention: %#v", resources)
	}

	noteRead := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 2, Method: "resources/read", Params: rawJSON(t, map[string]any{"uri": noteResourceURI(key)})}))
	noteText := firstResourceText(t, noteRead)
	if !strings.Contains(noteText, "# Memory: "+key) || !strings.Contains(noteText, "Keep one child") || !strings.Contains(noteText, "Status: fresh") {
		t.Fatalf("unexpected note resource: %s", noteText)
	}
	note, err := store.Read(key)
	if err != nil {
		t.Fatal(err)
	}
	if note.RetrievalCount != 2 {
		t.Fatalf("expected resource read plus explicit read usage, got %#v", note)
	}

	fileRead := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 3, Method: "resources/read", Params: rawJSON(t, map[string]any{"uri": fileResourceURI("README.md")})}))
	if text := firstResourceText(t, fileRead); !strings.Contains(text, "Native UX fixture") {
		t.Fatalf("unexpected file resource: %s", text)
	}
}

func TestResourceReadRedactsAndRejectsUnsafePaths(t *testing.T) {
	server, root := setupNativeUXServer(t)
	secret := "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
	if err := os.WriteFile(filepath.Join(root, "config.txt"), []byte("token="+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN="+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "private.pem"), []byte("PRIVATE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escaped-link.txt")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	read := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: rawJSON(t, map[string]any{"uri": fileResourceURI("config.txt")})}))
	text := firstResourceText(t, read)
	if strings.Contains(text, secret) || !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("expected redacted file resource, got %s", text)
	}
	for _, uri := range []string{"repo://file/../outside.txt", fileResourceURI(".env"), fileResourceURI("private.pem"), fileResourceURI("escaped-link.txt"), "file:///etc/passwd"} {
		resp := server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 2, Method: "resources/read", Params: rawJSON(t, map[string]any{"uri": uri})})
		if resp.Error == nil || resp.Error.Code != -32002 {
			t.Fatalf("expected resource-not-found for %s, got %#v", uri, resp)
		}
	}
}

func TestResourceBoundsNotesAndPreservesUTF8Prefixes(t *testing.T) {
	t.Setenv("MEMENTO_RESOURCE_MAX_BYTES", "200")
	server, root := setupNativeUXServer(t)
	store, err := NewNoteStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(Note{Key: "large", Text: strings.Repeat("note ", 200)}); err != nil {
		t.Fatal(err)
	}
	read := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: rawJSON(t, map[string]any{"uri": noteResourceURI("large")})}))
	if text := firstResourceText(t, read); len(text) > 200 || !strings.Contains(text, "Resource truncated") {
		t.Fatalf("expected bounded note resource, got %d bytes: %s", len(text), text)
	}

	t.Setenv("MEMENTO_RESOURCE_MAX_BYTES", "5")
	if err := os.WriteFile(filepath.Join(root, "unicode.txt"), []byte("abcdé"), 0o644); err != nil {
		t.Fatal(err)
	}
	unicodeRead := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 2, Method: "resources/read", Params: rawJSON(t, map[string]any{"uri": fileResourceURI("unicode.txt")})}))
	if text := firstResourceText(t, unicodeRead); text != "abcd" {
		t.Fatalf("expected valid UTF-8 prefix, got %q", text)
	}
	if err := os.WriteFile(filepath.Join(root, "invalid.txt"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	invalid := server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 3, Method: "resources/read", Params: rawJSON(t, map[string]any{"uri": fileResourceURI("invalid.txt")})})
	if invalid.Error == nil || invalid.Error.Code != -32002 {
		t.Fatalf("expected invalid UTF-8 rejection, got %#v", invalid)
	}
}

func TestResourcesListPaginationAndTemplates(t *testing.T) {
	server, root := setupNativeUXServer(t)
	store, err := NewNoteStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < defaultResourcePageSize+2; index++ {
		key := strings.Repeat("x", 3) + "-" + string(rune('A'+index%26)) + "-" + strings.Repeat("z", index/26)
		if _, err := store.Upsert(Note{Key: key, Text: "pagination"}); err != nil {
			t.Fatal(err)
		}
	}
	first := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 1, Method: "resources/list", Params: json.RawMessage(`{}`)}))
	if got := len(resourceDescriptors(t, first["resources"])); got != defaultResourcePageSize {
		t.Fatalf("expected %d first-page resources, got %d", defaultResourcePageSize, got)
	}
	cursor, ok := first["nextCursor"].(string)
	if !ok || cursor == "" {
		t.Fatalf("expected next cursor, got %#v", first)
	}
	second := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 2, Method: "resources/list", Params: rawJSON(t, map[string]any{"cursor": cursor})}))
	if len(resourceDescriptors(t, second["resources"])) == 0 {
		t.Fatal("expected second resource page")
	}
	templates := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 3, Method: "resources/templates/list", Params: json.RawMessage(`{}`)}))
	encoded, _ := json.Marshal(templates)
	if !strings.Contains(string(encoded), `repo://file/{path}`) {
		t.Fatalf("expected file resource template, got %s", encoded)
	}
	invalid := server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 4, Method: "resources/list", Params: json.RawMessage(`{"cursor":"invalid"}`)})
	if invalid.Error == nil || invalid.Error.Code != -32602 {
		t.Fatalf("expected invalid cursor error, got %#v", invalid)
	}
}

func TestPrimePromptDiscoveryAndBoundedContext(t *testing.T) {
	t.Setenv("MEMENTO_PRIME_MAX_BYTES", "3000")
	server, root := setupNativeUXServer(t)
	store, err := NewNoteStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(Note{Key: "runtime-contract", Text: "The broker keeps one child per repository."}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "service.go"), []byte("package fixture\n\nfunc Start() {\n\tpanic(\"body must stay out of outline\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	listed := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 1, Method: "prompts/list", Params: json.RawMessage(`{}`)}))
	encoded, _ := json.Marshal(listed)
	if !strings.Contains(string(encoded), `"name":"prime"`) || !strings.Contains(string(encoded), `"name":"path"`) || !strings.Contains(string(encoded), `"name":"focus"`) {
		t.Fatalf("expected discoverable prime prompt, got %s", encoded)
	}

	got := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 2, Method: "prompts/get", Params: rawJSON(t, map[string]any{
		"name":      "prime",
		"arguments": map[string]string{"path": "service.go", "focus": "Start lifecycle"},
	})}))
	text := firstPromptText(t, got)
	for _, expected := range []string{"Start lifecycle", "runtime-contract", "Native UX fixture", "Active file outline", `"name": "Start"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in prime prompt: %s", expected, text)
		}
	}
	if strings.Contains(text, "body must stay out") {
		t.Fatalf("prime outline leaked function body: %s", text)
	}
	if len(text) > 3000 {
		t.Fatalf("prime prompt exceeded byte cap: %d", len(text))
	}
	unknown := server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 3, Method: "prompts/get", Params: json.RawMessage(`{"name":"missing"}`)})
	if unknown.Error == nil || unknown.Error.Code != -32602 {
		t.Fatalf("expected invalid prompt error, got %#v", unknown)
	}
	oversized := server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 4, Method: "prompts/get", Params: rawJSON(t, map[string]any{"name": "prime", "arguments": map[string]string{"focus": strings.Repeat("x", 2_001)}})})
	if oversized.Error == nil || oversized.Error.Code != -32602 {
		t.Fatalf("expected oversized prompt argument error, got %#v", oversized)
	}
}

func TestResourceAndPromptMethodsFollowCurrentWorkspace(t *testing.T) {
	server, _ := setupNativeUXServer(t)
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(second, "README.md"), []byte("Second workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server.setCurrentRoot(second)
	read := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: rawJSON(t, map[string]any{"uri": fileResourceURI("README.md")})}))
	if text := firstResourceText(t, read); !strings.Contains(text, "Second workspace") {
		t.Fatalf("resource did not follow current workspace: %s", text)
	}
	prompt := rpcResultMap(t, server.handleRPC(context.Background(), rpcRequest{JSONRPC: "2.0", ID: 2, Method: "prompts/get", Params: json.RawMessage(`{"name":"prime"}`)}))
	if text := firstPromptText(t, prompt); !strings.Contains(text, "Second workspace") {
		t.Fatalf("prompt did not follow current workspace: %s", text)
	}
}

func TestNativeUXStdioProtocolDiscovery(t *testing.T) {
	server, root := setupNativeUXServer(t)
	store, err := NewNoteStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(Note{Key: "wire-note", Text: "wire content"}); err != nil {
		t.Fatal(err)
	}
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"note://memory/wire-note"}}`,
	}
	var output bytes.Buffer
	if err := server.ServeStdio(context.Background(), strings.NewReader(strings.Join(requests, "\n")+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != len(requests) {
		t.Fatalf("expected %d protocol responses, got %d: %s", len(requests), len(lines), output.String())
	}
	joined := output.String()
	for _, expected := range []string{`"resources":{}`, `"prompts":{}`, `note://memory/wire-note`, `"name":"prime"`, `wire content`} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in protocol output: %s", expected, joined)
		}
	}
}

func setupNativeUXServer(t *testing.T) (*Server, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Native UX fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/native\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{Root: root, Child: true})
	if err != nil {
		t.Fatal(err)
	}
	return server, root
}

func rpcResultMap(t *testing.T, response rpcResponse) map[string]any {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("unexpected RPC error: %#v", response.Error)
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", response.Result)
	}
	return result
}

func resourceDescriptors(t *testing.T, value any) []resourceDescriptor {
	t.Helper()
	resources, ok := value.([]resourceDescriptor)
	if !ok {
		t.Fatalf("expected resource descriptors, got %T", value)
	}
	return resources
}

func hasResource(resources []resourceDescriptor, uri string) bool {
	for _, resource := range resources {
		if resource.URI == uri {
			return true
		}
	}
	return false
}

func firstResourceText(t *testing.T, result map[string]any) string {
	t.Helper()
	contents, ok := result["contents"].([]resourceContent)
	if !ok || len(contents) != 1 {
		t.Fatalf("expected one resource content, got %#v", result["contents"])
	}
	return contents[0].Text
}

func firstPromptText(t *testing.T, result map[string]any) string {
	t.Helper()
	messages, ok := result["messages"].([]map[string]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one prompt message, got %#v", result["messages"])
	}
	content, ok := messages[0]["content"].(map[string]any)
	if !ok {
		t.Fatalf("expected prompt content, got %#v", messages[0])
	}
	text, _ := content["text"].(string)
	return text
}
