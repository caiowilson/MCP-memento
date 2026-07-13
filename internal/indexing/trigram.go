package indexing

import (
	"sort"
	"strings"
	"time"
)

// trigramIndex is an in-memory, case-folded index over redacted chunk content.
// Callers synchronize access with Indexer.mu.
type trigramIndex struct {
	byPath map[string][]uint32
}

func newTrigramIndex() trigramIndex {
	return trigramIndex{
		byPath: map[string][]uint32{},
	}
}

func trigramKeys(text string) []uint32 {
	unique := map[uint32]struct{}{}
	addTrigramKeys(unique, text)
	keys := make([]uint32, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(a, b int) bool { return keys[a] < keys[b] })
	return keys
}

func trigramKeysForChunks(chunks []Chunk) []uint32 {
	unique := map[uint32]struct{}{}
	for _, chunk := range chunks {
		addTrigramKeys(unique, chunk.Content)
	}
	keys := make([]uint32, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(a, b int) bool { return keys[a] < keys[b] })
	return keys
}

func addTrigramKeys(unique map[uint32]struct{}, text string) {
	folded := []byte(strings.ToLower(text))
	for index := 0; index+2 < len(folded); index++ {
		key := uint32(folded[index])<<16 | uint32(folded[index+1])<<8 | uint32(folded[index+2])
		unique[key] = struct{}{}
	}
}

func (t *trigramIndex) replace(path string, keys []uint32) {
	if t.byPath == nil {
		*t = newTrigramIndex()
	}
	t.byPath[path] = append([]uint32(nil), keys...)
}

func (t *trigramIndex) remove(path string) {
	delete(t.byPath, path)
}

// candidates returns nil,false when the query is too short to prefilter.
func (t *trigramIndex) candidates(query string) (map[string]struct{}, bool) {
	keys := trigramKeys(query)
	if len(keys) == 0 {
		return nil, false
	}
	result := make(map[string]struct{}, len(t.byPath))
	for path, pathKeys := range t.byPath {
		matches := true
		for _, key := range keys {
			position := sort.Search(len(pathKeys), func(index int) bool { return pathKeys[index] >= key })
			if position == len(pathKeys) || pathKeys[position] != key {
				matches = false
				break
			}
		}
		if matches {
			result[path] = struct{}{}
		}
	}
	return result, true
}

// SubstringCandidateSnapshot conservatively filters live repository files.
// Files absent from the index, missing trigram data, or changed since the
// snapshot always remain eligible.
type SubstringCandidateSnapshot struct {
	enabled    bool
	candidates map[string]struct{}
	files      map[string]substringFileSnapshot
}

type substringFileSnapshot struct {
	size    int64
	modTime int64
	ready   bool
}

func (s SubstringCandidateSnapshot) MayContain(path string, size int64, modTime time.Time) bool {
	if !s.enabled {
		return true
	}
	file, ok := s.files[path]
	if !ok || !file.ready || file.size != size || file.modTime != modTime.UnixNano() {
		return true
	}
	_, ok = s.candidates[path]
	return ok
}

// SubstringCandidates snapshots the current lexical candidate set for callers
// that scan live files, such as repo_search.
func (i *Indexer) SubstringCandidates(query string) SubstringCandidateSnapshot {
	i.mu.Lock()
	defer i.mu.Unlock()
	candidates, enabled := i.trigrams.candidates(strings.TrimSpace(query))
	snapshot := SubstringCandidateSnapshot{
		enabled:    enabled,
		candidates: candidates,
		files:      make(map[string]substringFileSnapshot, len(i.manifest.Files)),
	}
	for path, entry := range i.manifest.Files {
		_, ready := i.trigrams.byPath[path]
		snapshot.files[path] = substringFileSnapshot{
			size:    entry.Size,
			modTime: entry.ModTime,
			ready:   ready,
		}
	}
	return snapshot
}

func (i *Indexer) rebuildTrigramIndex() {
	i.mu.Lock()
	entries := make(map[string]fileEntry, len(i.manifest.Files))
	for path, entry := range i.manifest.Files {
		entries[path] = entry
	}
	i.mu.Unlock()

	rebuilt := newTrigramIndex()
	for path, entry := range entries {
		chunks, err := i.readChunksFile(entry.ID)
		if err != nil {
			continue
		}
		rebuilt.replace(path, trigramKeysForChunks(chunks))
	}
	i.mu.Lock()
	i.trigrams = rebuilt
	i.mu.Unlock()
}
