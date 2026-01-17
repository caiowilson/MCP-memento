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
	Key       string            `json:"key"`
	Text      string            `json:"text"`
	Tags      []string          `json:"tags,omitempty"`
	Path      string            `json:"path,omitempty"`
	UpdatedAt string            `json:"updatedAt"`
	Meta      map[string]string `json:"meta,omitempty"`
}

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

	now := time.Now().UTC().Format(time.RFC3339)
	n.UpdatedAt = now

	f, err := s.loadLocked()
	if err != nil {
		return Note{}, err
	}

	n.Key = strings.TrimSpace(n.Key)
	n.Path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(n.Path)))
	n.Tags = normalizeTags(n.Tags)

	found := false
	for i := range f.Notes {
		if f.Notes[i].Key == n.Key {
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

	out := make([]Note, 0, min(limit, len(f.Notes)))
	for _, n := range f.Notes {
		if len(tagSet) > 0 && !noteHasAllTags(n, tagSet) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(n.Text + "\n" + n.Key + "\n" + n.Path)
			if !strings.Contains(hay, qLower) {
				continue
			}
		}
		out = append(out, n)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
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
