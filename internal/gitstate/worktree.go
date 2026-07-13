package gitstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	gitStatusOutputMaxBytes      = 8 * 1024 * 1024
	gitStatusErrorOutputMaxBytes = 8 * 1024
	WorktreeChangeModified       = "modified"
	WorktreeChangeAdded          = "added"
	WorktreeChangeDeleted        = "deleted"
	WorktreeChangeRenamed        = "renamed"
	WorktreeChangeCopied         = "copied"
	WorktreeChangeUntracked      = "untracked"
	WorktreeChangeUnmerged       = "unmerged"
	WorktreeChangeTypeChanged    = "type_changed"
)

var errGitOutputLimit = errors.New("Git output exceeded safety limit")

// WorktreeChange describes one entry from Git's porcelain v1 status output.
// Paths are slash-separated and relative to the requested workspace root.
type WorktreeChange struct {
	Path           string `json:"path"`
	PreviousPath   string `json:"previousPath,omitempty"`
	IndexStatus    string `json:"indexStatus"`
	WorktreeStatus string `json:"worktreeStatus"`
	Kind           string `json:"kind"`
	Staged         bool   `json:"staged"`
	Unstaged       bool   `json:"unstaged"`
	Untracked      bool   `json:"untracked"`
	Deleted        bool   `json:"deleted"`
	Renamed        bool   `json:"renamed"`
	Copied         bool   `json:"copied"`
}

// LoadWorktreeChanges returns deterministic structured Git status records for
// the requested workspace. A nested workspace is scoped to its own subtree.
func LoadWorktreeChanges(ctx context.Context, root string) ([]WorktreeChange, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefixBytes, err := runGit(ctx, root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, fmt.Errorf("resolve Git workspace prefix: %w", err)
	}
	prefix := filepath.ToSlash(strings.TrimSuffix(string(prefixBytes), "\n"))

	status, err := runGit(ctx, root,
		"status",
		"--porcelain=v1",
		"-z",
		"--renames",
		"--untracked-files=all",
		"--",
		".",
	)
	if err != nil {
		return nil, fmt.Errorf("read Git worktree status: %w", err)
	}
	return parsePorcelainV1Z(status, prefix)
}

func runGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	gitArgs := make([]string, 0, len(args)+3)
	gitArgs = append(gitArgs, "--literal-pathspecs", "-C", root)
	gitArgs = append(gitArgs, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	stdout := newBoundedGitBuffer(gitStatusOutputMaxBytes)
	stderr := newBoundedGitBuffer(gitStatusErrorOutputMaxBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if stderr.truncated {
			return nil, fmt.Errorf("%w: Git error output exceeded safety limit", err)
		}
		if message := strings.TrimSpace(stderr.buf.String()); message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stdout.truncated {
		return nil, fmt.Errorf("%w of %d bytes", errGitOutputLimit, gitStatusOutputMaxBytes)
	}
	return stdout.buf.Bytes(), nil
}

type boundedGitBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newBoundedGitBuffer(limit int) *boundedGitBuffer {
	if limit < 0 {
		limit = 0
	}
	return &boundedGitBuffer{limit: limit}
}

func (b *boundedGitBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buf.Write(data)
	}
	if original > remaining {
		b.truncated = true
	}
	return original, nil
}

