package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

var defaultMCPEnv = map[string]string{
	"MEMENTO_INDEX_POLL_SECONDS": "10",
	"MEMENTO_GIT_POLL_SECONDS":   "2",
	"MEMENTO_GIT_DEBOUNCE_MS":    "500",
	"MEMENTO_FS_DEBOUNCE_MS":     "500",
}

func handleCLICommand(args []string, stdout, stderr io.Writer) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}

	switch args[0] {
	case "setup":
		if err := runSetup(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "setup: %v\n", err)
			return true, 1
		}
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
Omit mode unless you need to force a low-level output such as full, outline, or summary.
If repo_context returns suggestedNextCall, prefer following it for a deeper read without repeating context.
When you change repositories in the same MCP session, call repo_switch_workspace with the new root path instead of restarting.
Existing explicit mode calls still work, but new callers should prefer intent.`
}

func cliHelpText() string {
	return `memento-mcp

Usage:
  memento-mcp               Start the MCP stdio server in the current working directory
  memento-mcp --root DIR    Start the server using DIR as workspace root (default: cwd)
  memento-mcp setup         Detect MCP clients and write config (interactive)
  memento-mcp setup --client=vscode --client=cursor
                            Configure specific clients (non-interactive)
  memento-mcp setup --print-only
                            Print config to stdout without writing files
  memento-mcp print-config  Print a generic mcpServers config JSON snippet
  memento-mcp print-guidance
                            Print copyable LLM guidance for repo_context intent routing
  memento-mcp help          Show this help text`
}
