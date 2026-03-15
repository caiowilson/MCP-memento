package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// mcpClient describes a known MCP client and how to generate its config.
type mcpClient struct {
	Name       string
	Slug       string // CLI identifier: vscode, cursor, claude-desktop, windsurf
	ConfigPath string // resolved absolute path
	HasCwd     bool   // whether the client supports a "cwd" field
	CwdVar     string // variable name for workspace root (e.g. "${workspaceFolder}")
}

func knownClients(exe string) []mcpClient {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}

	clients := []mcpClient{
		{
			Name:       "VS Code",
			Slug:       "vscode",
			ConfigPath: vscodeMCPConfigPath(home),
			HasCwd:     true,
			CwdVar:     "${workspaceFolder}",
		},
		{
			Name:       "Cursor",
			Slug:       "cursor",
			ConfigPath: filepath.Join(home, ".cursor", "mcp.json"),
			HasCwd:     true,
			CwdVar:     "${workspaceFolder}",
		},
		{
			Name:       "Claude Desktop",
			Slug:       "claude-desktop",
			ConfigPath: claudeDesktopConfigPath(home),
			HasCwd:     false,
		},
		{
			Name:       "Windsurf",
			Slug:       "windsurf",
			ConfigPath: windsurfMCPConfigPath(home),
			HasCwd:     true,
			CwdVar:     "${workspaceFolder}",
		},
	}

	// Filter out clients with empty config paths (unsupported OS).
	var valid []mcpClient
	for _, c := range clients {
		if c.ConfigPath != "" {
			valid = append(valid, c)
		}
	}
	return valid
}

func vscodeMCPConfigPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")
	case "linux":
		return filepath.Join(home, ".config", "Code", "User", "mcp.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return ""
		}
		return filepath.Join(appdata, "Code", "User", "mcp.json")
	}
	return ""
}

func claudeDesktopConfigPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "linux":
		return filepath.Join(home, ".config", "claude", "claude_desktop_config.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return ""
		}
		return filepath.Join(appdata, "Claude", "claude_desktop_config.json")
	}
	return ""
}

func windsurfMCPConfigPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, ".windsurf", "mcp.json")
	case "linux":
		return filepath.Join(home, ".config", "windsurf", "mcp.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return ""
		}
		return filepath.Join(appdata, "Windsurf", "mcp.json")
	}
	return ""
}

// configStatus returns "configured" if the file exists and contains "memento-mcp",
// "exists" if it exists but has no entry, or "not found".
func configStatus(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "not found"
	}
	if strings.Contains(string(data), "memento-mcp") {
		return "configured"
	}
	return "exists"
}

// buildClientConfig generates the mcpServers JSON entry for a client.
func buildClientConfig(c mcpClient, command string) map[string]any {
	entry := map[string]any{
		"command": command,
		"args":    []string{},
		"env":     defaultMCPEnv,
	}
	if c.HasCwd {
		entry["name"] = "memento-mcp"
		entry["transport"] = "stdio"
		entry["cwd"] = c.CwdVar
	}
	return entry
}

// upsertConfig reads an existing config file (or starts fresh), adds/updates
// the memento-mcp entry in mcpServers, and returns the updated JSON bytes.
func upsertConfig(path string, entry map[string]any) ([]byte, error) {
	var root map[string]any

	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			// File exists but isn't valid JSON — start fresh but warn.
			root = nil
		}
	}
	if root == nil {
		root = map[string]any{}
	}

	servers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
	}
	servers["memento-mcp"] = entry
	root["mcpServers"] = servers

	return json.MarshalIndent(root, "", "  ")
}