func parsePorcelainV1Z(raw []byte, workspacePrefix string) ([]WorktreeChange, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[len(raw)-1] != 0 {
		return nil, errors.New("malformed Git porcelain output: missing NUL terminator")
	}

	workspacePrefix = filepath.ToSlash(workspacePrefix)
	fields := bytes.Split(raw[:len(raw)-1], []byte{0})
	changes := make([]WorktreeChange, 0, len(fields))
	for index := 0; index < len(fields); index++ {
		entry := fields[index]
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, fmt.Errorf("malformed Git porcelain entry at field %d", index)
		}
		indexStatus := entry[0]
		worktreeStatus := entry[1]
		path, err := workspaceRelativeGitPath(string(entry[3:]), workspacePrefix)
		if err != nil {
			return nil, fmt.Errorf("parse Git porcelain path at field %d: %w", index, err)
		}

		change := WorktreeChange{
			Path:           path,
			IndexStatus:    string(indexStatus),
			WorktreeStatus: string(worktreeStatus),
		}
		change.Untracked = indexStatus == '?' && worktreeStatus == '?'
		change.Staged = !change.Untracked && indexStatus != ' ' && indexStatus != '!'
		change.Unstaged = !change.Untracked && worktreeStatus != ' ' && worktreeStatus != '!'
		change.Deleted = indexStatus == 'D' || worktreeStatus == 'D'
		change.Renamed = indexStatus == 'R' || worktreeStatus == 'R'
		change.Copied = indexStatus == 'C' || worktreeStatus == 'C'

		if change.Renamed || change.Copied {
			if index+1 >= len(fields) || len(fields[index+1]) == 0 {
				return nil, fmt.Errorf("malformed Git porcelain rename/copy entry for %q: missing previous path", path)
			}
			previousPath, err := workspaceRelativeGitPath(string(fields[index+1]), workspacePrefix)
			if err != nil {
				return nil, fmt.Errorf("parse previous Git porcelain path for %q: %w", path, err)
			}
			change.PreviousPath = previousPath
			index++
		}

		change.Kind = classifyWorktreeChange(indexStatus, worktreeStatus, change)
		changes = append(changes, change)
	}

	sort.Slice(changes, func(left, right int) bool {
		if changes[left].Path != changes[right].Path {
			return changes[left].Path < changes[right].Path
		}
		if changes[left].PreviousPath != changes[right].PreviousPath {
			return changes[left].PreviousPath < changes[right].PreviousPath
		}
		if changes[left].IndexStatus != changes[right].IndexStatus {
			return changes[left].IndexStatus < changes[right].IndexStatus
		}
		return changes[left].WorktreeStatus < changes[right].WorktreeStatus
	})
	return changes, nil
}

func workspaceRelativeGitPath(repoRelative, workspacePrefix string) (string, error) {
	if repoRelative == "" {
		return "", errors.New("empty path")
	}
	repoRelative = filepath.ToSlash(filepath.Clean(filepath.FromSlash(repoRelative)))
	if workspacePrefix != "" {
		if !strings.HasSuffix(workspacePrefix, "/") {
			workspacePrefix += "/"
		}
		if !strings.HasPrefix(repoRelative, workspacePrefix) {
			return "", fmt.Errorf("path %q is outside workspace prefix %q", repoRelative, workspacePrefix)
		}
		repoRelative = strings.TrimPrefix(repoRelative, workspacePrefix)
	}
	if repoRelative == "" || repoRelative == "." || repoRelative == ".." || strings.HasPrefix(repoRelative, "../") {
		return "", fmt.Errorf("invalid workspace-relative path %q", repoRelative)
	}
	return repoRelative, nil
}

func classifyWorktreeChange(indexStatus, worktreeStatus byte, change WorktreeChange) string {
	switch {
	case change.Untracked:
		return WorktreeChangeUntracked
	case isUnmergedStatus(indexStatus, worktreeStatus):
		return WorktreeChangeUnmerged
	case change.Renamed:
		return WorktreeChangeRenamed
	case change.Copied:
		return WorktreeChangeCopied
	case change.Deleted:
		return WorktreeChangeDeleted
	case indexStatus == 'A' || worktreeStatus == 'A':
		return WorktreeChangeAdded
	case indexStatus == 'T' || worktreeStatus == 'T':
		return WorktreeChangeTypeChanged
	default:
		return WorktreeChangeModified
	}
}

func isUnmergedStatus(indexStatus, worktreeStatus byte) bool {
	if indexStatus == 'U' || worktreeStatus == 'U' {
		return true
	}
	pair := string([]byte{indexStatus, worktreeStatus})
	switch pair {
	case "DD", "AA":
		return true
	default:
		return false
	}
}
