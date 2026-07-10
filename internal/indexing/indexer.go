package indexing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"memento-mcp/internal/embedding"
	"memento-mcp/internal/redact"
)

type Config struct {
	RootAbs            string
	StoreDir           string
	MaxTotalBytes      int64
	MaxFileBytes       int64
	MaxChunkBytes      int
	MaxChunkLines      int
	PollInterval       time.Duration
	PreferredExts      []string
	AllowGlobs         []string
	DenyGlobs          []string
	ExtraIgnoreDirs    []string
	ExtraIgnoreGlobs   []string
	Redactor           *redact.Redactor
	Embedder           embedding.Embedder
	SemanticWeight     float64
	EmbeddingBatchSize int
}

type Status struct {
	Ready         bool   `json:"ready"`
	LastIndexedAt string `json:"lastIndexedAt,omitempty"`
	FilesIndexed  int    `json:"filesIndexed"`
	BytesIndexed  int64  `json:"bytesIndexed"`
	Partial       bool   `json:"partial"`
	Error         string `json:"error,omitempty"`
}

type Indexer struct {
	rootAbs     string
	dir         string
	cfg         Config
	ignoreRules *ignoreRules
	redactor    *redact.Redactor
	embedder    embedding.Embedder

	mu                    sync.Mutex
	manifest              manifest
	status                Status
	embeddingBackoffUntil time.Time

	reqCh chan request
}

type request struct {
	ctx   context.Context
	paths []string // repo-relative, posix
	full  bool
	done  chan error
}

var errEmbeddingBackoff = errors.New("embedding runtime is in retry backoff")

func New(cfg Config) (*Indexer, error) {
	if cfg.RootAbs == "" {
		return nil, errors.New("RootAbs is required")
	}
	rootAbs, err := filepath.Abs(cfg.RootAbs)
	if err != nil {
		return nil, err
	}
	cfg.RootAbs = rootAbs
	applyDefaults(&cfg)
	if cfg.Redactor == nil {
		cfg.Redactor = redact.Default()
	}

	dir := cfg.StoreDir
	if dir == "" {
		var err error
		dir, err = repoIndexDir(rootAbs)
		if err != nil {
			return nil, err
		}
	} else {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		dir = absDir
	}
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		return nil, err
	}

	idx := &Indexer{
		rootAbs:  rootAbs,
		dir:      dir,
		cfg:      cfg,
		redactor: cfg.Redactor,
		embedder: cfg.Embedder,
		reqCh:    make(chan request, 8),
	}

	rules, err := loadIgnoreRules(rootAbs)
	if err != nil {
		return nil, err
	}
	idx.ignoreRules = rules

	if err := idx.loadManifest(); err != nil || idx.manifest.RedactionFingerprint != idx.redactor.Fingerprint() {
		if err := idx.resetIndexFiles(); err != nil {
			return nil, err
		}
	} else if idx.manifest.EmbeddingFingerprint != idx.embeddingFingerprint() {
		if err := idx.resetVectorFiles(); err != nil {
			return nil, err
		}
	}
	return idx, nil
}

// ReloadIgnoreRules re-reads .gitignore and .mementoignore from the workspace root.
func (i *Indexer) ReloadIgnoreRules() error {
	rules, err := loadIgnoreRules(i.rootAbs)
	if err != nil {
		return err
	}
	i.mu.Lock()
	i.ignoreRules = rules
	i.mu.Unlock()
	return nil
}

func (i *Indexer) Start(ctx context.Context) {
	go i.worker(ctx)
	if i.cfg.PollInterval > 0 {
		go i.poller(ctx)
	}
}

func (i *Indexer) IndexAll(ctx context.Context) error {
	done := make(chan error, 1)
	i.reqCh <- request{ctx: ctx, full: true, done: done}
	return <-done
}

func (i *Indexer) EnsureIndexed(ctx context.Context, relPaths []string) error {
	done := make(chan error, 1)
	i.reqCh <- request{ctx: ctx, paths: relPaths, full: false, done: done}
	return <-done
}

