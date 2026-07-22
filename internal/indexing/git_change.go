package indexing

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"memento-mcp/internal/gitstate"
)

const gitPollJitterFraction = 0.20

var gitMonitorSequence atomic.Uint64

type GitChangeMonitor struct {
	root             string
	idx              *Indexer
	hotInterval      time.Duration
	maxInterval      time.Duration
	maxErrorInterval time.Duration
	debounce         time.Duration
	onChange         ChangeHandler
	statusPaths      map[string]gitPathState
	pendingAdd       map[string]struct{}
	pendingDel       map[string]struct{}
	wake             chan struct{}
	jitter           func(time.Duration) time.Duration
	mu               sync.Mutex
	timer            *time.Timer
}

type ChangeHandler func(add, del []string)

type GitChangeMonitorConfig struct {
	RootAbs              string
	Indexer              *Indexer
	HotPollInterval      time.Duration
	MaxPollInterval      time.Duration
	MaxErrorPollInterval time.Duration
	Debounce             time.Duration
	OnChange             ChangeHandler
}

type gitPollOutcome uint8

const (
	gitPollUnchanged gitPollOutcome = iota
	gitPollChanged
	gitPollFailed
)

type adaptiveGitPollSchedule struct {
	hot      time.Duration
	maxIdle  time.Duration
	maxError time.Duration
	current  time.Duration
}

type gitPathState struct {
	operation byte
	exists    bool
	mode      os.FileMode
	size      int64
	modTime   int64
}

func NewGitChangeMonitor(cfg GitChangeMonitorConfig) *GitChangeMonitor {
	if cfg.HotPollInterval <= 0 {
		cfg.HotPollInterval = 2 * time.Second
	}
	if cfg.MaxPollInterval <= 0 {
		cfg.MaxPollInterval = 30 * time.Second
	}
	if cfg.MaxPollInterval < cfg.HotPollInterval {
		cfg.MaxPollInterval = cfg.HotPollInterval
	}
	if cfg.MaxErrorPollInterval <= 0 {
		cfg.MaxErrorPollInterval = 60 * time.Second
	}
	if cfg.MaxErrorPollInterval < cfg.MaxPollInterval {
		cfg.MaxErrorPollInterval = cfg.MaxPollInterval
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 500 * time.Millisecond
	}
	seed := time.Now().UnixNano() ^ int64(os.Getpid()) ^ int64(gitMonitorSequence.Add(1))
	rng := rand.New(rand.NewSource(seed))
	return &GitChangeMonitor{
		root:             cfg.RootAbs,
		idx:              cfg.Indexer,
		hotInterval:      cfg.HotPollInterval,
		maxInterval:      cfg.MaxPollInterval,
		maxErrorInterval: cfg.MaxErrorPollInterval,
		debounce:         cfg.Debounce,
		onChange:         cfg.OnChange,
		statusPaths:      map[string]gitPathState{},
		pendingAdd:       map[string]struct{}{},
		pendingDel:       map[string]struct{}{},
		wake:             make(chan struct{}, 1),
		jitter: func(interval time.Duration) time.Duration {
			return jitterGitPollInterval(rng, interval)
		},
	}
}

func (m *GitChangeMonitor) Start(ctx context.Context) {
	go func() {
		schedule := newAdaptiveGitPollSchedule(m.hotInterval, m.maxInterval, m.maxErrorInterval)
		delay := m.jitter(schedule.current)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		due := time.Now().Add(delay)
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.wake:
				delay = m.jitter(schedule.Wake())
				wakeDue := time.Now().Add(delay)
				if wakeDue.Before(due) {
					resetGitPollTimer(timer, delay)
					due = wakeDue
				}
			case <-timer.C:
				delay = m.jitter(schedule.Observe(m.pollOnce(ctx)))
				timer.Reset(delay)
				due = time.Now().Add(delay)
			}
		}
	}()
}

// Wake returns an idle monitor to its hot interval without allowing repeated
// activity to postpone an already-earlier poll.
func (m *GitChangeMonitor) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func newAdaptiveGitPollSchedule(hot, maxIdle, maxError time.Duration) *adaptiveGitPollSchedule {
	return &adaptiveGitPollSchedule{hot: hot, maxIdle: maxIdle, maxError: maxError, current: hot}
}

