package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRootFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantRoot string
		wantRest []string
	}{
		{"no flag", []string{"setup"}, "", []string{"setup"}},
		{"--root with space", []string{"--root", "/tmp/proj", "setup"}, "/tmp/proj", []string{"setup"}},
		{"--root= form", []string{"--root=/tmp/proj", "setup"}, "/tmp/proj", []string{"setup"}},
		{"empty args", nil, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, rest := extractRootFlag(tt.args)
			if root != tt.wantRoot {
				t.Errorf("root = %q, want %q", root, tt.wantRoot)
			}
			if len(rest) != len(tt.wantRest) {
				t.Errorf("rest = %v, want %v", rest, tt.wantRest)
			}
		})
	}
}

func TestExtractServeFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantRoot  string
		wantChild bool
		wantRest  []string
	}{
		{
			name:      "keeps non-serve args",
			args:      []string{"setup", "--print-only"},
			wantRoot:  "",
			wantChild: false,
			wantRest:  []string{"setup", "--print-only"},
		},
		{
			name:      "extracts child and root flags",
			args:      []string{"--child", "--root", "/tmp/proj", "setup"},
			wantRoot:  "/tmp/proj",
			wantChild: true,
			wantRest:  []string{"setup"},
		},
		{
			name:      "extracts child and inline root flag",
			args:      []string{"setup", "--root=/tmp/proj", "--child"},
			wantRoot:  "/tmp/proj",
			wantChild: true,
			wantRest:  []string{"setup"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, child, rest := extractServeFlags(tt.args)
			if root != tt.wantRoot {
				t.Fatalf("root = %q, want %q", root, tt.wantRoot)
			}
			if child != tt.wantChild {
				t.Fatalf("child = %v, want %v", child, tt.wantChild)
			}
			if len(rest) != len(tt.wantRest) {
				t.Fatalf("rest len = %d, want %d (%v)", len(rest), len(tt.wantRest), rest)
			}
			for i := range rest {
				if rest[i] != tt.wantRest[i] {
					t.Fatalf("rest[%d] = %q, want %q", i, rest[i], tt.wantRest[i])
				}
			}
		})
	}
}

func TestServerConfigForServeLeavesAbsentRootEmpty(t *testing.T) {
	cfg := serverConfigForServe("", false)
	if cfg.Root != "" {
		t.Fatalf("expected absent --root to pass through empty, got %q", cfg.Root)
	}
	if cfg.Child {
		t.Fatal("expected child=false")
	}
}

