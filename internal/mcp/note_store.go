package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type NoteStore struct {
	mu         sync.Mutex
	path       string
	lockPath   string
	legacyPath string
	repo       string
	scope      string
}

type Note struct {
	Key                 string            `json:"key"`
	Text                string            `json:"text"`
	Tags                []string          `json:"tags,omitempty"`
	Path                string            `json:"path,omitempty"`
	UpdatedAt           string            `json:"updatedAt"`
	Meta                map[string]string `json:"meta,omitempty"`
	Anchors             []NoteAnchor      `json:"anchors,omitempty"`
	Status              NoteStatus        `json:"status"`
	StaleReason         string            `json:"staleReason,omitempty"`
	StaleAt             string            `json:"staleAt,omitempty"`
	VerifiedAt          string            `json:"verifiedAt,omitempty"`
	Orphaned            bool              `json:"orphaned"`
	OrphanReason        string            `json:"orphanReason,omitempty"`
	OrphanedAt          string            `json:"orphanedAt,omitempty"`
	TombstonedAt        string            `json:"tombstonedAt,omitempty"`
	FailedAdjudications int               `json:"failedAdjudications"`
	RetrievalCount      int               `json:"retrievalCount"`
	LastRetrievedAt     string            `json:"lastRetrievedAt,omitempty"`
}

type NoteAnchor struct {
	Path        string `json:"path,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
	CommitSHA   string `json:"commitSha,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
	Branch      string `json:"branch,omitempty"`
	StartLine   int    `json:"startLine,omitempty"`
	EndLine     int    `json:"endLine,omitempty"`
}

type NoteStatus string

const (
	NoteStatusFresh      NoteStatus = "fresh"
	NoteStatusStale      NoteStatus = "stale"
	NoteStatusTombstoned NoteStatus = "tombstoned"
)

type noteFile struct {
	Repo  string `json:"repo"`
	Notes []Note `json:"notes"`
}

func NewNoteStore(repoRoot string) (*NoteStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	checkoutRoot, err := filepath.Abs(strings.TrimSpace(repoRoot))
	if err != nil {
		return nil, err
	}
	checkoutRoot = filepath.Clean(checkoutRoot)
	scopeRoot, err := memoryScopeRoot(checkoutRoot)
	if err != nil {
		return nil, err
	}
	path := noteStorePath(home, scopeRoot)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	store := &NoteStore{
		path:     path,
		lockPath: filepath.Join(dir, "notes.lock"),
		repo:     checkoutRoot,
		scope:    scopeRoot,
	}
	legacyPath := noteStorePath(home, checkoutRoot)
	if legacyPath != path {
		store.legacyPath = legacyPath
	}
	if err := store.migrateLegacy(); err != nil {
		return nil, err
	}
	return store, nil
}

// memoryScopeRoot maps linked Git worktrees to the corresponding path in the
// main worktree. Durable notes are repository-scoped and should therefore be
// shared across linked checkouts, while NoteStore.repo remains the active
// checkout so anchor reconciliation still observes its branch and files.
// Non-Git workspaces retain their literal root. Git worktrees fail closed if
// Git identifies the checkout but cannot provide a canonical main worktree.
func memoryScopeRoot(repoRoot string) (string, error) {
	repoRoot = canonicalPath(repoRoot)
	topOut, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return repoRoot, nil
	}
	worktreeTop := canonicalPath(strings.TrimSpace(string(topOut)))
	if worktreeTop == "" {
		return "", fmt.Errorf("resolve durable memory scope for %s: Git returned an empty worktree root", repoRoot)
	}

	listOut, err := exec.Command("git", "-C", repoRoot, "worktree", "list", "--porcelain", "-z").Output()
	if err != nil {
		return "", fmt.Errorf("resolve durable memory scope for %s: list Git worktrees: %w", repoRoot, err)
	}
	mainWorktreeTop := ""
	for _, field := range bytes.Split(listOut, []byte{0}) {
		value := strings.TrimSpace(string(field))
		if strings.HasPrefix(value, "worktree ") {
			mainWorktreeTop = canonicalPath(strings.TrimSpace(strings.TrimPrefix(value, "worktree ")))
			break
		}
	}
	if mainWorktreeTop == "" {
		return "", fmt.Errorf("resolve durable memory scope for %s: Git returned no main worktree", repoRoot)
	}

	rel, err := filepath.Rel(worktreeTop, repoRoot)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("resolve durable memory scope for %s: checkout is outside Git worktree %s", repoRoot, worktreeTop)
	}
	return filepath.Clean(filepath.Join(mainWorktreeTop, rel)), nil
}