func (i *Indexer) RemovePaths(relPaths []string) error {
	relPaths = normalizeRelPaths(relPaths)
	if len(relPaths) == 0 {
		return nil
	}
	i.mu.Lock()
	for _, rel := range relPaths {
		ent, ok := i.manifest.Files[rel]
		if !ok {
			continue
		}
		_ = os.Remove(i.chunkFilePath(ent.ID))
		i.removeVectorFile(ent.ID)
		i.manifest.TotalBytes -= ent.Size
		delete(i.manifest.Files, rel)
	}
	i.mu.Unlock()
	return i.saveManifest()
}

func (i *Indexer) Status() Status {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.status
}

type DebugInfo struct {
	RootAbs          string   `json:"root"`
	StoreDir         string   `json:"storeDir"`
	FilesIndexed     int      `json:"filesIndexed"`
	TotalBytes       int64    `json:"totalBytes"`
	PreferredExts    []string `json:"preferredExts"`
	AllowGlobs       []string `json:"allowGlobs"`
	DenyGlobs        []string `json:"denyGlobs"`
	ExtraIgnoreDirs  []string `json:"extraIgnoreDirs"`
	ExtraIgnoreGlobs []string `json:"extraIgnoreGlobs"`
	SemanticEnabled  bool     `json:"semanticEnabled"`
	EmbeddingModel   string   `json:"embeddingModel,omitempty"`
	SemanticWeight   float64  `json:"semanticWeight,omitempty"`
	VectorsIndexed   int      `json:"vectorsIndexed"`
	LastError        string   `json:"lastError,omitempty"`
}

func (i *Indexer) FileChunks(relPath string) ([]Chunk, error) {
	i.mu.Lock()
	ent, ok := i.manifest.Files[relPath]
	i.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	return i.readChunksFile(ent.ID)
}

func (i *Indexer) DebugInfo() DebugInfo {
	i.mu.Lock()
	defer i.mu.Unlock()
	vectorsIndexed := 0
	for _, entry := range i.manifest.Files {
		vectorsIndexed += entry.Vectors
	}
	info := DebugInfo{
		RootAbs:          i.rootAbs,
		StoreDir:         i.dir,
		FilesIndexed:     len(i.manifest.Files),
		TotalBytes:       i.manifest.TotalBytes,
		PreferredExts:    append([]string{}, i.cfg.PreferredExts...),
		AllowGlobs:       append([]string{}, i.cfg.AllowGlobs...),
		DenyGlobs:        append([]string{}, i.cfg.DenyGlobs...),
		ExtraIgnoreDirs:  append([]string{}, i.cfg.ExtraIgnoreDirs...),
		ExtraIgnoreGlobs: append([]string{}, i.cfg.ExtraIgnoreGlobs...),
		SemanticEnabled:  i.embedder != nil,
		VectorsIndexed:   vectorsIndexed,
		LastError:        i.status.Error,
	}
	if i.embedder != nil {
		info.EmbeddingModel = i.embedder.Name()
		info.SemanticWeight = i.cfg.SemanticWeight
	}
	return info
}

func (i *Indexer) SemanticEnabled() bool {
	return i.embedder != nil
}

func (i *Indexer) Clear() error {
	i.mu.Lock()
	ids := make([]string, 0, len(i.manifest.Files))
	for _, ent := range i.manifest.Files {
		ids = append(ids, ent.ID)
	}
	i.manifest = manifest{}
	i.status = Status{}
	i.mu.Unlock()

	for _, id := range ids {
		_ = os.Remove(i.chunkFilePath(id))
		i.removeVectorFile(id)
	}
	return i.saveManifest()
}

func (i *Indexer) Search(query string, maxResults int, restrictPaths []string) ([]Chunk, error) {
	return i.SearchContext(context.Background(), query, maxResults, restrictPaths)
}

