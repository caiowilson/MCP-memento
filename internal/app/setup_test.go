package app

import (
	"bytes"
	"encoding/json"
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

func TestParseSetupFlags(t *testing.T) {
	opts := parseSetupFlags([]string{"--client=vscode", "--client=cursor", "--print-only"})
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
	if cfg["name"] != "memento-mcp" {
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
	if _, ok := servers["memento-mcp"]; !ok {
		t.Error("expected memento-mcp entry")
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
	if _, ok := servers["memento-mcp"]; !ok {
		t.Error("upsert should add memento-mcp")
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

	err := configureClients(clients, "/bin/memento-mcp", true, &buf)
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

	err := configureClients(clients, "/bin/memento-mcp", false, &buf)
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

	got := filterClients(all, []string{"vscode", "claude-desktop"})
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
	if !strings.Contains(err.Error(), "no matching clients") {
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
