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
	if entry.Env["MEMENTO_GIT_MAX_POLL_SECONDS"] != "30" || entry.Env["MEMENTO_GIT_ERROR_MAX_POLL_SECONDS"] != "60" {
		t.Fatalf("expected adaptive Git polling caps, got %#v", entry.Env)
	}
	if entry.Env["MEMENTO_CHANGE_DETECTOR"] != "auto" {
		t.Fatalf("expected MEMENTO_CHANGE_DETECTOR=auto, got %#v", entry.Env)
	}
	if entry.Env["MEMENTO_CONTEXT_MAX_TOKENS"] != "7000" {
		t.Fatalf("expected MEMENTO_CONTEXT_MAX_TOKENS=7000, got %#v", entry.Env)
	}
	if entry.Env["MEMENTO_FEEDBACK_ENABLED"] != "false" {
		t.Fatalf("expected feedback to be explicitly disabled, got %#v", entry.Env)
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
	if !strings.Contains(stdout.String(), "memory_search") || !strings.Contains(stdout.String(), "memory_list") || !strings.Contains(stdout.String(), "Linked Git worktrees share") || !strings.Contains(stdout.String(), "active checkout as root") || !strings.Contains(stdout.String(), "repo_context") || !strings.Contains(stdout.String(), "repo_diff_context") || !strings.Contains(stdout.String(), "without paths") || !strings.Contains(stdout.String(), "staged, unstaged, and untracked") || !strings.Contains(stdout.String(), "bounded, redacted unified diff summary") || !strings.Contains(stdout.String(), "repo_outline") || !strings.Contains(stdout.String(), "Anchor durable notes") || !strings.Contains(stdout.String(), "prime MCP prompt") || !strings.Contains(stdout.String(), "intent") {
		t.Fatalf("expected repo_context guidance, got %q", stdout.String())
	}
}

func TestHandleCLICommandClaudeMD(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, exitCode := handleCLICommand([]string{"claude-md", "--print-only"}, &stdout, &stderr)
	if !handled {
		t.Fatal("expected command to be handled")
	}
	if exitCode != 0 {
		t.Fatalf("expected exitCode=0, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Recommended workflow") {
		t.Fatalf("expected guidance block, got %q", stdout.String())
	}
}

func TestCLIHelpTextMentionsClaudeMD(t *testing.T) {
	if !strings.Contains(cliHelpText(), "claude-md") {
		t.Fatal("expected help text to mention claude-md")
	}
}

func TestHandleCLICommandVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, exitCode := handleCLICommand([]string{"version"}, &stdout, &stderr)
	if !handled || exitCode != 0 || strings.TrimSpace(stdout.String()) == "" || stderr.Len() != 0 {
		t.Fatalf("version command = handled %v, exit %d, stdout %q, stderr %q", handled, exitCode, stdout.String(), stderr.String())
	}
}

func TestCLIHelpTextMentionsUpdate(t *testing.T) {
	if !strings.Contains(cliHelpText(), "update --check") || !strings.Contains(cliHelpText(), "version") {
		t.Fatal("expected help text to mention update checking and version")
	}
}
