package gitstate

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// IgnoredPaths is a snapshot of untracked paths ignored by Git's standard
// exclude sources: nested .gitignore files, .git/info/exclude, and the user's
// core.excludesFile. Tracked files are intentionally absent, matching Git.
type IgnoredPaths struct {
	files      map[string]struct{}
	dirs       map[string]struct{}
	ready      bool
	failClosed bool
}

// LoadIgnoredPaths returns an empty snapshot outside a Git worktree. Git
// availability failures are treated as an empty snapshot so repository tools
// remain useful when Git is unavailable. Safety-limit failures match every
// path fail-closed so contextless callers cannot expose ignored content.
func LoadIgnoredPaths(root string) *IgnoredPaths {
	out, err := LoadIgnoredPathsContext(context.Background(), root)
	if err != nil {
		out.ready = true
		out.failClosed = true
	}
	return out
}

// LoadIgnoredPathsContext returns the same bounded snapshot while honoring
// cancellation. Ordinary Git availability failures preserve the historical
// empty-snapshot behavior; output-limit failures are returned fail-closed.
func LoadIgnoredPathsContext(ctx context.Context, root string) (*IgnoredPaths, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	out := &IgnoredPaths{files: map[string]struct{}{}, dirs: map[string]struct{}{}}
	raw, err := runGit(ctx, root, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return out, ctxErr
		}
		if errors.Is(err, errGitOutputLimit) {
			return out, err
		}
		return out, nil
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
	return out, nil
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
	if i.failClosed {
		return true
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
