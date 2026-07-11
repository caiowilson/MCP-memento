package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type NoteStore struct {
	mu   sync.Mutex
	path string
	repo string
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
	sum := sha256.Sum256([]byte(repoRoot))
	repoID := hex.EncodeToString(sum[:16])
	dir := filepath.Join(home, ".memento-mcp", "repos", repoID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &NoteStore{
		path: filepath.Join(dir, "notes.json"),
		repo: repoRoot,
	}, nil
}

func (s *NoteStore) Upsert(n Note) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(n.Key) == "" {
		return Note{}, fmt.Errorf("missing note key")
	}
	if strings.TrimSpace(n.Text) == "" {
		return Note{}, fmt.Errorf("missing note text")
	}

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
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = 20
	}

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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()

	f := noteFile{Repo: s.repo, Notes: nil}
	return s.saveLocked(f)
}

// List returns all notes for this repo scope, ordered by insertion.
func (s *NoteStore) List() ([]Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return noteFile{Repo: s.repo, Notes: nil}, nil
		}
		return noteFile{}, err
	}
	var f noteFile
	if err := json.Unmarshal(b, &f); err != nil {
		return noteFile{Repo: s.repo, Notes: nil}, nil
	}
	if f.Repo == "" {
		f.Repo = s.repo
	}
	for index := range f.Notes {
		if f.Notes[index].Status == "" {
			f.Notes[index].Status = NoteStatusFresh
		}
	}
	return f, nil
}

func (s *NoteStore) saveLocked(f noteFile) error {
	f.Repo = s.repo
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
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