func TestParseSetupFlags(t *testing.T) {
	opts, err := parseSetupFlags([]string{"--client=vscode", "--client=cursor", "--print-only"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.printOnly {
		t.Error("expected printOnly=true")
	}
	if len(opts.clients) != 2 || opts.clients[0] != "vscode" || opts.clients[1] != "cursor" {
		t.Errorf("unexpected clients: %v", opts.clients)
	}
}

func TestBuildClientConfigWithCwd(t *testing.T) {
	c := mcpClient{Name: "VS Code", Slug: "vscode", HasCwd: true, CwdVar: "${workspaceFolder}"}
	cfg := buildClientConfig(c, "/usr/local/bin/memento-mcp")

	if cfg["command"] != "/usr/local/bin/memento-mcp" {
		t.Errorf("unexpected command: %v", cfg["command"])
	}
	if cfg["cwd"] != "${workspaceFolder}" {
		t.Errorf("expected cwd for VS Code, got %v", cfg["cwd"])
	}
	if cfg["name"] != "memento" {
		t.Errorf("expected name for VS Code, got %v", cfg["name"])
	}
	if cfg["transport"] != "stdio" {
		t.Errorf("expected transport for VS Code, got %v", cfg["transport"])
	}
}

func TestBuildClientConfigWithoutCwd(t *testing.T) {
	c := mcpClient{Name: "Claude Desktop", Slug: "claude-desktop", HasCwd: false}
	cfg := buildClientConfig(c, "/usr/local/bin/memento-mcp")

	if _, has := cfg["cwd"]; has {
		t.Error("Claude Desktop config should not have cwd")
	}
	if _, has := cfg["name"]; has {
		t.Error("Claude Desktop config should not have name")
	}
	if _, has := cfg["transport"]; has {
		t.Error("Claude Desktop config should not have transport")
	}
	if cfg["command"] != "/usr/local/bin/memento-mcp" {
		t.Errorf("unexpected command: %v", cfg["command"])
	}
}

func TestUpsertConfigNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	entry := map[string]any{"command": "/bin/memento-mcp", "args": []string{}}
	data, err := upsertConfig(path, entry)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	servers := result["mcpServers"].(map[string]any)
	if _, ok := servers["memento"]; !ok {
		t.Error("expected memento entry")
	}
}

func TestUpsertConfigPreservesOtherServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	// Write existing config with another server
	existing := `{
  "mcpServers": {
    "other-server": {
      "command": "/bin/other",
      "args": []
    }
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	entry := map[string]any{"command": "/bin/memento-mcp", "args": []string{}}
	data, err := upsertConfig(path, entry)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	servers := result["mcpServers"].(map[string]any)
	if _, ok := servers["other-server"]; !ok {
		t.Error("upsert should preserve other servers")
	}
	if _, ok := servers["memento"]; !ok {
		t.Error("upsert should add memento")
	}
}

func TestUpsertConfigUpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	existing := `{
  "mcpServers": {
    "memento-mcp": {
      "command": "/old/path"
    }
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	entry := map[string]any{"command": "/new/path", "args": []string{}}
	data, err := upsertConfig(path, entry)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	servers := result["mcpServers"].(map[string]any)
	mcp := servers["memento-mcp"].(map[string]any)
	if mcp["command"] != "/new/path" {
		t.Errorf("expected updated command, got %v", mcp["command"])
	}
}

func TestConfigureClientsPrintOnly(t *testing.T) {
	var buf bytes.Buffer
	clients := []mcpClient{
		{Name: "VS Code", Slug: "vscode", HasCwd: true, CwdVar: "${workspaceFolder}",
			ConfigPath: "/fake/path/mcp.json"},
	}

	err := configureClients(clients, "/bin/memento-mcp", setupOptions{printOnly: true}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "# VS Code") {
		t.Error("print-only should include client name header")
	}
	if !strings.Contains(out, "memento-mcp") {
		t.Error("print-only should include config JSON")
	}
	if !strings.Contains(out, "${workspaceFolder}") {
		t.Error("print-only should include cwd variable for VS Code")
	}
}

func TestConfigureClientsWritesFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "subdir", "mcp.json")

	var buf bytes.Buffer
	clients := []mcpClient{
		{Name: "Test", Slug: "test", HasCwd: true, CwdVar: "${workspaceFolder}",
			ConfigPath: configPath},
	}

	err := configureClients(clients, "/bin/memento-mcp", setupOptions{}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	// Verify file was written
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file should exist: %v", err)
	}
	if !strings.Contains(string(data), "memento-mcp") {
		t.Error("config file should contain memento-mcp entry")
	}

	// Verify output
	if !strings.Contains(buf.String(), "✓") {
		t.Error("should print success marker")
	}
}