// writeConfig writes data to a config file, creating parent dirs as needed.
func writeConfig(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// setupOptions holds parsed flags for the setup command.
type setupOptions struct {
	clients   []string // explicit --client values
	printOnly bool     // --print-only
}

func parseSetupFlags(args []string) setupOptions {
	var opts setupOptions
	for _, a := range args {
		if a == "--print-only" {
			opts.printOnly = true
		} else if strings.HasPrefix(a, "--client=") {
			opts.clients = append(opts.clients, strings.TrimPrefix(a, "--client="))
		}
	}
	return opts
}

// runSetup is the entry point for `memento-mcp setup`.
func runSetup(args []string, stdout, stderr io.Writer) error {
	opts := parseSetupFlags(args)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	clients := knownClients(exe)
	if len(clients) == 0 {
		return fmt.Errorf("no supported MCP clients detected for this OS")
	}

	// Non-interactive: --client flags specified
	if len(opts.clients) > 0 {
		return setupNonInteractive(clients, opts, exe, stdout, stderr)
	}

	// Interactive
	return setupInteractive(clients, opts, exe, stdout, stderr, os.Stdin)
}

func setupNonInteractive(clients []mcpClient, opts setupOptions, exe string, stdout, stderr io.Writer) error {
	selected := filterClients(clients, opts.clients)
	if len(selected) == 0 {
		valid := make([]string, len(clients))
		for i, c := range clients {
			valid[i] = c.Slug
		}
		return fmt.Errorf("no matching clients for %v (valid: %s)", opts.clients, strings.Join(valid, ", "))
	}
	return configureClients(selected, exe, opts.printOnly, stdout)
}

func setupInteractive(clients []mcpClient, opts setupOptions, exe string, stdout, stderr io.Writer, stdin io.Reader) error {
	fmt.Fprintln(stdout, "memento-mcp setup")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Detected clients:")

	for i, c := range clients {
		status := configStatus(c.ConfigPath)
		fmt.Fprintf(stdout, "  [%d] %-18s (%s — %s)\n", i+1, c.Name, shortenPath(c.ConfigPath), status)
	}

	fmt.Fprintln(stdout, "  [A] All detected clients")
	fmt.Fprintln(stdout, "")
	fmt.Fprintf(stdout, "Select clients to configure [A]: ")

	reader := bufio.NewReader(stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)

	var selected []mcpClient
	if line == "" || strings.EqualFold(line, "a") {
		selected = clients
	} else {
		for _, part := range strings.Split(line, ",") {
			part = strings.TrimSpace(part)
			idx := 0
			if _, err := fmt.Sscanf(part, "%d", &idx); err == nil && idx >= 1 && idx <= len(clients) {
				selected = append(selected, clients[idx-1])
			}
		}
	}

	if len(selected) == 0 {
		fmt.Fprintln(stdout, "No clients selected.")
		return nil
	}

	fmt.Fprintln(stdout, "")
	return configureClients(selected, exe, opts.printOnly, stdout)
}

func configureClients(clients []mcpClient, exe string, printOnly bool, stdout io.Writer) error {
	for _, c := range clients {
		entry := buildClientConfig(c, exe)

		if printOnly {
			cfg := map[string]any{
				"mcpServers": map[string]any{
					"memento-mcp": entry,
				},
			}
			b, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "# %s (%s)\n%s\n\n", c.Name, c.ConfigPath, string(b))
			continue
		}

		data, err := upsertConfig(c.ConfigPath, entry)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		if err := writeConfig(c.ConfigPath, data); err != nil {
			return fmt.Errorf("%s: write %s: %w", c.Name, c.ConfigPath, err)
		}
		fmt.Fprintf(stdout, "  ✓ %-18s wrote %s\n", c.Name, c.ConfigPath)
	}

	if !printOnly {
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Done! Restart your IDE to activate memento-mcp.")
	}
	return nil
}

func filterClients(all []mcpClient, slugs []string) []mcpClient {
	want := map[string]bool{}
	for _, s := range slugs {
		want[strings.ToLower(strings.TrimSpace(s))] = true
	}
	var out []mcpClient
	for _, c := range all {
		if want[c.Slug] {
			out = append(out, c)
		}
	}
	return out
}

func shortenPath(path string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
