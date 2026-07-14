package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"memento-mcp/internal/mcp"
)

const (
	releasesAPIURL  = "https://api.github.com/repos/caiowilson/MCP-memento/releases?per_page=100"
	updateUserAgent = "memento-mcp-update-check"
	maxBinarySize   = 256 << 20
	updateInterval  = 24 * time.Hour
)

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type serverRelease struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

func (r serverRelease) version() string {
	return strings.TrimPrefix(r.TagName, "server/v")
}

type updater struct {
	client         *http.Client
	releasesURL    string
	currentVersion string
	goos           string
	goarch         string
	executable     func() (string, error)
	validate       func(path, expectedVersion string) error
}

func defaultUpdater() updater {
	return updater{
		client:         &http.Client{Timeout: 10 * time.Second},
		releasesURL:    releasesAPIURL,
		currentVersion: mcp.ServerVersion(),
		goos:           runtime.GOOS,
		goarch:         runtime.GOARCH,
		executable:     os.Executable,
		validate:       validateReleaseBinary,
	}
}

func runUpdate(args []string, stdout io.Writer) error {
	checkOnly := false
	for _, arg := range args {
		switch arg {
		case "--check":
			checkOnly = true
		default:
			return fmt.Errorf("unknown option %q (supported: --check)", arg)
		}
	}

	pluginManaged := os.Getenv("CLAUDE_PLUGIN_DATA") != ""
	if !checkOnly && pluginManaged {
		return errors.New("this installation is managed by the Claude Code plugin; run /plugin marketplace update memento-mcp, /plugin update memento@memento-mcp, then /reload-plugins")
	}

	u := defaultUpdater()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := u.latestRelease(ctx)
	if err != nil {
		return err
	}

	comparison, comparable := compareVersions(release.version(), u.currentVersion)
	if comparable && comparison <= 0 {
		fmt.Fprintf(stdout, "memento-mcp %s is up to date.\n", u.currentVersion)
		return nil
	}
	if checkOnly {
		if pluginManaged {
			fmt.Fprintf(stdout, "Update available: %s -> %s. Update the Claude Code plugin with /plugin marketplace update memento-mcp, /plugin update memento@memento-mcp, then /reload-plugins.\n", u.currentVersion, release.version())
			return nil
		}
		fmt.Fprintf(stdout, "Update available: %s -> %s. Run 'memento-mcp update'.\n", u.currentVersion, release.version())
		return nil
	}

	target, err := u.executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	if err := u.install(ctx, release, target); err != nil {
		return err
	}
	if u.goos == "windows" {
		fmt.Fprintf(stdout, "Verified memento-mcp %s; installation will finish as this command exits.\n", release.version())
		return nil
	}
	fmt.Fprintf(stdout, "Updated memento-mcp from %s to %s.\n", u.currentVersion, release.version())
	return nil
}

func (u updater) latestRelease(ctx context.Context) (serverRelease, error) {
	var releases []serverRelease
	if err := u.getJSON(ctx, u.releasesURL, &releases); err != nil {
		return serverRelease{}, fmt.Errorf("check latest release: %w", err)
	}
	var latest serverRelease
	for _, release := range releases {
		if release.Draft || release.Prerelease || !strings.HasPrefix(release.TagName, "server/v") {
			continue
		}
		if _, ok := compareVersions(release.version(), release.version()); !ok {
			continue
		}
		if latest.TagName == "" {
			latest = release
			continue
		}
		if comparison, ok := compareVersions(release.version(), latest.version()); ok && comparison > 0 {
			latest = release
		}
	}
	if latest.TagName != "" {
		return latest, nil
	}
	return serverRelease{}, errors.New("check latest release: no published server release found")
}

func (u updater) getJSON(ctx context.Context, url string, dst any) error {
	resp, err := u.request(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(dst)
}

func (u updater) request(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	return resp, nil
}

func (u updater) install(ctx context.Context, release serverRelease, target string) error {
	assetName, err := binaryAssetName(u.goos, u.goarch)
	if err != nil {
		return err
	}
	binaryURL, checksumURL := "", ""
	for _, asset := range release.Assets {
		switch asset.Name {
		case assetName:
			binaryURL = asset.URL
		case assetName + ".sha256":
			checksumURL = asset.URL
		}
	}
	if binaryURL == "" || checksumURL == "" {
		return fmt.Errorf("release %s does not contain %s and its checksum", release.TagName, assetName)
	}

	want, err := u.fetchChecksum(ctx, checksumURL, assetName)
	if err != nil {
		return fmt.Errorf("download checksum: %w", err)
	}

	dir := filepath.Dir(target)
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlinked executable %s", target)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect executable %s: %w", target, err)
	}
	tmp, err := os.CreateTemp(dir, ".memento-mcp-update-*")
	if err != nil {
		return fmt.Errorf("create update beside %s: %w", target, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	resp, err := u.request(ctx, binaryURL)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download release binary: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, maxBinarySize+1))
	closeErr := resp.Body.Close()
	if copyErr == nil && closeErr != nil {
		copyErr = closeErr
	}
	if copyErr == nil && written > maxBinarySize {
		copyErr = fmt.Errorf("release binary exceeds %d bytes", maxBinarySize)
	}
	if copyErr != nil {
		tmp.Close()
		return fmt.Errorf("download release binary: %w", copyErr)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, want) {
		tmp.Close()
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}

	mode := os.FileMode(0o755)
	if info, statErr := os.Stat(target); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("set executable permissions: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync update: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close update: %w", err)
	}
	if u.validate != nil {
		if err := u.validate(tmpPath, release.version()); err != nil {
			return fmt.Errorf("validate staged update: %w", err)
		}
	}
	if err := replaceExecutable(tmpPath, target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	return nil
}

