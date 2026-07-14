package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type clientKind uint8

const (
	jsonClient clientKind = iota
	cliClient
)

const canonicalServerName = "memento"

// mcpClient describes a known MCP client and how to configure it.
type mcpClient struct {
	Name       string
	Slug       string
	Kind       clientKind
	ConfigPath string
	HasCwd     bool
	CwdVar     string
	CLI        string
}

type clientState struct {
	Status  string
	Name    string
	Command string
}

var (
	clientLookPath = exec.LookPath
	clientCommand  = func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
)

func knownClients(exe string) []mcpClient {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}

	clients := []mcpClient{
		{Name: "Codex", Slug: "codex", Kind: cliClient, CLI: "codex"},
		{Name: "Claude Code", Slug: "claude-code", Kind: cliClient, CLI: "claude", ConfigPath: claudeCodeConfigPath(home)},
		{Name: "VS Code", Slug: "vscode", ConfigPath: vscodeMCPConfigPath(home), HasCwd: true, CwdVar: "${workspaceFolder}"},
		{Name: "Cursor", Slug: "cursor", ConfigPath: filepath.Join(home, ".cursor", "mcp.json"), HasCwd: true, CwdVar: "${workspaceFolder}"},
		{Name: "Claude Desktop", Slug: "claude-desktop", ConfigPath: claudeDesktopConfigPath(home)},
		{Name: "Windsurf", Slug: "windsurf", ConfigPath: windsurfMCPConfigPath(home), HasCwd: true, CwdVar: "${workspaceFolder}"},
	}

	valid := make([]mcpClient, 0, len(clients))
	for _, c := range clients {
		if c.Kind == cliClient || c.ConfigPath != "" {
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
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "Code", "User", "mcp.json")
		}
	}
	return ""
}

func claudeCodeConfigPath(home string) string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	return filepath.Join(home, ".claude.json")
}

func claudeDesktopConfigPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "linux":
		return filepath.Join(home, ".config", "claude", "claude_desktop_config.json")
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "Claude", "claude_desktop_config.json")
		}
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
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "Windsurf", "mcp.json")
		}
	}
	return ""
}

func buildClientConfig(c mcpClient, command string) map[string]any {
	entry := map[string]any{
		"command": command,
		"args":    []string{},
		"env":     defaultMCPEnv,
	}
	if c.HasCwd {
		entry["name"] = canonicalServerName
		entry["transport"] = "stdio"
		entry["cwd"] = c.CwdVar
	}
	return entry
}

func inspectJSONConfig(path, command string) (clientState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return clientState{Status: "not configured"}, nil
	}
	if err != nil {
		return clientState{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return clientState{}, errors.New("configuration is empty")
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return clientState{}, fmt.Errorf("invalid JSON: %w", err)
	}
	serversValue, exists := root["mcpServers"]
	if !exists {
		return clientState{Status: "not configured"}, nil
	}
	servers, ok := serversValue.(map[string]any)
	if !ok {
		return clientState{}, errors.New("mcpServers must be a JSON object")
	}
	return inspectServerEntries(servers, command)
}

func inspectServerEntries(servers map[string]any, command string) (clientState, error) {
	_, hasCanonical := servers[canonicalServerName]
	_, hasLegacy := servers["memento-mcp"]
	if hasCanonical && hasLegacy {
		return clientState{Status: "conflict"}, errors.New("both memento and memento-mcp entries exist")
	}
	name := canonicalServerName
	if !hasCanonical && hasLegacy {
		name = "memento-mcp"
	} else if !hasCanonical {
		return clientState{Status: "not configured"}, nil
	}
	entry, ok := servers[name].(map[string]any)
	if !ok {
		return clientState{}, fmt.Errorf("%s entry must be a JSON object", name)
	}
	configuredCommand, ok := entry["command"].(string)
	if !ok || configuredCommand == "" {
		return clientState{}, fmt.Errorf("%s command must be a non-empty string", name)
	}
	status := "stale"
	if command == "" || sameExecutable(configuredCommand, command) {
		status = "configured"
	}
	return clientState{Status: status, Name: name, Command: configuredCommand}, nil
}

