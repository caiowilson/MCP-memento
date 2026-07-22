package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"memento-mcp/internal/mcp"
)

var defaultMCPEnv = map[string]string{
	"MEMENTO_CHANGE_DETECTOR":            "auto",
	"MEMENTO_INDEX_POLL_SECONDS":         "10",
	"MEMENTO_GIT_POLL_SECONDS":           "2",
	"MEMENTO_GIT_MAX_POLL_SECONDS":       "30",
	"MEMENTO_GIT_ERROR_MAX_POLL_SECONDS": "60",
	"MEMENTO_GIT_DEBOUNCE_MS":            "500",
	"MEMENTO_FS_DEBOUNCE_MS":             "500",
	"MEMENTO_CONTEXT_MAX_TOKENS":         "7000",
	"MEMENTO_REDACTION_ENABLED":          "true",
	"MEMENTO_FEEDBACK_ENABLED":           "false",
}

func handleCLICommand(args []string, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}

	switch args[0] {
	case "feedback":
		if err := runFeedbackCommand(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "feedback: %v\n", err)
			return true, 1
		}
		return true, 0
	case "setup":
		if err := runSetup(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "setup: %v\n", err)
			return true, 1
		}
		return true, 0
	case "doctor":
		if err := runDoctor(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "doctor: %v\n", err)
			return true, 1
		}
		return true, 0
	case "claude-md":
		if err := runClaudeMD(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "claude-md: %v\n", err)
			return true, 1
		}
		return true, 0
	case "update":
		if err := runUpdate(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "update: %v\n", err)
			return true, 1
		}
		return true, 0
	case "version", "--version":
		fmt.Fprintln(stdout, mcp.ServerVersion())
		return true, 0
	case "print-config":
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "resolve executable: %v\n", err)
			return true, 1
		}
		text, err := buildMCPServersConfigJSON(exe)
		if err != nil {
			fmt.Fprintf(stderr, "build config: %v\n", err)
			return true, 1
		}
		fmt.Fprintln(stdout, text)
		return true, 0
	case "print-guidance":
		fmt.Fprintln(stdout, clientGuidanceText())
		return true, 0
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, cliHelpText())
		return true, 0
	default:
		fmt.Fprintln(stderr, cliHelpText())
		return true, 2
	}
}

func buildMCPServersConfigJSON(command string) (string, error) {
	entry := map[string]any{
		"name":      "memento-mcp",
		"transport": "stdio",
		"command":   command,
		"args":      []string{},
		"cwd":       "${workspaceFolder}",
		"env":       defaultMCPEnv,
	}
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"memento-mcp": entry,
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func clientGuidanceText() string {
	return `When using memento-mcp, start with repo_context and set intent to navigate, implement, or review.
Use repo_diff_context without paths to auto-detect staged, unstaged, and untracked Git changes, or pass a non-empty ordered path list to override detection; it returns exact-file chunks and a bounded, redacted unified diff summary without related-file expansion.
Use repo_outline when you need signatures and file structure without implementation bodies.
Anchor durable notes to code when possible. Verify stale notes before refreshing or tombstoning them.
Use the prime MCP prompt at session start and explicit note/file resources when the client supports native prompts and @-mentions.
Omit mode unless you need to force a low-level output such as full, outline, or summary.
If repo_context returns suggestedNextCall, prefer following it for a deeper read without repeating context.
When you change repositories in the same MCP session, call repo_switch_workspace with the new root path instead of restarting.
Existing explicit mode calls still work, but new callers should prefer intent.`
}

func cliHelpText() string {
	return `memento-mcp

Usage:
  memento-mcp               Start the MCP stdio server and auto-detect the workspace root
  memento-mcp --root DIR    Start the server using DIR as workspace root
  memento-mcp setup         Detect MCP clients and write config (interactive)
  memento-mcp setup --clients=codex,claude
                            Configure Codex and Claude Code (non-interactive)
  memento-mcp setup --client=vscode --client=cursor
                            Configure specific clients (non-interactive)
  memento-mcp setup --print-only
                            Print config to stdout without writing files
  memento-mcp setup --force  Replace a conflicting CLI registration after preflight
  memento-mcp doctor         Validate the binary and configured clients
  memento-mcp doctor --clients=codex,claude
                            Validate specific client registrations
  memento-mcp claude-md     Write or update Memento guidance in ./CLAUDE.local.md
  memento-mcp claude-md --print-only
                            Print the guidance block without writing
  memento-mcp update        Download, verify, and install the latest server release
  memento-mcp update --check
                            Check whether a newer release is available
  memento-mcp version       Print the installed server version
  memento-mcp print-config  Print a generic mcpServers config JSON snippet
  memento-mcp print-guidance
                            Print copyable LLM guidance for repo_context intent routing
  memento-mcp feedback status
                            Show opt-in state, local storage path, and event count
  memento-mcp feedback export [--evaluation]
                            Export aggregate diagnostics or an evaluation supplement
  memento-mcp feedback delete --confirm
                            Permanently delete all locally stored feedback events
  memento-mcp help          Show this help text

Workspace root precedence:
  --root DIR -> CLAUDE_PROJECT_DIR -> MCP roots/list -> current working directory

Feedback is disabled unless MEMENTO_FEEDBACK_ENABLED=true. It is local-only and
never sends data over the network.`
}
