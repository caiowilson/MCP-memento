package indexing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"memento-mcp/internal/gitstate"
)

type GitChangeMonitor struct {
	root       string
	idx        *Indexer
	interval   time.Duration
	debounce   time.Duration
	onChange   ChangeHandler
	pendingAdd map[string]struct{}
	pendingDel map[string]struct{}
	mu         sync.Mutex
	timer      *time.Timer
}

type ChangeHandler func(add, del []string)

func NewGitChangeMonitor(rootAbs string, idx *Indexer, interval, debounce time.Duration, onChange ChangeHandler) *GitChangeMonitor {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	return &GitChangeMonitor{
		root:       rootAbs,
		idx:        idx,
		interval:   interval,
		debounce:   debounce,
		onChange:   onChange,
		pendingAdd: map[string]struct{}{},
		pendingDel: map[string]struct{}{},
	}
}

func (m *GitChangeMonitor) Start(ctx context.Context) {
	t := time.NewTicker(m.interval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.pollOnce(ctx)
			}
		}
	}()
}

func IsGitRepo(rootAbs string) bool {
	cmd := exec.Command("git", "-C", rootAbs, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func (m *GitChangeMonitor) pollOnce(ctx context.Context) {
	add, del, err := gitStatusChanges(ctx, m.root)
	if err != nil || (len(add) == 0 && len(del) == 0) {
		return
	}

	m.mu.Lock()
	for _, p := range add {
		m.pendingAdd[p] = struct{}{}
	}
	for _, p := range del {
		m.pendingDel[p] = struct{}{}
	}
	if m.timer == nil {
		m.timer = time.AfterFunc(m.debounce, m.flush)
	}
	m.mu.Unlock()
}

func (m *GitChangeMonitor) flush() {
	m.mu.Lock()
	add := make([]string, 0, len(m.pendingAdd))
	del := make([]string, 0, len(m.pendingDel))
	for p := range m.pendingAdd {
		add = append(add, p)
	}
	for p := range m.pendingDel {
		del = append(del, p)
	}
	m.pendingAdd = map[string]struct{}{}
	m.pendingDel = map[string]struct{}{}
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()

	ignoreFileChanged := false
	for _, changed := range append(add, del...) {
		base := filepath.Base(changed)
		if base == ".gitignore" || base == ".mementoignore" {
			ignoreFileChanged = true
			break
		}
	}
	if ignoreFileChanged {
		_ = m.idx.IndexAll(context.Background())
	} else {
		if len(del) > 0 {
			_ = m.idx.RemovePaths(del)
		}
		if len(add) > 0 {
			_ = m.idx.EnsureIndexed(context.Background(), add)
		}
	}
	if m.onChange != nil {
		m.onChange(add, del)
	}
}

func gitStatusChanges(ctx context.Context, rootAbs string) ([]string, []string, error) {
	changes, err := gitstate.LoadWorktreeChanges(ctx, rootAbs)
	if err != nil {
		return nil, nil, err
	}
	add, del := classifyGitStatusChanges(rootAbs, changes)
	return add, del, nil
}

func classifyGitStatusChanges(rootAbs string, changes []gitstate.WorktreeChange) ([]string, []string) {
	add := make([]string, 0, len(changes))
	del := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Renamed && change.PreviousPath != "" {
			del = append(del, change.PreviousPath)
		}
		if change.Deleted {
			abs := filepath.Join(rootAbs, filepath.FromSlash(change.Path))
			if info, statErr := os.Stat(abs); statErr == nil && info.Mode().IsRegular() {
				add = append(add, change.Path)
				continue
			}
			del = append(del, change.Path)
			continue
		}
		add = append(add, change.Path)
	}
	return normalizeRelPaths(add), normalizeRelPaths(del)
}
