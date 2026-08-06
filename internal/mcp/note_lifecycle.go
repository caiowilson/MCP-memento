package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMemoryGCAgeDays             = 90
	minimumMemoryGCAgeDays             = 30
	defaultMemoryGCFailedAdjudications = 2
	defaultMemoryGCMaxRetrievals       = 3
)

type memoryGCRules struct {
	OlderThanDays          int
	MinFailedAdjudications int
	MaximumRetrievalCount  int
}

type memoryGCResult struct {
	DeletedKeys []string `json:"deletedKeys"`
	Deleted     int      `json:"deleted"`
	Retained    int      `json:"retained"`
}

type noteGitState struct {
	Head   string
	Branch string
}

func noteStatus(note Note) NoteStatus {
	if note.Status == "" {
		return NoteStatusFresh
	}
	return note.Status
}

func sortNoteMatches(notes []Note, matches []int) {
	sort.SliceStable(matches, func(i, j int) bool {
		return noteStatus(notes[matches[i]]) == NoteStatusFresh && noteStatus(notes[matches[j]]) != NoteStatusFresh
	})
}

func (s *NoteStore) ReconcileChanged(paths []string) error {
	unlock, err := s.lockStore()
	if err != nil {
		return err
	}
	defer unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	changedPaths := map[string]struct{}{}
	for _, path := range paths {
		if path = cleanNoteAnchorPath(path); path != "" {
			changedPaths[path] = struct{}{}
		}
	}
	if s.reconcileLocked(&f, changedPaths) {
		return s.saveLocked(f)
	}
	return nil
}

func (s *NoteStore) reconcileLocked(f *noteFile, changedPaths map[string]struct{}) bool {
	git := currentNoteGitState(s.repo)
	changed := false
	for noteIndex := range f.Notes {
		note := &f.Notes[noteIndex]
		if noteStatus(*note) == NoteStatusTombstoned {
			continue
		}
		for anchorIndex := range note.Anchors {
			anchor := &note.Anchors[anchorIndex]
			if len(changedPaths) > 0 {
				if _, ok := changedPaths[anchor.Path]; !ok {
					continue
				}
			}
			anchorChanged, reason, orphaned := s.reconcileAnchor(anchor, git)
			if !anchorChanged {
				continue
			}
			changed = true
			if orphaned {
				if s.allNoteAnchorsOrphaned(note, git) {
					markNoteOrphaned(note, reason)
					break
				}
			}
			markNoteStale(note, reason, false)
		}
	}
	return changed
}

func (s *NoteStore) allNoteAnchorsOrphaned(note *Note, git noteGitState) bool {
	if len(note.Anchors) == 0 {
		return false
	}
	for index := range note.Anchors {
		_, _, orphaned := s.reconcileAnchor(&note.Anchors[index], git)
		if !orphaned {
			return false
		}
	}
	return true
}

func (s *NoteStore) reconcileAnchor(anchor *NoteAnchor, git noteGitState) (bool, string, bool) {
	if anchor.Path == "" {
		if anchor.CommitSHA != "" && git.Head != "" && anchor.CommitSHA != git.Head && noteAnchorSameLineage(s.repo, *anchor, git) {
			return true, fmt.Sprintf("anchored commit advanced: %s -> %s", anchor.CommitSHA, git.Head), false
		}
		return false, "", false
	}
	abs, joinErr := safeJoin(s.repo, anchor.Path)
	if joinErr != nil {
		return true, joinErr.Error(), false
	}
	if _, err := os.Stat(abs); err == nil {
		hash, _, _, hashErr := s.currentAnchorHash(*anchor)
		if hashErr != nil {
			if strings.Contains(hashErr.Error(), "symbol") {
				orphaned := noteAnchorSameLineage(s.repo, *anchor, git)
				return true, hashErr.Error(), orphaned
			}
			return true, hashErr.Error(), false
		}
		if anchor.ContentHash != "" && hash != anchor.ContentHash {
			return true, fmt.Sprintf("anchored content changed: %s", anchor.Path), false
		}
		return false, "", false
	}
	if !noteAnchorSameLineage(s.repo, *anchor, git) {
		return false, "", false
	}
	if renamed := findNoteAnchorRename(s.repo, anchor.CommitSHA, anchor.Path); renamed != "" {
		oldPath := anchor.Path
		anchor.Path = renamed
		return true, fmt.Sprintf("anchored path renamed: %s -> %s", oldPath, renamed), false
	}
	if moved, found := findMovedAnchorSymbol(s.repo, *anchor); found {
		if moved == "" {
			return true, fmt.Sprintf("anchored symbol %q has multiple possible new paths", anchor.Symbol), false
		}
		oldPath := anchor.Path
		anchor.Path = moved
		return true, fmt.Sprintf("anchored symbol moved: %s -> %s", oldPath, moved), false
	}
	return true, fmt.Sprintf("anchored referent no longer exists: %s", anchor.Path), true
}