func (i *Indexer) SearchContext(ctx context.Context, query string, maxResults int, restrictPaths []string) ([]Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("query is required")
	}
	if maxResults <= 0 {
		maxResults = 20
	}
	qLower := strings.ToLower(q)

	restrict := map[string]struct{}{}
	for _, p := range restrictPaths {
		p = filepath.ToSlash(filepath.Clean(p))
		if p == "" || p == "." {
			continue
		}
		restrict[p] = struct{}{}
	}

	i.mu.Lock()
	entries := make(map[string]fileEntry, len(i.manifest.Files))
	paths := make([]string, 0, len(i.manifest.Files))
	for p, entry := range i.manifest.Files {
		if len(restrict) > 0 {
			if _, ok := restrict[p]; !ok {
				continue
			}
		}
		paths = append(paths, p)
		entries[p] = entry
	}
	i.mu.Unlock()

	sort.Strings(paths)

	var queryVector []float32
	if i.embedder != nil && i.embeddingRetryReady() {
		vectors, err := i.embedder.Embed(ctx, embedding.TaskQuery, []string{q})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			i.recordEmbeddingFailure()
			i.setError(fmt.Errorf("embed search query: %w", err))
		} else if len(vectors) == 1 {
			queryVector = append([]float32(nil), vectors[0]...)
			if err := normalizeVector(queryVector); err != nil {
				i.recordEmbeddingFailure()
				i.setError(fmt.Errorf("normalize search query embedding: %w", err))
				queryVector = nil
			} else {
				i.recordEmbeddingSuccess()
			}
		} else {
			i.recordEmbeddingFailure()
			i.setError(fmt.Errorf("embed search query: embedder returned %d vectors, expected 1", len(vectors)))
		}
	}

	type scored struct {
		chunk    Chunk
		lexical  int
		semantic float64
		hybrid   float64
	}
	results := make([]scored, 0, min(maxResults, 128))
	maxLexical := 0
	semanticUsed := false

	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := entries[p]
		chunks, err := i.readChunksFile(entry.ID)
		if err != nil {
			continue
		}
		vectorByLine := map[[2]int][]float32{}
		if len(queryVector) > 0 && entry.Vectors > 0 {
			vectors, err := i.readVectorsFile(entry.ID)
			if err != nil {
				i.markVectorsStale(p, entry, fmt.Errorf("read vectors: %w", err))
			} else if len(vectors) != len(chunks) {
				i.markVectorsStale(p, entry, fmt.Errorf("vector count %d does not match chunk count %d", len(vectors), len(chunks)))
			} else if len(vectors) > 0 && len(vectors[0].Values) != len(queryVector) {
				i.markVectorsStale(p, entry, fmt.Errorf("vector dimension %d does not match query dimension %d", len(vectors[0].Values), len(queryVector)))
			} else {
				for _, vector := range vectors {
					vectorByLine[[2]int{vector.StartLine, vector.EndLine}] = vector.Values
				}
				mappingMatches := len(vectorByLine) == len(chunks)
				for _, chunk := range chunks {
					if _, ok := vectorByLine[[2]int{chunk.StartLine, chunk.EndLine}]; !ok {
						mappingMatches = false
						break
					}
				}
				if !mappingMatches {
					i.markVectorsStale(p, entry, errors.New("vector chunk ranges do not match indexed chunks"))
					vectorByLine = map[[2]int][]float32{}
				}
			}
		}
		for _, ch := range chunks {
			lexical := lexicalChunkScore(ch, qLower)
			if lexical > maxLexical {
				maxLexical = lexical
			}
			semantic := 0.0
			hasVector := false
			if values, ok := vectorByLine[[2]int{ch.StartLine, ch.EndLine}]; ok && len(values) == len(queryVector) {
				semantic = dotProduct(queryVector, values)
				if semantic < 0 {
					semantic = 0
				} else if semantic > 1 {
					semantic = 1
				}
				hasVector = true
				semanticUsed = true
			}
			if lexical == 0 && (!hasVector || semantic == 0) {
				continue
			}
			results = append(results, scored{chunk: ch, lexical: lexical, semantic: semantic})
		}
	}

	if semanticUsed {
		semanticWeight := i.cfg.SemanticWeight
		lexicalWeight := 1 - semanticWeight
		for index := range results {
			lexicalNormalized := 0.0
			if maxLexical > 0 {
				lexicalNormalized = float64(results[index].lexical) / float64(maxLexical)
			}
			results[index].hybrid = lexicalWeight*lexicalNormalized + semanticWeight*results[index].semantic
		}
	}

	sort.Slice(results, func(a, b int) bool {
		if semanticUsed && results[a].hybrid != results[b].hybrid {
			return results[a].hybrid > results[b].hybrid
		}
		if results[a].lexical != results[b].lexical {
			return results[a].lexical > results[b].lexical
		}
		if results[a].chunk.Path != results[b].chunk.Path {
			return results[a].chunk.Path < results[b].chunk.Path
		}
		return results[a].chunk.StartLine < results[b].chunk.StartLine
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	out := make([]Chunk, 0, len(results))
	for _, r := range results {
		ch := r.chunk
		if semanticUsed {
			ch.Score = int(math.Round(r.hybrid * 10_000))
		} else {
			ch.Score = r.lexical
		}
		out = append(out, ch)
	}
	return out, nil
}

func lexicalChunkScore(chunk Chunk, queryLower string) int {
	hay := strings.ToLower(chunk.Content)
	if !strings.Contains(hay, queryLower) {
		return 0
	}
	score := 10 + strings.Count(hay, queryLower)
	if strings.Contains(strings.ToLower(chunk.Path), queryLower) {
		score += 5
	}
	return score
}

func dotProduct(a, b []float32) float64 {
	var sum float64
	for index := range a {
		sum += float64(a[index]) * float64(b[index])
	}
	return sum
}

func (i *Indexer) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-i.reqCh:
			var err error
			if req.full {
				err = i.indexAll(req.ctx)
			} else {
				err = i.indexFiles(req.ctx, req.paths)
			}
			req.done <- err
		}
	}
}

