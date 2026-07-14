package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.10.0", "0.9.0", 1, true},
		{"1.2.3", "1.2.3", 0, true},
		{"1.2.2", "1.2.3", -1, true},
		{"dev", "1.2.3", 0, false},
	}
	for _, tt := range tests {
		got, ok := compareVersions(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("compareVersions(%q, %q) = (%d, %v), want (%d, %v)", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

func TestUpdaterFindsLatestServerReleaseAndInstallsVerifiedAsset(t *testing.T) {
	binary := []byte("new memento binary")
	sum := sha256.Sum256(binary)
	checksum := hex.EncodeToString(sum[:])

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			fmt.Fprintf(w, `[{"tag_name":"server/vnot-semver"},{"tag_name":"server/v9.0.0","prerelease":true},{"tag_name":"extension/v9.0.0"},{"tag_name":"server/v1.1.0"},{"tag_name":"server/v1.2.0","assets":[{"name":"memento-mcp_darwin_arm64","browser_download_url":"%s/binary"},{"name":"memento-mcp_darwin_arm64.sha256","browser_download_url":"%s/checksum"}]}]`, server.URL, server.URL)
		case "/binary":
			w.Write(binary)
		case "/checksum":
			fmt.Fprintf(w, "%s  memento-mcp_darwin_arm64\n", checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "memento-mcp")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := updater{client: server.Client(), releasesURL: server.URL + "/releases", currentVersion: "1.1.0", goos: "darwin", goarch: "arm64"}
	release, err := u.latestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release.version() != "1.2.0" {
		t.Fatalf("latest version = %q", release.version())
	}
	if err := u.install(context.Background(), release, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("installed content = %q", got)
	}
}

func TestRunUpdateRejectsPluginManagedInstall(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())
	var out bytes.Buffer
	err := runUpdate(nil, &out)
	if err == nil || !strings.Contains(err.Error(), "managed by the Claude Code plugin") {
		t.Fatalf("expected plugin-managed error, got %v", err)
	}
}

func TestUpdaterRejectsChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprintln(w, strings.Repeat("0", 64), " memento-mcp_linux_x64")
			return
		}
		fmt.Fprint(w, "tampered")
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "memento-mcp")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	release := serverRelease{TagName: "server/v1.2.0", Assets: []releaseAsset{
		{Name: "memento-mcp_linux_x64", URL: server.URL + "/binary"},
		{Name: "memento-mcp_linux_x64.sha256", URL: server.URL + "/binary.sha256"},
	}}
	u := updater{client: server.Client(), goos: "linux", goarch: "amd64"}
	if err := u.install(context.Background(), release, target); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("target changed after rejected update: %q", got)
	}
}

func TestUpdaterRejectsStagedBinaryThatFailsVersionValidation(t *testing.T) {
	binary := []byte("new memento binary")
	sum := sha256.Sum256(binary)
	checksum := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprintf(w, "%s  memento-mcp_linux_x64\n", checksum)
			return
		}
		w.Write(binary)
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "memento-mcp")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	release := serverRelease{TagName: "server/v1.2.0", Assets: []releaseAsset{
		{Name: "memento-mcp_linux_x64", URL: server.URL + "/binary"},
		{Name: "memento-mcp_linux_x64.sha256", URL: server.URL + "/binary.sha256"},
	}}
	u := updater{
		client: server.Client(), goos: "linux", goarch: "amd64",
		validate: func(path, expected string) error {
			if expected != "1.2.0" {
				t.Fatalf("expected version = %q", expected)
			}
			return fmt.Errorf("candidate did not start")
		},
	}
	if err := u.install(context.Background(), release, target); err == nil || !strings.Contains(err.Error(), "validate staged update") {
		t.Fatalf("expected staged validation failure, got %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("target changed after staged validation failure: %q", got)
	}
}

func TestUpdateCacheAndNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "update-check.json")
	want := updateCheckCache{CheckedAt: time.Now().UTC().Truncate(time.Second), LatestVersion: "1.2.0"}
	if err := writeUpdateCache(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok := readUpdateCache(path)
	if !ok || got.LatestVersion != want.LatestVersion || !got.CheckedAt.Equal(want.CheckedAt) {
		t.Fatalf("read cache = %#v, %v", got, ok)
	}
	var out bytes.Buffer
	writeUpdateNotice(&out, "1.1.0", "1.2.0", false)
	if !strings.Contains(out.String(), "memento-mcp update") {
		t.Fatalf("notice = %q", out.String())
	}
	out.Reset()
	writeUpdateNotice(&out, "1.1.0", "1.2.0", true)
	if !strings.Contains(out.String(), "via /plugin") {
		t.Fatalf("plugin notice = %q", out.String())
	}
}