func inspectCLIClient(c mcpClient, command string) (clientState, error) {
	if _, err := clientLookPath(c.CLI); err != nil {
		return clientState{Status: "unavailable"}, nil
	}
	if c.Slug == "claude-code" {
		return inspectJSONConfig(c.ConfigPath, command)
	}
	if c.Slug != "codex" {
		return clientState{}, fmt.Errorf("unsupported CLI client %q", c.Slug)
	}

	out, err := clientCommand(c.CLI, "mcp", "list", "--json")
	if err != nil {
		return clientState{}, fmt.Errorf("query Codex MCP registrations: %w", err)
	}
	var entries []struct {
		Name      string `json:"name"`
		Transport struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"transport"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return clientState{}, fmt.Errorf("parse Codex MCP registrations: %w", err)
	}
	servers := map[string]any{}
	for _, entry := range entries {
		if entry.Name != canonicalServerName && entry.Name != "memento-mcp" {
			continue
		}
		if entry.Transport.Type != "stdio" {
			return clientState{}, fmt.Errorf("%s is not a stdio server", entry.Name)
		}
		servers[entry.Name] = map[string]any{"command": entry.Transport.Command}
	}
	return inspectServerEntries(servers, command)
}

func inspectClient(c mcpClient, command string) (clientState, error) {
	if c.Kind == cliClient {
		return inspectCLIClient(c, command)
	}
	return inspectJSONConfig(c.ConfigPath, command)
}

func sameExecutable(a, b string) bool {
	cleanA, errA := filepath.Abs(a)
	cleanB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		if runtime.GOOS == "windows" {
			return strings.EqualFold(filepath.Clean(cleanA), filepath.Clean(cleanB))
		}
		return filepath.Clean(cleanA) == filepath.Clean(cleanB)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func upsertConfig(path string, entry map[string]any) ([]byte, error) {
	return upsertNamedConfig(path, entry, canonicalServerName)
}

func upsertNamedConfig(path string, entry map[string]any, preferredName string) ([]byte, error) {
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err == nil {
		if len(strings.TrimSpace(string(data))) == 0 {
			return nil, errors.New("configuration is empty")
		}
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	servers := map[string]any{}
	if value, ok := root["mcpServers"]; ok {
		var valid bool
		servers, valid = value.(map[string]any)
		if !valid {
			return nil, errors.New("mcpServers must be a JSON object")
		}
	}
	_, hasCanonical := servers[canonicalServerName]
	_, hasLegacy := servers["memento-mcp"]
	if hasCanonical && hasLegacy {
		return nil, errors.New("both memento and memento-mcp entries exist")
	}
	name := preferredName
	if hasCanonical {
		name = canonicalServerName
	} else if hasLegacy {
		name = "memento-mcp"
	}
	servers[name] = entry
	root["mcpServers"] = servers
	return json.MarshalIndent(root, "", "  ")
}

func writeConfig(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to replace symlinked configuration")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	wanted := append(append([]byte(nil), data...), '\n')
	return writeAtomicBytes(path, wanted, mode)
}

func writeAtomicBytes(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".memento-config-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

type setupOptions struct {
	clients   []string
	printOnly bool
	force     bool
}

func parseSetupFlags(args []string) (setupOptions, error) {
	var opts setupOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--print-only":
			opts.printOnly = true
		case a == "--force":
			opts.force = true
		case a == "--client" || a == "--clients":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			i++
			opts.clients = appendClientValues(opts.clients, args[i])
		case strings.HasPrefix(a, "--client="):
			opts.clients = appendClientValues(opts.clients, strings.TrimPrefix(a, "--client="))
		case strings.HasPrefix(a, "--clients="):
			opts.clients = appendClientValues(opts.clients, strings.TrimPrefix(a, "--clients="))
		default:
			return opts, fmt.Errorf("unknown option %q", a)
		}
	}
	return opts, nil
}

func appendClientValues(dst []string, value string) []string {
	for _, part := range strings.Split(value, ",") {
		part = normalizeClientSlug(part)
		if part != "" {
			dst = append(dst, part)
		}
	}
	return dst
}

func normalizeClientSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "claude" {
		return "claude-code"
	}
	return value
}