func (s *adaptiveGitPollSchedule) Observe(outcome gitPollOutcome) time.Duration {
	switch outcome {
	case gitPollChanged:
		s.current = s.hot
	case gitPollFailed:
		s.current = doubledDurationCapped(s.current, s.maxError)
	default:
		s.current = doubledDurationCapped(s.current, s.maxIdle)
	}
	return s.current
}

func (s *adaptiveGitPollSchedule) Wake() time.Duration {
	s.current = s.hot
	return s.current
}

func doubledDurationCapped(current, cap time.Duration) time.Duration {
	if current >= cap || current > cap/2 {
		return cap
	}
	return current * 2
}

func jitterGitPollInterval(rng *rand.Rand, interval time.Duration) time.Duration {
	factor := 1 - gitPollJitterFraction + rng.Float64()*(2*gitPollJitterFraction)
	jittered := time.Duration(float64(interval) * factor)
	if jittered <= 0 {
		return time.Nanosecond
	}
	return jittered
}

func resetGitPollTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func IsGitRepo(rootAbs string) bool {
	if hasGitWorktreeMarker(rootAbs) {
		return true
	}
	cmd := exec.Command("git", "-C", rootAbs, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func hasGitWorktreeMarker(rootAbs string) bool {
	marker := filepath.Join(rootAbs, ".git")
	info, err := os.Stat(marker)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return true
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 4096 {
		return false
	}
	contents, err := os.ReadFile(marker)
	if err != nil {
		return false
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(contents)), "gitdir:")
	if !ok {
		return false
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(rootAbs, gitDir)
	}
	gitDirInfo, err := os.Stat(filepath.Clean(gitDir))
	return err == nil && gitDirInfo.IsDir()
}

func (m *GitChangeMonitor) pollOnce(ctx context.Context) gitPollOutcome {
	add, del, err := gitStatusChanges(ctx, m.root)
	if err != nil {
		return gitPollFailed
	}
	current := make(map[string]gitPathState, len(add)+len(del))
	for _, path := range add {
		current[path] = gitPathFingerprint(m.root, path, 'a')
	}
	for _, path := range del {
		current[path] = gitPathFingerprint(m.root, path, 'd')
	}

	m.mu.Lock()
	for _, p := range add {
		if previous, ok := m.statusPaths[p]; !ok || previous != current[p] {
			m.pendingAdd[p] = struct{}{}
		}
	}
	for _, p := range del {
		if previous, ok := m.statusPaths[p]; !ok || previous != current[p] {
			m.pendingDel[p] = struct{}{}
		}
	}
	for path := range m.statusPaths {
		if _, stillDirty := current[path]; stillDirty {
			continue
		}
		abs := filepath.Join(m.root, filepath.FromSlash(path))
		if info, statErr := os.Stat(abs); statErr == nil && info.Mode().IsRegular() {
			m.pendingAdd[path] = struct{}{}
		} else {
			m.pendingDel[path] = struct{}{}
		}
	}
	changed := !equalGitPathStates(m.statusPaths, current)
	m.statusPaths = current
	if len(m.pendingAdd) == 0 && len(m.pendingDel) == 0 {
		m.mu.Unlock()
		if changed {
			return gitPollChanged
		}
		return gitPollUnchanged
	}
	if m.timer == nil {
		m.timer = time.AfterFunc(m.debounce, m.flush)
	}
	m.mu.Unlock()
	if changed {
		return gitPollChanged
	}
	return gitPollUnchanged
}

func gitPathFingerprint(root, path string, operation byte) gitPathState {
	state := gitPathState{operation: operation}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return state
	}
	state.exists = true
	state.mode = info.Mode()
	state.size = info.Size()
	state.modTime = info.ModTime().UnixNano()
	return state
}

func equalGitPathStates(left, right map[string]gitPathState) bool {
	if len(left) != len(right) {
		return false
	}
	for path, state := range left {
		if other, ok := right[path]; !ok || other != state {
			return false
		}
	}
	return true
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