func canonicalPath(path string) string {
	if abs, err := filepath.Abs(strings.TrimSpace(path)); err == nil {
		path = abs
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func noteStorePath(home, root string) string {
	sum := sha256.Sum256([]byte(root))
	repoID := hex.EncodeToString(sum[:16])
	return filepath.Join(home, ".memento-mcp", "repos", repoID, "notes.json")
}

func (s *NoteStore) lockStore() (func(), error) {
	s.mu.Lock()
	lockPath := s.lockPath
	if lockPath == "" {
		lockPath = s.path + ".lock"
	}
	releaseFileLock, err := acquireNoteFileLock(lockPath)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	return func() {
		releaseFileLock()
		s.mu.Unlock()
	}, nil
}

func (s *NoteStore) storageScope() string {
	if s.scope != "" {
		return s.scope
	}
	return s.repo
}

func (s *NoteStore) migrateLegacy() error {
	if s.legacyPath == "" {
		return nil
	}
	if _, err := os.Stat(s.legacyPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	unlock, err := s.lockStore()
	if err != nil {
		return err
	}
	defer unlock()
	legacyUnlock, err := acquireNoteFileLock(filepath.Join(filepath.Dir(s.legacyPath), "notes.lock"))
	if err != nil {
		return err
	}
	defer legacyUnlock()

	legacy, exists, err := loadNoteFile(s.legacyPath, s.repo)
	if err != nil || !exists {
		return err
	}
	current, err := s.loadLocked()
	if err != nil {
		return err
	}
	byKey := make(map[string]int, len(current.Notes))
	for index := range current.Notes {
		byKey[current.Notes[index].Key] = index
	}
	changed := false
	for _, legacyNote := range legacy.Notes {
		index, found := byKey[legacyNote.Key]
		if !found {
			current.Notes = append(current.Notes, legacyNote)
			byKey[legacyNote.Key] = len(current.Notes) - 1
			changed = true
			continue
		}
		original := current.Notes[index]
		merged := newerNote(original, legacyNote)
		if merged.RetrievalCount < original.RetrievalCount {
			merged.RetrievalCount = original.RetrievalCount
		}
		if merged.RetrievalCount < legacyNote.RetrievalCount {
			merged.RetrievalCount = legacyNote.RetrievalCount
		}
		if legacyNote.LastRetrievedAt > merged.LastRetrievedAt {
			merged.LastRetrievedAt = legacyNote.LastRetrievedAt
		}
		if noteRecency(legacyNote) > noteRecency(original) || merged.RetrievalCount != original.RetrievalCount || merged.LastRetrievedAt != original.LastRetrievedAt {
			current.Notes[index] = merged
			changed = true
		}
	}
	if changed {
		if err := s.saveLocked(current); err != nil {
			return err
		}
	}
	backup := fmt.Sprintf("%s.migrated-%d", s.legacyPath, time.Now().UTC().UnixNano())
	if err := os.Rename(s.legacyPath, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("archive migrated legacy notes: %w", err)
	}
	return nil
}

func newerNote(current, candidate Note) Note {
	if noteRecency(candidate) > noteRecency(current) {
		return candidate
	}
	return current
}

func noteRecency(note Note) string {
	latest := note.UpdatedAt
	for _, value := range []string{note.VerifiedAt, note.StaleAt, note.OrphanedAt, note.TombstonedAt, note.LastRetrievedAt} {
		if value > latest {
			latest = value
		}
	}
	return latest
}

func (s *NoteStore) Upsert(n Note) (Note, error) {
	if strings.TrimSpace(n.Key) == "" {
		return Note{}, fmt.Errorf("missing note key")
	}
	if strings.TrimSpace(n.Text) == "" {
		return Note{}, fmt.Errorf("missing note text")
	}
	unlock, err := s.lockStore()
	if err != nil {
		return Note{}, err
	}
	defer unlock()

	f, err := s.loadLocked()
	if err != nil {
		return Note{}, err
	}

	n.Key = strings.TrimSpace(n.Key)
	if strings.TrimSpace(n.Path) != "" {
		n.Path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(n.Path)))
	} else {
		n.Path = ""
	}
	n.Tags = normalizeTags(n.Tags)
	n.Anchors, err = s.snapshotAnchors(n.Anchors)
	if err != nil {
		return Note{}, err
	}
	n.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	n.Status = NoteStatusFresh
	n.VerifiedAt = n.UpdatedAt
	n.StaleReason = ""
	n.StaleAt = ""
	n.Orphaned = false
	n.OrphanReason = ""
	n.OrphanedAt = ""
	n.TombstonedAt = ""
	n.FailedAdjudications = 0

	found := false
	for i := range f.Notes {
		if f.Notes[i].Key == n.Key {
			n.RetrievalCount = f.Notes[i].RetrievalCount
			n.LastRetrievedAt = f.Notes[i].LastRetrievedAt
			f.Notes[i] = n
			found = true
			break
		}
	}
	if !found {
		f.Notes = append(f.Notes, n)
	}

	if err := s.saveLocked(f); err != nil {
		return Note{}, err
	}
	return n, nil
}

func (s *NoteStore) Search(query string, tags []string, limit int) ([]Note, error) {
	if limit <= 0 {
		limit = 20
	}
	unlock, err := s.lockStore()
	if err != nil {
		return nil, err
	}
	defer unlock()

	f, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	q := strings.TrimSpace(query)
	qLower := strings.ToLower(q)
	tagSet := make(map[string]struct{}, len(tags))
	for _, t := range normalizeTags(tags) {
		tagSet[t] = struct{}{}
	}

	changed := s.reconcileLocked(&f, nil)
	matches := make([]int, 0, len(f.Notes))
	for index, n := range f.Notes {
		if noteStatus(n) == NoteStatusTombstoned {
			continue
		}
		if len(tagSet) > 0 && !noteHasAllTags(n, tagSet) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(n.Text + "\n" + n.Key + "\n" + n.Path)
			if !strings.Contains(hay, qLower) {
				continue
			}
		}
		matches = append(matches, index)
	}
	sortNoteMatches(f.Notes, matches)
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]Note, 0, len(matches))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, index := range matches {
		f.Notes[index].RetrievalCount++
		f.Notes[index].LastRetrievedAt = now
		out = append(out, f.Notes[index])
		changed = true
	}
	if changed {
		if err := s.saveLocked(f); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Read returns one active note and records that its contents were retrieved.
func (s *NoteStore) Read(key string) (Note, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Note{}, fmt.Errorf("missing note key")
	}
	unlock, err := s.lockStore()
	if err != nil {
		return Note{}, err
	}
	defer unlock()
	f, err := s.loadLocked()
	if err != nil {
		return Note{}, err
	}
	changed := s.reconcileLocked(&f, nil)
	for index := range f.Notes {
		note := &f.Notes[index]
		if note.Key != key || noteStatus(*note) == NoteStatusTombstoned {
			continue
		}
		note.RetrievalCount++
		note.LastRetrievedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.saveLocked(f); err != nil {
			return Note{}, err
		}
		return *note, nil
	}
	if changed {
		if err := s.saveLocked(f); err != nil {
			return Note{}, err
		}
	}
	return Note{}, fmt.Errorf("note not found: %q", key)
}