func (i *Indexer) poller(ctx context.Context) {
	t := time.NewTicker(i.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = i.IndexAll(ctx)
		}
	}
}

func (i *Indexer) indexFiles(ctx context.Context, relPaths []string) error {
	relPaths = normalizeRelPaths(relPaths)
	if len(relPaths) == 0 {
		return nil
	}
	i.beginIndexPass()

	changed := false
	var totalBytes int64

	i.mu.Lock()
	totalBytes = i.manifest.TotalBytes
	rules := i.ignoreRules
	i.mu.Unlock()

	for _, rel := range relPaths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if rules.matchesPath(rel) {
			continue
		}
		if !shouldIndex(rel, i.cfg.PreferredExts, i.cfg.AllowGlobs, i.cfg.DenyGlobs) {
			continue
		}
		abs, err := safeJoin(i.rootAbs, rel)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > i.cfg.MaxFileBytes {
			continue
		}
		existingSize := i.indexedFileSize(rel)
		if totalBytes-existingSize+info.Size() > i.cfg.MaxTotalBytes {
			continue
		}

		ok, delta, err := i.indexOne(ctx, abs, rel, info)
		if err != nil {
			i.setError(err)
			continue
		}
		if ok {
			totalBytes += delta
			changed = true
		}
	}

	if changed {
		return i.saveManifest()
	}
	return nil
}

func (i *Indexer) indexAll(ctx context.Context) error {
	i.beginIndexPass()
	candidates, err := i.listCandidates(ctx)
	if err != nil {
		i.setError(err)
		return err
	}

	// Remove deleted files.
	i.mu.Lock()
	existing := map[string]struct{}{}
	for _, c := range candidates {
		existing[c.Rel] = struct{}{}
	}
	for rel, ent := range i.manifest.Files {
		if _, ok := existing[rel]; ok {
			continue
		}
		_ = os.Remove(i.chunkFilePath(ent.ID))
		i.removeVectorFile(ent.ID)
		i.manifest.TotalBytes -= ent.Size
		delete(i.manifest.Files, rel)
	}
	i.mu.Unlock()

	var totalBytes int64
	i.mu.Lock()
	totalBytes = i.manifest.TotalBytes
	i.mu.Unlock()

	bytesIndexed := int64(0)
	filesIndexed := 0
	partial := false

	for _, c := range candidates {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		existingSize := i.indexedFileSize(c.Rel)
		if totalBytes-existingSize+c.Size > i.cfg.MaxTotalBytes {
			partial = true
			continue
		}
		ok, delta, err := i.indexOne(ctx, c.Abs, c.Rel, c.Info)
		if err != nil {
			i.setError(err)
			continue
		}
		if ok {
			totalBytes += delta
			bytesIndexed += max64(delta, 0)
			filesIndexed++
		}
	}

	if err := i.saveManifest(); err != nil {
		i.setError(err)
		return err
	}

	i.mu.Lock()
	lastError := i.status.Error
	i.status = Status{
		Ready:         true,
		LastIndexedAt: time.Now().UTC().Format(time.RFC3339),
		FilesIndexed:  filesIndexed,
		BytesIndexed:  bytesIndexed,
		Partial:       partial,
		Error:         lastError,
	}
	i.mu.Unlock()
	return nil
}