func runSetup(args []string, stdout, stderr io.Writer) error {
	opts, err := parseSetupFlags(args)
	if err != nil {
		return err
	}
	if os.Getenv("CLAUDE_PLUGIN_DATA") != "" && !opts.printOnly {
		return errors.New("refusing to persist a Claude plugin-managed cache path; run setup from a standalone installation")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	clients := knownClients(exe)
	if len(clients) == 0 {
		return errors.New("no supported MCP clients detected for this OS")
	}
	if len(opts.clients) > 0 {
		return setupNonInteractive(clients, opts, exe, stdout, stderr)
	}
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return errors.New("interactive setup requires a terminal; pass --clients=codex,claude or another explicit client list")
	}
	return setupInteractive(clients, opts, exe, stdout, stderr, os.Stdin)
}

func setupNonInteractive(clients []mcpClient, opts setupOptions, exe string, stdout, stderr io.Writer) error {
	selected, err := filterClients(clients, opts.clients)
	if err != nil {
		return err
	}
	return configureClients(selected, exe, opts, stdout)
}

func setupInteractive(clients []mcpClient, opts setupOptions, exe string, stdout, stderr io.Writer, stdin io.Reader) error {
	fmt.Fprintln(stdout, "memento-mcp setup")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Supported clients:")
	var detected []mcpClient
	for i, c := range clients {
		state, err := inspectClient(c, exe)
		status := state.Status
		if err != nil {
			status = "needs attention"
		} else if c.Kind == cliClient && state.Status != "unavailable" {
			detected = append(detected, c)
		} else if c.Kind == jsonClient {
			if _, statErr := os.Stat(c.ConfigPath); statErr == nil {
				detected = append(detected, c)
			}
		}
		location := c.ConfigPath
		if c.Kind == cliClient {
			location = c.CLI
		}
		fmt.Fprintf(stdout, "  [%d] %-18s (%s — %s)\n", i+1, c.Name, shortenPath(location), status)
	}
	fmt.Fprintln(stdout, "  [A] All detected clients")
	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, "Select clients to configure [A]: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	selected := detected
	if line != "" && !strings.EqualFold(line, "a") {
		selected = nil
		for _, part := range strings.Split(line, ",") {
			var idx int
			if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &idx); err == nil && idx >= 1 && idx <= len(clients) {
				selected = append(selected, clients[idx-1])
			}
		}
	}
	if len(selected) == 0 {
		fmt.Fprintln(stdout, "No clients selected.")
		return nil
	}
	fmt.Fprintln(stdout)
	return configureClients(selected, exe, opts, stdout)
}

type clientPlan struct {
	client mcpClient
	state  clientState
	data   []byte
}