func TestFilterClients(t *testing.T) {
	all := []mcpClient{
		{Slug: "vscode"},
		{Slug: "cursor"},
		{Slug: "claude-desktop"},
	}

	got, err := filterClients(all, []string{"vscode", "claude-desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0].Slug != "vscode" || got[1].Slug != "claude-desktop" {
		t.Errorf("unexpected slugs: %v, %v", got[0].Slug, got[1].Slug)
	}
}

func TestSetupNonInteractive(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	var stdout, stderr bytes.Buffer
	clients := []mcpClient{
		{Name: "Test", Slug: "test", HasCwd: true, CwdVar: "${workspaceFolder}",
			ConfigPath: configPath},
	}
	opts := setupOptions{clients: []string{"test"}}

	err := setupNonInteractive(clients, opts, "/bin/memento-mcp", &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config should be written: %v", err)
	}
	if !strings.Contains(string(data), "memento-mcp") {
		t.Error("config should contain memento-mcp")
	}
}

func TestSetupNonInteractiveInvalidClient(t *testing.T) {
	var stdout, stderr bytes.Buffer
	clients := []mcpClient{
		{Name: "Test", Slug: "test"},
	}
	opts := setupOptions{clients: []string{"nonexistent"}}

	err := setupNonInteractive(clients, opts, "/bin/memento-mcp", &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown client")
	}
	if !strings.Contains(err.Error(), "unknown client selection") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetupCLIIntegration(t *testing.T) {
	// Test that "setup --print-only --client=vscode" works through handleCLICommand
	var stdout, stderr bytes.Buffer
	handled, code := handleCLICommand([]string{"setup", "--print-only", "--client=vscode"}, &stdout, &stderr)
	if !handled {
		t.Fatal("setup should be handled")
	}
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "memento-mcp") {
		t.Error("output should contain config")
	}
}

func TestParseSetupFlagsSupportsClientListsAndRejectsUnknown(t *testing.T) {
	opts, err := parseSetupFlags([]string{"--clients", "codex,claude", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.force || len(opts.clients) != 2 || opts.clients[1] != "claude-code" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if _, err := parseSetupFlags([]string{"--wat"}); err == nil {
		t.Fatal("expected unknown option to fail")
	}
}

func TestConfigureClientsRefusesInvalidJSONWithoutChangingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	original := []byte("{ definitely not json\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	client := mcpClient{Name: "Test", Slug: "test", ConfigPath: path}
	err := configureClients([]mcpClient{client}, "/bin/memento-mcp", setupOptions{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("configuration changed: %q", got)
	}
}

func TestUpsertConfigRefusesWrongContainerAndConflictingAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertConfig(path, map[string]any{"command": "/bin/memento"}); err == nil {
		t.Fatal("expected wrong mcpServers type to fail")
	}
	conflict := `{"mcpServers":{"memento":{"command":"/one"},"memento-mcp":{"command":"/two"}}}`
	if err := os.WriteFile(path, []byte(conflict), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertConfig(path, map[string]any{"command": "/bin/memento"}); err == nil {
		t.Fatal("expected alias conflict to fail")
	}
}

func TestWriteConfigIsAtomicAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(path, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".memento-config-*")); len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func TestConfigureCodexUsesOfficialCLIAndIsIdempotent(t *testing.T) {
	oldLookPath, oldCommand := clientLookPath, clientCommand
	t.Cleanup(func() { clientLookPath, clientCommand = oldLookPath, oldCommand })
	clientLookPath = func(name string) (string, error) { return "/bin/" + name, nil }
	var calls [][]string
	configured := false
	clientCommand = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if len(args) >= 3 && args[0] == "mcp" && args[1] == "list" {
			if configured {
				return []byte(`[{"name":"memento","transport":{"type":"stdio","command":"/bin/memento-mcp"}}]`), nil
			}
			return []byte(`[]`), nil
		}
		configured = true
		return nil, nil
	}
	client := mcpClient{Name: "Codex", Slug: "codex", Kind: cliClient, CLI: "codex"}
	var out bytes.Buffer
	if err := configureClients([]mcpClient{client}, "/bin/memento-mcp", setupOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || strings.Join(calls[1], " ") != "codex mcp add memento -- /bin/memento-mcp" {
		t.Fatalf("unexpected calls: %v", calls)
	}
	calls = nil
	if err := configureClients([]mcpClient{client}, "/bin/memento-mcp", setupOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("idempotent setup should only inspect, got %v", calls)
	}
}

func TestClaudeForcedReplacementRestoresExactConfigOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	original := []byte(`{"mcpServers":{"memento":{"command":"/old"}}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	oldCommand := clientCommand
	t.Cleanup(func() { clientCommand = oldCommand })
	clientCommand = func(name string, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[1] == "remove" {
			if err := os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
				return nil, err
			}
			return nil, nil
		}
		return nil, errors.New("simulated add failure")
	}
	client := mcpClient{Name: "Claude Code", Slug: "claude-code", Kind: cliClient, CLI: "claude", ConfigPath: path}
	err := configureCLIClient(client, clientState{Status: "stale", Name: "memento"}, "/new", true)
	if err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("expected restored failure, got %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("restored bytes differ: %q", got)
	}
}

func TestConfigureClientsPreflightsAllJSONBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	clients := []mcpClient{
		{Name: "First", Slug: "first", ConfigPath: first},
		{Name: "Invalid", Slug: "invalid", ConfigPath: invalid},
	}
	if err := configureClients(clients, "/bin/memento", setupOptions{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected preflight failure")
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first target was written before preflight completed: %v", err)
	}
}

func TestRunSetupRefusesPluginManagedExecutablePath(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())
	err := runSetup([]string{"--client=vscode"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "plugin-managed cache path") {
		t.Fatalf("expected plugin-managed setup refusal, got %v", err)
	}
}