type candidate struct {
	Rel  string
	Abs  string
	Size int64
	Info os.FileInfo
}

func (i *Indexer) listCandidates(ctx context.Context) ([]candidate, error) {
	// Snapshot the ignore rules under the lock: ReloadIgnoreRules may swap
	// i.ignoreRules concurrently from the file-watcher goroutine.
	i.mu.Lock()
	rules := i.ignoreRules
	i.mu.Unlock()

	out := make([]candidate, 0, 256)
	err := filepath.WalkDir(i.rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel, err := filepath.Rel(i.rootAbs, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		name := d.Name()
		if d.IsDir() {
			if shouldIgnoreDir(name, i.cfg.ExtraIgnoreDirs) {
				return filepath.SkipDir
			}
			if rel != "." && rules.matchesPath(rel+"/") {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldIgnoreFile(name, i.cfg.ExtraIgnoreGlobs) {
			return nil
		}
		if rules.matchesPath(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() <= 0 || info.Size() > i.cfg.MaxFileBytes {
			return nil
		}
		if !shouldIndex(rel, i.cfg.PreferredExts, i.cfg.AllowGlobs, i.cfg.DenyGlobs) {
			return nil
		}
		out = append(out, candidate{Rel: rel, Abs: path, Size: info.Size(), Info: info})
		return nil
	})
	if err != nil {
		return nil, err
	}

	priority := extPriority(i.cfg.PreferredExts)
	sort.Slice(out, func(a, b int) bool {
		pa := priority[strings.ToLower(filepath.Ext(out[a].Rel))]
		pb := priority[strings.ToLower(filepath.Ext(out[b].Rel))]
		if pa != pb {
			return pa < pb
		}
		if out[a].Size != out[b].Size {
			return out[a].Size < out[b].Size
		}
		return out[a].Rel < out[b].Rel
	})
	return out, nil
}

func (i *Indexer) indexOne(ctx context.Context, abs, rel string, info os.FileInfo) (changed bool, deltaBytes int64, err error) {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "" || rel == "." {
		return false, 0, nil
	}

	i.mu.Lock()
	ent, ok := i.manifest.Files[rel]
	i.mu.Unlock()

	mod := info.ModTime().UnixNano()
	needsVectors := false
	if i.embedder != nil && ok {
		needsVectors = ent.Vectors != ent.Chunks
		if !needsVectors {
			_, statErr := os.Stat(i.vectorFilePath(ent.ID))
			needsVectors = statErr != nil
		}
	}
	if ok && ent.Size == info.Size() && ent.ModTime == mod && !needsVectors {
		return false, 0, nil
	}

	b, err := os.ReadFile(abs)
	if err != nil {
		return false, 0, err
	}
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:16])

	id := fileID(rel)
	content := i.redactor.Redact(string(b))
	chunks := ChunkFile(rel, guessLanguage(rel), content, i.cfg.MaxChunkLines, i.cfg.MaxChunkBytes)
	if err := i.writeChunksFile(id, chunks); err != nil {
		return false, 0, err
	}
	vectorCount := 0
	if i.embedder != nil && len(chunks) > 0 {
		vectors, embedErr := i.embedChunks(ctx, chunks)
		if embedErr != nil {
			i.removeVectorFile(id)
			if !errors.Is(embedErr, errEmbeddingBackoff) {
				i.setError(fmt.Errorf("embed %s: %w", rel, embedErr))
			}
		} else if err := i.writeVectorsFile(id, i.embeddingFingerprint(), vectors); err != nil {
			i.removeVectorFile(id)
			i.setError(fmt.Errorf("persist vectors for %s: %w", rel, err))
		} else {
			vectorCount = len(vectors)
		}
	} else {
		i.removeVectorFile(id)
	}

	newEntry := fileEntry{
		ID:       id,
		Size:     info.Size(),
		ModTime:  mod,
		Hash:     hash,
		Language: guessLanguage(rel),
		Chunks:   len(chunks),
		Vectors:  vectorCount,
	}

	i.mu.Lock()
	if i.manifest.Files == nil {
		i.manifest.Files = map[string]fileEntry{}
	}
	oldSize := int64(0)
	if ok {
		oldSize = ent.Size
	}
	i.manifest.Files[rel] = newEntry
	i.manifest.TotalBytes = i.manifest.TotalBytes - oldSize + newEntry.Size
	i.mu.Unlock()

	return true, newEntry.Size - oldSize, nil
}

func (i *Indexer) setError(err error) {
	i.mu.Lock()
	i.status.Error = err.Error()
	i.mu.Unlock()
}

func (i *Indexer) indexedFileSize(rel string) int64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.manifest.Files[rel].Size
}

