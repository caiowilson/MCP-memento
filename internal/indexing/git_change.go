package indexing

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GitChangeMonitor struct {
	root       string
	idx        *Indexer
	interval   time.Duration
	debounce   time.Duration
	pendingAdd map[string]struct{}
	pendingDel map[string]struct{}
	mu         sync.Mutex
	timer      *time.Timer
}

func NewGitChangeMonitor(rootAbs string, idx *Indexer, interval, debounce time.Duration) *GitChangeMonitor {
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

	if len(del) > 0 {
		_ = m.idx.RemovePaths(del)
	}
	if len(add) > 0 {
		_ = m.idx.EnsureIndexed(context.Background(), add)
	}
}

func gitStatusChanges(ctx context.Context, rootAbs string) ([]string, []string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", rootAbs, "status", "--porcelain", "-z", "--untracked-files=all")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, err
	}
	return parsePorcelainZ(out)
}

func parsePorcelainZ(b []byte) ([]string, []string, error) {
	if len(b) == 0 {
		return nil, nil, nil
	}
	entries := bytes.Split(b, []byte{0})
	add := []string{}
	del := []string{}

	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) == 0 {
			continue
		}
		if len(e) < 3 {
			return add, del, errors.New("unexpected porcelain entry")
		}
		status := string(e[:2])
		path := strings.TrimSpace(string(e[3:]))
		if path == "" {
			continue
		}
		path = filepath.ToSlash(filepath.Clean(path))

		isRename := strings.Contains(status, "R") || strings.Contains(status, "C")
		if isRename {
			if i+1 < len(entries) && len(entries[i+1]) > 0 {
				oldPath := path
				newPath := strings.TrimSpace(string(entries[i+1]))
				newPath = filepath.ToSlash(filepath.Clean(newPath))
				del = append(del, oldPath)
				add = append(add, newPath)
				i++
				continue
			}
		}

		if strings.Contains(status, "D") {
			del = append(del, path)
			continue
		}
		add = append(add, path)
	}

	return normalizeRelPaths(add), normalizeRelPaths(del), nil
}