func findMovedAnchorSymbol(root string, anchor NoteAnchor) (string, bool) {
	if anchor.Symbol == "" {
		return "", false
	}
	wantedLanguage := languageForStructuredOutline(anchor.Path)
	matches := []string{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && shouldIgnoreDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldIgnoreFile(entry.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == anchor.Path || languageForStructuredOutline(rel) != wantedLanguage {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil || info.Size() > defaultRepoOutlineMaxFileBytes {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		outline := extractStructuredFileOutline(path, source)
		if _, ok := findAnchorSymbol(outline.Symbols, anchor.Symbol); ok {
			matches = append(matches, rel)
		}
		return nil
	})
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", len(matches) > 1
}

func markNoteStale(note *Note, reason string, failedAdjudication bool) {
	now := time.Now().UTC().Format(time.RFC3339)
	if noteStatus(*note) != NoteStatusTombstoned {
		note.Status = NoteStatusStale
	}
	if note.StaleAt == "" {
		note.StaleAt = now
	}
	note.StaleReason = strings.TrimSpace(reason)
	if failedAdjudication {
		note.FailedAdjudications++
	}
}

func markNoteOrphaned(note *Note, reason string) {
	markNoteStale(note, reason, false)
	now := time.Now().UTC().Format(time.RFC3339)
	note.Orphaned = true
	note.OrphanReason = strings.TrimSpace(reason)
	if note.OrphanedAt == "" {
		note.OrphanedAt = now
	}
	note.Status = NoteStatusTombstoned
	if note.TombstonedAt == "" {
		note.TombstonedAt = now
	}
}

func (s *NoteStore) MarkStale(key, reason string, failedAdjudication, orphaned bool) (Note, error) {
	key, reason = strings.TrimSpace(key), strings.TrimSpace(reason)
	if key == "" || reason == "" {
		return Note{}, fmt.Errorf("key and reason are required")
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
	for index := range f.Notes {
		if f.Notes[index].Key != key {
			continue
		}
		markNoteStale(&f.Notes[index], reason, failedAdjudication)
		if orphaned {
			f.Notes[index].Orphaned = true
			f.Notes[index].OrphanReason = reason
			if f.Notes[index].OrphanedAt == "" {
				f.Notes[index].OrphanedAt = time.Now().UTC().Format(time.RFC3339)
			}
		}
		if err := s.saveLocked(f); err != nil {
			return Note{}, err
		}
		return f.Notes[index], nil
	}
	return Note{}, fmt.Errorf("note not found: %q", key)
}

func (s *NoteStore) Verify(key string) (Note, error) {
	key = strings.TrimSpace(key)
	unlock, err := s.lockStore()
	if err != nil {
		return Note{}, err
	}
	defer unlock()
	f, err := s.loadLocked()
	if err != nil {
		return Note{}, err
	}
	for index := range f.Notes {
		if f.Notes[index].Key != key {
			continue
		}
		anchors, snapshotErr := s.snapshotAnchors(f.Notes[index].Anchors)
		if snapshotErr != nil {
			return Note{}, snapshotErr
		}
		note := &f.Notes[index]
		note.Anchors = anchors
		note.Status = NoteStatusFresh
		note.StaleReason, note.StaleAt = "", ""
		note.Orphaned, note.OrphanReason, note.OrphanedAt = false, "", ""
		note.TombstonedAt = ""
		note.FailedAdjudications = 0
		note.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.saveLocked(f); err != nil {
			return Note{}, err
		}
		return *note, nil
	}
	return Note{}, fmt.Errorf("note not found: %q", key)
}

func (s *NoteStore) Tombstone(key, reason string) (Note, error) {
	key, reason = strings.TrimSpace(key), strings.TrimSpace(reason)
	if key == "" || reason == "" {
		return Note{}, fmt.Errorf("key and reason are required")
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
	for index := range f.Notes {
		if f.Notes[index].Key != key {
			continue
		}
		note := &f.Notes[index]
		markNoteStale(note, reason, true)
		note.Status = NoteStatusTombstoned
		if note.TombstonedAt == "" {
			note.TombstonedAt = time.Now().UTC().Format(time.RFC3339)
		}
		if err := s.saveLocked(f); err != nil {
			return Note{}, err
		}
		return *note, nil
	}
	return Note{}, fmt.Errorf("note not found: %q", key)
}

func (s *NoteStore) GarbageCollect(rules memoryGCRules) (memoryGCResult, error) {
	if rules.OlderThanDays <= 0 {
		rules.OlderThanDays = defaultMemoryGCAgeDays
	}
	if rules.OlderThanDays < minimumMemoryGCAgeDays {
		rules.OlderThanDays = minimumMemoryGCAgeDays
	}
	if rules.MinFailedAdjudications <= 0 {
		rules.MinFailedAdjudications = defaultMemoryGCFailedAdjudications
	}
	if rules.MaximumRetrievalCount <= 0 {
		rules.MaximumRetrievalCount = defaultMemoryGCMaxRetrievals
	}
	if rules.MaximumRetrievalCount > defaultMemoryGCMaxRetrievals {
		rules.MaximumRetrievalCount = defaultMemoryGCMaxRetrievals
	}
	unlock, err := s.lockStore()
	if err != nil {
		return memoryGCResult{}, err
	}
	defer unlock()
	f, err := s.loadLocked()
	if err != nil {
		return memoryGCResult{}, err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(rules.OlderThanDays) * 24 * time.Hour)
	kept := make([]Note, 0, len(f.Notes))
	result := memoryGCResult{}
	for _, note := range f.Notes {
		eligible := noteStatus(note) == NoteStatusTombstoned && note.Orphaned && note.FailedAdjudications >= rules.MinFailedAdjudications && note.RetrievalCount < rules.MaximumRetrievalCount
		if eligible {
			tombstonedAt, parseErr := time.Parse(time.RFC3339, note.TombstonedAt)
			eligible = parseErr == nil && tombstonedAt.Before(cutoff)
		}
		if eligible && note.LastRetrievedAt != "" {
			lastRetrievedAt, parseErr := time.Parse(time.RFC3339, note.LastRetrievedAt)
			eligible = parseErr == nil && lastRetrievedAt.Before(cutoff)
		}
		if eligible {
			result.DeletedKeys = append(result.DeletedKeys, note.Key)
			continue
		}
		kept = append(kept, note)
	}
	result.Deleted = len(result.DeletedKeys)
	result.Retained = len(kept)
	if result.Deleted > 0 {
		f.Notes = kept
		if err := s.saveLocked(f); err != nil {
			return memoryGCResult{}, err
		}
	}
	return result, nil
}

func parsePositiveInt(value any, fallback int) int {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case string:
		if parsed, err := strconv.Atoi(typed); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}