func (i *Indexer) markVectorsStale(rel string, snapshot fileEntry, cause error) {
	i.mu.Lock()
	current, ok := i.manifest.Files[rel]
	if !ok || current.ID != snapshot.ID || current.Hash != snapshot.Hash || current.ModTime != snapshot.ModTime {
		i.mu.Unlock()
		return
	}
	current.Vectors = 0
	i.manifest.Files[rel] = current
	i.status.Error = fmt.Sprintf("semantic vectors for %s are stale: %v", rel, cause)
	i.mu.Unlock()
}

func (i *Indexer) beginIndexPass() {
	i.mu.Lock()
	if i.embedder == nil || !time.Now().Before(i.embeddingBackoffUntil) {
		i.status.Error = ""
	}
	i.mu.Unlock()
}

func (i *Indexer) embedChunks(ctx context.Context, chunks []Chunk) ([]chunkVector, error) {
	if !i.embeddingRetryReady() {
		return nil, errEmbeddingBackoff
	}
	out := make([]chunkVector, 0, len(chunks))
	batchSize := i.cfg.EmbeddingBatchSize
	for start := 0; start < len(chunks); start += batchSize {
		end := min(start+batchSize, len(chunks))
		inputs := make([]string, 0, end-start)
		for _, chunk := range chunks[start:end] {
			inputs = append(inputs, fmt.Sprintf("path: %s\nlanguage: %s\n%s", chunk.Path, chunk.Language, chunk.Content))
		}
		vectors, err := i.embedder.Embed(ctx, embedding.TaskDocument, inputs)
		if err != nil {
			if ctx.Err() == nil {
				i.recordEmbeddingFailure()
			}
			return nil, err
		}
		if len(vectors) != len(inputs) {
			i.recordEmbeddingFailure()
			return nil, fmt.Errorf("embedder returned %d vectors for %d chunks", len(vectors), len(inputs))
		}
		for offset, values := range vectors {
			values = append([]float32(nil), values...)
			if err := normalizeVector(values); err != nil {
				i.recordEmbeddingFailure()
				return nil, fmt.Errorf("chunk %d: %w", start+offset, err)
			}
			chunk := chunks[start+offset]
			out = append(out, chunkVector{StartLine: chunk.StartLine, EndLine: chunk.EndLine, Values: values})
		}
	}
	i.recordEmbeddingSuccess()
	return out, nil
}

func (i *Indexer) embeddingRetryReady() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return !time.Now().Before(i.embeddingBackoffUntil)
}

func (i *Indexer) recordEmbeddingFailure() {
	i.mu.Lock()
	i.embeddingBackoffUntil = time.Now().Add(30 * time.Second)
	i.mu.Unlock()
}

