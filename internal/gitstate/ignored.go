package gitstate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IgnoredPaths is a snapshot of untracked paths ignored by Git's standard
// exclude sources: nested .gitignore files, .git/info/exclude, and the user's
// core.excludesFile. Tracked files are intentionally absent, matching Git.
type IgnoredPaths struct {
	files map[string]struct{}
	dirs  map[string]struct{}
	ready bool
}

// LoadIgnoredPaths returns an empty snapshot outside a Git worktree. Git
// failures are treated as an empty snapshot so repository tools remain useful
// when Git is unavailable; built-in sensitive-path filters still apply.
func LoadIgnoredPaths(root string) *IgnoredPaths {
	out := &IgnoredPaths{files: map[string]struct{}{}, dirs: map[string]struct{}{}}
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	raw, err := cmd.Output()
	if err != nil {
		return out
	}
	out.ready = true
	for _, field := range bytes.Split(raw, []byte{0}) {
		path := filepath.ToSlash(filepath.Clean(string(field)))
		if path == "" || path == "." || strings.HasPrefix(path, "../") {
			continue
		}
		if bytes.HasSuffix(field, []byte{'/'}) {
			out.dirs[path] = struct{}{}
			continue
		}
		out.files[path] = struct{}{}
	}
	return out
}

// Available reports whether Git produced the snapshot for this worktree.
func (i *IgnoredPaths) Available() bool {
	return i != nil && i.ready
}

// Matches reports whether rel or one of its parent directories is ignored.
func (i *IgnoredPaths) Matches(rel string) bool {
	if i == nil {
		return false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") {
		return false
	}
	if _, ok := i.files[rel]; ok {
		return true
	}
	for parent := rel; parent != "." && parent != ""; parent = filepath.ToSlash(filepath.Dir(parent)) {
		if _, ok := i.dirs[parent]; ok {
			return true
		}
	}
	return false
}
