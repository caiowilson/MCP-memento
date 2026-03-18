package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildMCPServersConfigJSON(t *testing.T) {
	text, err := buildMCPServersConfigJSON("/tmp/memento-mcp")
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Cwd     string            `json:"cwd"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatal(err)
	}

	entry, ok := decoded.MCPServers["memento-mcp"]
	if !ok {
		t.Fatal("expected memento-mcp entry")
	}
	if entry.Command != "/tmp/memento-mcp" {
		t.Fatalf("expected command path, got %q", entry.Command)
	}
	if entry.Cwd != "${workspaceFolder}" {
		t.Fatalf("expected cwd placeholder, got %q", entry.Cwd)
	}
	if entry.Env["MEMENTO_GIT_POLL_SECONDS"] != "2" {
		t.Fatalf("expected default env, got %#v", entry.Env)
	}
	if entry.Env["MEMENTO_CHANGE_DETECTOR"] != "auto" {
		t.Fatalf("expected MEMENTO_CHANGE_DETECTOR=auto, got %#v", entry.Env)
	}
}

func TestHandleCLICommandPrintGuidance(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, exitCode := handleCLICommand([]string{"print-guidance"}, &stdout, &stderr)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	if exitCode != 0 {
		t.Fatalf("expected exitCode=0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "repo_context") || !strings.Contains(stdout.String(), "intent") {
		t.Fatalf("expected repo_context guidance, got %q", stdout.String())
	}
}