func (i *Indexer) recordEmbeddingSuccess() {
	i.mu.Lock()
	i.embeddingBackoffUntil = time.Time{}
	i.mu.Unlock()
}

func normalizeVector(vector []float32) error {
	if len(vector) == 0 || len(vector) > maxVectorDimension {
		return fmt.Errorf("invalid vector dimension %d", len(vector))
	}
	var normSquared float64
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("vector contains a non-finite value")
		}
		normSquared += float64(value) * float64(value)
	}
	if normSquared == 0 {
		return errors.New("vector has zero magnitude")
	}
	inverseNorm := 1 / math.Sqrt(normSquared)
	for index := range vector {
		vector[index] = float32(float64(vector[index]) * inverseNorm)
	}
	return nil
}

func (i *Indexer) loadManifest() error {
	b, err := os.ReadFile(filepath.Join(i.dir, "manifest.json"))
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if m.Version != 1 {
		return errors.New("unsupported manifest version")
	}
	i.mu.Lock()
	i.manifest = m
	i.mu.Unlock()
	return nil
}

func (i *Indexer) saveManifest() error {
	i.mu.Lock()
	i.manifest.Version = 1
	i.manifest.Root = i.rootAbs
	i.manifest.RedactionFingerprint = i.redactor.Fingerprint()
	i.manifest.EmbeddingFingerprint = i.embeddingFingerprint()
	i.manifest.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(i.manifest, "", "  ")
	i.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := filepath.Join(i.dir, "manifest.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(i.dir, "manifest.json"))
}

func (i *Indexer) chunkFilePath(id string) string {
	return filepath.Join(i.dir, "files", id+".jsonl")
}

func (i *Indexer) writeChunksFile(id string, chunks []Chunk) error {
	p := i.chunkFilePath(id)
	tmp := p + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, ch := range chunks {
		if err := enc.Encode(ch); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

func (i *Indexer) readChunksFile(id string) ([]Chunk, error) {
	p := i.chunkFilePath(id)
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	out := make([]Chunk, 0, 16)
	for dec.More() {
		var ch Chunk
		if err := dec.Decode(&ch); err != nil {
			return out, err
		}
		out = append(out, ch)
	}
	return out, nil
}

type manifest struct {
	Version              int                  `json:"version"`
	Root                 string               `json:"root"`
	UpdatedAt            string               `json:"updatedAt"`
	RedactionFingerprint string               `json:"redactionFingerprint"`
	EmbeddingFingerprint string               `json:"embeddingFingerprint"`
	TotalBytes           int64                `json:"totalBytes"`
	Files                map[string]fileEntry `json:"files"`
}

func (i *Indexer) resetIndexFiles() error {
	filesDir := filepath.Join(i.dir, "files")
	if err := os.RemoveAll(filesDir); err != nil {
		return err
	}
	for _, name := range []string{"manifest.json", "manifest.json.tmp"} {
		if err := os.Remove(filepath.Join(i.dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return err
	}
	i.manifest = manifest{
		RedactionFingerprint: i.redactor.Fingerprint(),
		EmbeddingFingerprint: i.embeddingFingerprint(),
		Files:                map[string]fileEntry{},
	}
	return nil
}

type fileEntry struct {
	ID       string `json:"id"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"`
	Hash     string `json:"hash"`
	Language string `json:"language"`
	Chunks   int    `json:"chunks"`
	Vectors  int    `json:"vectors,omitempty"`
}

func (i *Indexer) embeddingFingerprint() string {
	if i.embedder == nil {
		return "disabled"
	}
	return i.embedder.Fingerprint()
}

func repoIndexDir(rootAbs string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(rootAbs))
	repoID := hex.EncodeToString(sum[:16])
	return filepath.Join(home, ".memento-mcp", "repos", repoID, "index", "v1"), nil
}

func fileID(rel string) string {
	sum := sha256.Sum256([]byte(rel))
	return hex.EncodeToString(sum[:16])
}

func safeJoin(rootAbs, rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimPrefix(rel, "./")
	rootAbs = filepath.Clean(rootAbs)

	joined := filepath.Join(rootAbs, filepath.FromSlash(rel))
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return "", errors.New("path escapes workspace root")
	}
	return abs, nil
}

func shouldIgnoreDir(name string, extra []string) bool {
	for _, d := range extra {
		if name == d {
			return true
		}
	}
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", "out", ".vscode", ".idea", ".memento-mcp":
		return true
	default:
		return strings.HasPrefix(name, ".git")
	}
}

func shouldIgnoreFile(name string, extraGlobs []string) bool {
	if name == "" {
		return true
	}
	for _, g := range extraGlobs {
		if ok, _ := filepath.Match(g, name); ok {
			return true
		}
	}
	low := strings.ToLower(name)
	if strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".jpg") || strings.HasSuffix(low, ".jpeg") || strings.HasSuffix(low, ".gif") || strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, ".pdf") {
		return true
	}
	if name == "server" {
		return true
	}
	return false
}