func (s *NoteStore) Clear() error {
	unlock, err := s.lockStore()
	if err != nil {
		return err
	}
	defer unlock()

	f := noteFile{Repo: s.storageScope(), Notes: nil}
	return s.saveLocked(f)
}

// List returns all notes for this repo scope, ordered by insertion.
func (s *NoteStore) List() ([]Note, error) {
	unlock, err := s.lockStore()
	if err != nil {
		return nil, err
	}
	defer unlock()
	f, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	if s.reconcileLocked(&f, nil) {
		if err := s.saveLocked(f); err != nil {
			return nil, err
		}
	}
	out := make([]Note, len(f.Notes))
	copy(out, f.Notes)
	return out, nil
}

// Delete removes the note with the given key. Returns an error if not found.
func (s *NoteStore) Delete(key string) error {
	key = strings.TrimSpace(key)
	unlock, err := s.lockStore()
	if err != nil {
		return err
	}
	defer unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	for i, n := range f.Notes {
		if n.Key == key {
			f.Notes = append(f.Notes[:i], f.Notes[i+1:]...)
			return s.saveLocked(f)
		}
	}
	return fmt.Errorf("note not found: %q", key)
}

func (s *NoteStore) loadLocked() (noteFile, error) {
	f, _, err := loadNoteFile(s.path, s.storageScope())
	if err != nil {
		return noteFile{}, err
	}
	if f.Repo == "" {
		f.Repo = s.storageScope()
	}
	for index := range f.Notes {
		if f.Notes[index].Status == "" {
			f.Notes[index].Status = NoteStatusFresh
		}
	}
	return f, nil
}

func loadNoteFile(path, repo string) (noteFile, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return noteFile{Repo: repo, Notes: nil}, false, nil
		}
		return noteFile{}, false, err
	}
	var f noteFile
	if err := json.Unmarshal(b, &f); err != nil {
		return noteFile{Repo: repo, Notes: nil}, true, nil
	}
	if f.Repo == "" {
		f.Repo = repo
	}
	for index := range f.Notes {
		if f.Notes[index].Status == "" {
			f.Notes[index].Status = NoteStatusFresh
		}
	}
	return f, true, nil
}

func (s *NoteStore) saveLocked(f noteFile) error {
	f.Repo = s.storageScope()
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".notes-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func noteHasAllTags(n Note, required map[string]struct{}) bool {
	if len(required) == 0 {
		return true
	}
	have := map[string]struct{}{}
	for _, t := range normalizeTags(n.Tags) {
		have[t] = struct{}{}
	}
	for t := range required {
		if _, ok := have[t]; !ok {
			return false
		}
	}
	return true
}