func configureClients(clients []mcpClient, exe string, opts setupOptions, stdout io.Writer) error {
	plans := make([]clientPlan, 0, len(clients))
	for _, c := range clients {
		plan := clientPlan{client: c}
		if c.Kind == jsonClient {
			entry := buildClientConfig(c, exe)
			data, err := upsertConfig(c.ConfigPath, entry)
			if err != nil {
				return fmt.Errorf("%s: %w", c.Name, err)
			}
			if err := preflightConfigTarget(c.ConfigPath); err != nil {
				return fmt.Errorf("%s: %w", c.Name, err)
			}
			plan.data = data
		} else if !opts.printOnly {
			state, err := inspectCLIClient(c, exe)
			if err != nil {
				return fmt.Errorf("%s: %w", c.Name, err)
			}
			if state.Status == "unavailable" {
				return fmt.Errorf("%s: %s command is not installed", c.Name, c.CLI)
			}
			if state.Status == "stale" && !opts.force {
				return fmt.Errorf("%s: existing %s registration uses another command; rerun with --force to replace it", c.Name, state.Name)
			}
			plan.state = state
		}
		plans = append(plans, plan)
	}

	for _, plan := range plans {
		c := plan.client
		if opts.printOnly {
			if c.Kind == cliClient {
				fmt.Fprintf(stdout, "# %s\n%s mcp add %s -- %s\n\n", c.Name, c.CLI, canonicalServerName, shellQuote(exe))
				continue
			}
			fmt.Fprintf(stdout, "# %s (%s)\n%s\n\n", c.Name, c.ConfigPath, string(plan.data))
			continue
		}
		if c.Kind == cliClient {
			if plan.state.Status == "configured" {
				fmt.Fprintf(stdout, "  ✓ %-18s already configured\n", c.Name)
				continue
			}
			if err := configureCLIClient(c, plan.state, exe, opts.force); err != nil {
				return fmt.Errorf("%s: %w", c.Name, err)
			}
			fmt.Fprintf(stdout, "  ✓ %-18s registered with %s\n", c.Name, c.CLI)
			continue
		}
		current, _ := os.ReadFile(c.ConfigPath)
		wanted := append(append([]byte(nil), plan.data...), '\n')
		if string(current) == string(wanted) {
			fmt.Fprintf(stdout, "  ✓ %-18s already configured\n", c.Name)
			continue
		}
		if err := writeConfig(c.ConfigPath, plan.data); err != nil {
			return fmt.Errorf("%s: write %s: %w", c.Name, c.ConfigPath, err)
		}
		fmt.Fprintf(stdout, "  ✓ %-18s wrote %s\n", c.Name, c.ConfigPath)
	}
	if !opts.printOnly {
		fmt.Fprintln(stdout, "\nDone. Restart active clients so they reconnect to Memento.")
	}
	return nil
}

func preflightConfigTarget(path string) error {
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to replace symlinked configuration")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func configureCLIClient(c mcpClient, state clientState, exe string, force bool) error {
	name := state.Name
	if name == "" {
		name = canonicalServerName
	}
	if c.Slug == "codex" {
		_, err := clientCommand(c.CLI, "mcp", "add", name, "--", exe)
		return err
	}
	if c.Slug != "claude-code" {
		return fmt.Errorf("unsupported CLI client %q", c.Slug)
	}
	if state.Status != "stale" {
		_, err := clientCommand(c.CLI, "mcp", "add", "--scope", "user", name, "--", exe)
		return err
	}
	if !force {
		return errors.New("replacement requires --force")
	}
	snapshot, mode, existed, err := snapshotFile(c.ConfigPath)
	if err != nil {
		return err
	}
	if _, err := clientCommand(c.CLI, "mcp", "remove", name, "--scope", "user"); err != nil {
		return err
	}
	if _, err := clientCommand(c.CLI, "mcp", "add", "--scope", "user", name, "--", exe); err != nil {
		if restoreErr := restoreFile(c.ConfigPath, snapshot, mode, existed); restoreErr != nil {
			return fmt.Errorf("replace registration: %v (restore failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace registration: %w (original configuration restored)", err)
	}
	return nil
}

func snapshotFile(path string) ([]byte, os.FileMode, bool, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, false, errors.New("refusing to replace symlinked configuration")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, 0, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0o600, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, false, err
	}
	return data, info.Mode().Perm(), true, nil
}

func restoreFile(path string, data []byte, mode os.FileMode, existed bool) error {
	if !existed {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomicBytes(path, data, mode)
}

func filterClients(all []mcpClient, slugs []string) ([]mcpClient, error) {
	want := map[string]bool{}
	selectAll := false
	for _, slug := range slugs {
		slug = normalizeClientSlug(slug)
		if slug == "all" {
			selectAll = true
			continue
		}
		want[slug] = true
	}
	if selectAll {
		return append([]mcpClient(nil), all...), nil
	}
	valid := map[string]bool{}
	var out []mcpClient
	for _, c := range all {
		valid[c.Slug] = true
		if want[c.Slug] {
			out = append(out, c)
		}
	}
	var invalid []string
	for slug := range want {
		if !valid[slug] {
			invalid = append(invalid, slug)
		}
	}
	if len(invalid) > 0 || len(out) == 0 {
		available := make([]string, 0, len(all))
		for _, c := range all {
			available = append(available, c.Slug)
		}
		return nil, fmt.Errorf("unknown client selection %v (valid: %s)", invalid, strings.Join(available, ", "))
	}
	return out, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func shortenPath(path string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