func isPreferredExt(rel string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(rel))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

func extPriority(exts []string) map[string]int {
	m := map[string]int{}
	for i, e := range exts {
		m[e] = i
	}
	return m
}

func normalizeRelPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, p := range paths {
		p = filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
		if p == "" || p == "." {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func applyDefaults(cfg *Config) {
	if cfg.MaxTotalBytes <= 0 {
		cfg.MaxTotalBytes = 20 * 1024 * 1024
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 1 * 1024 * 1024
	}
	if cfg.MaxChunkBytes <= 0 {
		cfg.MaxChunkBytes = 8 * 1024
	}
	if cfg.MaxChunkLines <= 0 {
		cfg.MaxChunkLines = 200
	}
	if math.IsNaN(cfg.SemanticWeight) || math.IsInf(cfg.SemanticWeight, 0) || cfg.SemanticWeight <= 0 || cfg.SemanticWeight > 1 {
		cfg.SemanticWeight = embedding.DefaultSemanticWeight
	}
	if cfg.EmbeddingBatchSize <= 0 {
		cfg.EmbeddingBatchSize = embedding.DefaultBatchSize
	}
	if len(cfg.PreferredExts) == 0 {
		cfg.PreferredExts = []string{".go", ".ts", ".tsx", ".js", ".jsx", ".php", ".md", ".json", ".yaml", ".yml"}
	}
	if len(cfg.AllowGlobs) == 0 {
		cfg.AllowGlobs = []string{
			"go.mod",
			"go.sum",
			"README*",
			"Makefile",
			"Dockerfile",
			".github/workflows/*",
			"Taskfile.yml",
			"Taskfile.yaml",
		}
	}
	if len(cfg.DenyGlobs) == 0 {
		cfg.DenyGlobs = []string{
			".env*",
			"*.key",
			"*.pem",
			"*.p12",
			"*.pfx",
			"*.crt",
			"*.der",
			"*.ppk",
			"id_rsa",
			"id_ed25519",
			"*.sqlite",
			"*.db",
			"*.bin",
			"*.exe",
		}
	}
}

func guessLanguage(rel string) string {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return "ts/js"
	case ".php":
		return "php"
	default:
		return "text"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func shouldIndex(rel string, exts, allowGlobs, denyGlobs []string) bool {
	if matchAnyGlob(rel, denyGlobs) {
		return false
	}
	if matchAnyGlob(rel, allowGlobs) {
		return true
	}
	return isPreferredExt(rel, exts)
}

func matchAnyGlob(rel string, globs []string) bool {
	if len(globs) == 0 {
		return false
	}
	rel = filepath.ToSlash(rel)
	base := pathBase(rel)
	for _, g := range globs {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		g = filepath.ToSlash(g)
		if ok, _ := pathMatch(g, rel); ok {
			return true
		}
		if ok, _ := pathMatch(g, base); ok {
			return true
		}
	}
	return false
}

func pathMatch(pattern, name string) (bool, error) {
	// Use path-style matching (always forward slashes).
	return path.Match(pattern, name)
}

func pathBase(rel string) string {
	if idx := strings.LastIndex(rel, "/"); idx >= 0 {
		return rel[idx+1:]
	}
	return rel
}
