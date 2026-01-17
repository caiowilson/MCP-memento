package indexing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	RootAbs          string
	StoreDir         string
	MaxTotalBytes    int64
	MaxFileBytes     int64
	MaxChunkBytes    int
	MaxChunkLines    int
	PollInterval     time.Duration
	PreferredExts    []string
	ExtraIgnoreDirs  []string
	ExtraIgnoreGlobs []string
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
	rootAbs string
	dir     string
	cfg     Config

	mu       sync.Mutex
	manifest manifest
	status   Status

	reqCh chan request
}

type request struct {
	ctx   context.Context
	paths []string // repo-relative, posix
	full  bool
	done  chan error
}

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
		rootAbs: rootAbs,
		dir:     dir,
		cfg:     cfg,
		reqCh:   make(chan request, 8),
	}
	if err := idx.loadManifest(); err != nil {
		// keep going with empty manifest
	}
	return idx, nil
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

func (i *Indexer) Status() Status {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.status
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

func (i *Indexer) Search(query string, maxResults int, restrictPaths []string) ([]Chunk, error) {
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
	paths := make([]string, 0, len(i.manifest.Files))
	for p := range i.manifest.Files {
		if len(restrict) > 0 {
			if _, ok := restrict[p]; !ok {
				continue
			}
		}
		paths = append(paths, p)
	}
	i.mu.Unlock()

	sort.Strings(paths)

	type scored struct {
		chunk Chunk
		score int
	}
	results := make([]scored, 0, min(maxResults, 32))

	for _, p := range paths {
		chunks, err := i.FileChunks(p)
		if err != nil {
			continue
		}
		for _, ch := range chunks {
			hay := strings.ToLower(ch.Content)
			if !strings.Contains(hay, qLower) {
				continue
			}
			score := 10 + strings.Count(hay, qLower)
			if strings.Contains(strings.ToLower(ch.Path), qLower) {
				score += 5
			}
			results = append(results, scored{chunk: ch, score: score})
		}
	}

	sort.Slice(results, func(a, b int) bool {
		if results[a].score != results[b].score {
			return results[a].score > results[b].score
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
		ch.Score = r.score
		out = append(out, ch)
	}
	return out, nil
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

	changed := false
	var totalBytes int64

	i.mu.Lock()
	totalBytes = i.manifest.TotalBytes
	i.mu.Unlock()

	for _, rel := range relPaths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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
		if totalBytes+info.Size() > i.cfg.MaxTotalBytes {
			continue
		}

		ok, delta, err := i.indexOne(abs, rel, info)
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

		if totalBytes+c.Size > i.cfg.MaxTotalBytes {
			partial = true
			continue
		}
		ok, delta, err := i.indexOne(c.Abs, c.Rel, c.Info)
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
	i.status = Status{
		Ready:         true,
		LastIndexedAt: time.Now().UTC().Format(time.RFC3339),
		FilesIndexed:  filesIndexed,
		BytesIndexed:  bytesIndexed,
		Partial:       partial,
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
		name := d.Name()
		if d.IsDir() {
			if shouldIgnoreDir(name, i.cfg.ExtraIgnoreDirs) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldIgnoreFile(name, i.cfg.ExtraIgnoreGlobs) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() <= 0 || info.Size() > i.cfg.MaxFileBytes {
			return nil
		}
		rel, err := filepath.Rel(i.rootAbs, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !isPreferredExt(rel, i.cfg.PreferredExts) {
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

func (i *Indexer) indexOne(abs, rel string, info os.FileInfo) (changed bool, deltaBytes int64, err error) {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "" || rel == "." {
		return false, 0, nil
	}

	i.mu.Lock()
	ent, ok := i.manifest.Files[rel]
	i.mu.Unlock()

	mod := info.ModTime().UnixNano()
	if ok && ent.Size == info.Size() && ent.ModTime == mod {
		return false, 0, nil
	}

	b, err := os.ReadFile(abs)
	if err != nil {
		return false, 0, err
	}
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:16])

	id := fileID(rel)
	chunks := ChunkFile(rel, guessLanguage(rel), string(b), i.cfg.MaxChunkLines, i.cfg.MaxChunkBytes)
	if err := i.writeChunksFile(id, chunks); err != nil {
		return false, 0, err
	}

	newEntry := fileEntry{
		ID:       id,
		Size:     info.Size(),
		ModTime:  mod,
		Hash:     hash,
		Language: guessLanguage(rel),
		Chunks:   len(chunks),
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
	Version    int                  `json:"version"`
	Root       string               `json:"root"`
	UpdatedAt  string               `json:"updatedAt"`
	TotalBytes int64                `json:"totalBytes"`
	Files      map[string]fileEntry `json:"files"`
}

type fileEntry struct {
	ID       string `json:"id"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"`
	Hash     string `json:"hash"`
	Language string `json:"language"`
	Chunks   int    `json:"chunks"`
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
	if len(cfg.PreferredExts) == 0 {
		cfg.PreferredExts = []string{".go", ".ts", ".tsx", ".js", ".jsx", ".php", ".md", ".json", ".yaml", ".yml"}
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
