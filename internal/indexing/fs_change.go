package indexing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type FSChangeMonitor struct {
	root     string
	idx      *Indexer
	debounce time.Duration
	onChange ChangeHandler

	mu         sync.Mutex
	pendingAdd map[string]struct{}
	pendingDel map[string]struct{}
	timer      *time.Timer
}

func NewFSChangeMonitor(rootAbs string, idx *Indexer, debounce time.Duration, onChange ChangeHandler) *FSChangeMonitor {
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	return &FSChangeMonitor{
		root:       rootAbs,
		idx:        idx,
		debounce:   debounce,
		onChange:   onChange,
		pendingAdd: map[string]struct{}{},
		pendingDel: map[string]struct{}{},
	}
}

func (m *FSChangeMonitor) Start(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := m.addDirRecursive(w, m.root); err != nil {
		_ = w.Close()
		return err
	}

	go func() {
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-w.Events:
				if !ok {
					return
				}
				m.handleEvent(w, evt)
			case <-w.Errors:
				// ignore watcher errors; best-effort
			}
		}
	}()
	return nil
}

func (m *FSChangeMonitor) handleEvent(w *fsnotify.Watcher, evt fsnotify.Event) {
	if evt.Name == "" {
		return
	}
	abs := filepath.Clean(evt.Name)
	rel, err := filepath.Rel(m.root, abs)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)

	if evt.Op&fsnotify.Create == fsnotify.Create {
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			_ = m.addDirRecursive(w, abs)
			return
		}
	}

	if isIgnoredPath(rel) {
		return
	}

	if evt.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		m.enqueueDelete(rel)
		return
	}
	if evt.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Chmod) != 0 {
		m.enqueueAdd(rel)
	}
}

func (m *FSChangeMonitor) enqueueAdd(rel string) {
	m.mu.Lock()
	m.pendingAdd[rel] = struct{}{}
	if m.timer == nil {
		m.timer = time.AfterFunc(m.debounce, m.flush)
	}
	m.mu.Unlock()
}

func (m *FSChangeMonitor) enqueueDelete(rel string) {
	m.mu.Lock()
	m.pendingDel[rel] = struct{}{}
	if m.timer == nil {
		m.timer = time.AfterFunc(m.debounce, m.flush)
	}
	m.mu.Unlock()
}

func (m *FSChangeMonitor) flush() {
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
	if m.onChange != nil {
		m.onChange(add, del)
	}
}

func (m *FSChangeMonitor) addDirRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if shouldIgnoreDir(name, m.idx.cfg.ExtraIgnoreDirs) {
				return filepath.SkipDir
			}
			return w.Add(path)
		}
		return nil
	})
}

func isIgnoredPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return true
	}
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if p == "" {
			continue
		}
		if shouldIgnoreDir(p, nil) {
			return true
		}
	}
	return false
}