func (u updater) fetchChecksum(ctx context.Context, url, assetName string) (string, error) {
	resp, err := u.request(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) != 2 || len(fields[0]) != sha256.Size*2 || strings.TrimPrefix(fields[1], "*") != assetName {
		return "", errors.New("invalid SHA-256 sidecar")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("invalid SHA-256 sidecar")
	}
	return strings.ToLower(fields[0]), nil
}

func validateReleaseBinary(path, expectedVersion string) error {
	out, err := exec.Command(path, "version").Output()
	if err != nil {
		return err
	}
	got := strings.TrimSpace(string(out))
	if got != expectedVersion {
		return fmt.Errorf("reported version %q, expected %q", got, expectedVersion)
	}
	return nil
}

func binaryAssetName(goos, goarch string) (string, error) {
	if goos != "darwin" && goos != "linux" && goos != "windows" {
		return "", fmt.Errorf("unsupported operating system %q", goos)
	}
	arch := goarch
	if goarch == "amd64" {
		arch = "x64"
	}
	if arch != "x64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported architecture %q", goarch)
	}
	name := "memento-mcp_" + goos + "_" + arch
	if goos == "windows" {
		name += ".exe"
	}
	return name, nil
}

func compareVersions(a, b string) (int, bool) {
	parse := func(version string) ([]int, bool) {
		version = strings.TrimPrefix(strings.TrimSpace(version), "v")
		if strings.ContainsAny(version, "+-") {
			return nil, false
		}
		parts := strings.Split(version, ".")
		if len(parts) != 3 {
			return nil, false
		}
		values := make([]int, len(parts))
		for i, part := range parts {
			value, err := strconv.Atoi(part)
			if err != nil || value < 0 {
				return nil, false
			}
			values[i] = value
		}
		return values, true
	}
	av, aok := parse(a)
	bv, bok := parse(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range av {
		if av[i] < bv[i] {
			return -1, true
		}
		if av[i] > bv[i] {
			return 1, true
		}
	}
	return 0, true
}

type updateCheckCache struct {
	CheckedAt     time.Time `json:"checkedAt"`
	LatestVersion string    `json:"latestVersion"`
}

func startUpdateNotice(stderr io.Writer) {
	if !updateChecksEnabled() || mcp.ServerVersion() == "dev" {
		return
	}
	cachePath, err := updateCachePath()
	if err != nil {
		return
	}
	if cached, ok := readUpdateCache(cachePath); ok && time.Since(cached.CheckedAt) < updateInterval {
		writeUpdateNotice(stderr, mcp.ServerVersion(), cached.LatestVersion, os.Getenv("CLAUDE_PLUGIN_DATA") != "")
		return
	}
	go func() {
		u := defaultUpdater()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		release, err := u.latestRelease(ctx)
		if err != nil {
			return
		}
		cache := updateCheckCache{CheckedAt: time.Now().UTC(), LatestVersion: release.version()}
		_ = writeUpdateCache(cachePath, cache)
		writeUpdateNotice(stderr, u.currentVersion, cache.LatestVersion, os.Getenv("CLAUDE_PLUGIN_DATA") != "")
	}()
}

func updateChecksEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MEMENTO_UPDATE_CHECK"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func updateCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".memento-mcp", "update-check.json"), nil
}

func readUpdateCache(path string) (updateCheckCache, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return updateCheckCache{}, false
	}
	var cache updateCheckCache
	if json.Unmarshal(b, &cache) != nil || cache.CheckedAt.IsZero() || cache.LatestVersion == "" {
		return updateCheckCache{}, false
	}
	return cache, true
}

func writeUpdateCache(path string, cache updateCheckCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-check-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func writeUpdateNotice(w io.Writer, current, latest string, pluginManaged bool) {
	if comparison, ok := compareVersions(latest, current); ok && comparison > 0 {
		if pluginManaged {
			fmt.Fprintf(w, "memento-mcp: update available (%s -> %s); update the Claude Code plugin via /plugin\n", current, latest)
			return
		}
		fmt.Fprintf(w, "memento-mcp: update available (%s -> %s); run 'memento-mcp update'\n", current, latest)
	}
}
