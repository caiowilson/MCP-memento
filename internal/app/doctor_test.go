package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorClientsPassesForMatchingJSONRegistration(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "memento-mcp")
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "mcp.json")
	entry := buildClientConfig(mcpClient{}, exe)
	data, err := upsertConfig(config, entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(config, data); err != nil {
		t.Fatal(err)
	}
	clients := []mcpClient{{Name: "Test Client", Slug: "test", ConfigPath: config}}
	var out bytes.Buffer
	if err := doctorClients(clients, []string{"test"}, exe, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[PASS] binary") || !strings.Contains(out.String(), "[PASS] Test Client") || !strings.Contains(out.String(), "Healthy.") {
		t.Fatalf("unexpected doctor output: %s", out.String())
	}
}

func TestDoctorClientsFailsForStaleAndMissingRegistration(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "memento-mcp")
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "stale.json")
	if err := os.WriteFile(stale, []byte(`{"mcpServers":{"memento":{"command":"/other"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.json")
	clients := []mcpClient{
		{Name: "Stale", Slug: "stale", ConfigPath: stale},
		{Name: "Missing", Slug: "missing", ConfigPath: missing},
	}
	var out bytes.Buffer
	err := doctorClients(clients, []string{"stale", "missing"}, exe, &out)
	if err == nil || !strings.Contains(err.Error(), "2 diagnostic") {
		t.Fatalf("expected two failures, got %v", err)
	}
	if !strings.Contains(out.String(), "another executable") || !strings.Contains(out.String(), "not registered") {
		t.Fatalf("unexpected doctor output: %s", out.String())
	}
}

func TestDoctorFlagsAcceptAliasesAndRejectUnknown(t *testing.T) {
	opts, err := parseDoctorFlags([]string{"--clients=codex,claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.clients) != 2 || opts.clients[1] != "claude-code" {
		t.Fatalf("unexpected clients: %v", opts.clients)
	}
	if _, err := parseDoctorFlags([]string{"--network"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
}

func TestDoctorCommandDispatches(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := handleCLICommand([]string{"doctor", "--client=definitely-not-a-client"}, &stdout, &stderr)
	if !handled || code != 1 || !strings.Contains(stderr.String(), "unknown client selection") {
		t.Fatalf("handled=%v code=%d stdout=%q stderr=%q", handled, code, stdout.String(), stderr.String())
	}
}
