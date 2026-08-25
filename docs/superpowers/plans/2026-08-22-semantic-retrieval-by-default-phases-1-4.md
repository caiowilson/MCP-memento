# Semantic Retrieval by Default (Phases 1–4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make semantic retrieval the default mode, with lexical fallback reported as a healthy state rather than an error.

**Architecture:** Availability is separated from configuration. A `Runtime` decorator in `internal/embedding` wraps the concrete embedder and owns probe state, backoff, and failure classification; the concrete embedder stays purely "configured" so the embedding fingerprint never encodes reachability. Vector gaps are repaired by embedding chunks already persisted on disk, so an unreachable runtime never forces source files to be re-read or re-chunked.

**Tech Stack:** Go 1.25.5, standard library only. `CGO_ENABLED=0`. Tests use `testing` plus `net/http/httptest`.

**Spec:** `docs/superpowers/specs/2026-08-22-semantic-retrieval-by-default-design.md`

## Global Constraints

- Go 1.25.5. No new module dependencies. `go.mod` must not change.
- `CGO_ENABLED=0` must keep working for all six release targets. No cgo, no build tags that break cross-compilation.
- Every failure path degrades to lexical retrieval and keeps serving. No embedding failure may fail a tool call.
- `status.Error` is reserved for genuine faults. A missing runtime in `auto` mode is not an error.
- Embedding identity describes what the vectors were made with, never what is reachable right now.
- Run `gofmt -l internal/ cmd/` (must print nothing) and `go vet ./...` before every commit.
- Redaction is unchanged: chunks are redacted before embedding.

## Scope

This plan covers phases 1–4 of the spec: the safety fixes, the availability decorator, status and doctor reporting, and the default flip. It delivers semantic-by-default on the existing Ollama provider.

Phases 5–6 of the spec (the llama.cpp provider, process supervision, and consented provisioning) are **not** in this plan and get their own plan.

## File Structure

**Created:**

- `internal/indexing/vector_backfill.go` — finds files with missing or incomplete vectors and repairs them from persisted chunks. Kept out of `indexer.go`, which is already 42 KB.
- `internal/indexing/vector_backfill_test.go` — backfill and no-re-read tests.
- `internal/embedding/mode.go` — the tri-state `Mode` type and `ParseMode`.
- `internal/embedding/mode_test.go`
- `internal/embedding/runtime.go` — the `Runtime` availability decorator.
- `internal/embedding/runtime_test.go`
- `internal/indexing/semantic_status.go` — derives the `semantic` status block from the manifest and the runtime.
- `internal/indexing/semantic_status_test.go`
- `internal/app/doctor_semantic.go` — the doctor semantic section.
- `internal/app/doctor_semantic_test.go`

**Modified:**

- `internal/indexing/indexer.go` — drop `needsVectors` from the skip condition; call backfill; fix `embeddingFingerprint`; add the `semantic` field to `Status`.
- `internal/embedding/config.go` — `RuntimeConfig.Enabled bool` becomes `Mode`; wrap the embedder in `Runtime`.
- `internal/mcp/server.go` — pass the runtime through `indexerConfig`.
- `internal/feedback/feedback.go` — `SemanticRetrieval` reads the mode instead of an opt-in bool.
- `internal/app/cli.go` — `defaultMCPEnv` gains `MEMENTO_SEMANTIC_ENABLED`.
- `internal/app/doctor.go` — call the semantic section.
- `docs/adr/ADRs.md`, `docs/README.md` — ADR supersession and documentation.

---

### Task 1: Repair vector gaps without re-reading source

Today `indexOne` treats a missing vector as a reason to re-read, re-hash, and re-chunk the whole file. When the embedder is unreachable that condition never clears, so every file is reprocessed on every poll cycle forever. The fix is to make vector gaps repairable from the chunks already on disk.

**Files:**

- Modify: `internal/indexing/indexer.go` (the skip condition in `indexOne`, around lines 1058–1070; the `indexAll` tail, around line 950)
- Create: `internal/indexing/vector_backfill.go`
- Test: `internal/indexing/vector_backfill_test.go`

**Interfaces:**

- Consumes: `readChunksFile(id string) ([]Chunk, error)`, `embedChunks(ctx context.Context, chunks []Chunk) ([]chunkVector, error)`, `writeVectorsFile(id, fingerprint string, vectors []chunkVector) error`, `vectorFilePath(id string) string`, `embeddingRetryReady() bool`, `errEmbeddingBackoff`.
- Produces: `func (i *Indexer) pendingVectorFiles() []string`, `func (i *Indexer) pendingVectorCount() int`, `func (i *Indexer) backfillVectors(ctx context.Context) error`. Task 6 uses `pendingVectorCount`.

- [ ] **Step 1: Write the failing test**

Create `internal/indexing/vector_backfill_test.go`. The no-re-read assertion works by rewriting file content to the same length and restoring the original mtime: if the indexer re-reads, the stored hash changes; if it correctly skips, the hash is unchanged.

```go
package indexing

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "testing"
    "time"

    "memento-mcp/internal/embedding"
)

// flakyEmbedder fails until release is closed, then succeeds.
type flakyEmbedder struct {
    release chan struct{}
    calls   int
}

func (e *flakyEmbedder) Embed(_ context.Context, _ embedding.Task, inputs []string) ([][]float32, error) {
    e.calls++
    select {
    case <-e.release:
    default:
        return nil, errors.New("embedding unavailable")
    }
    out := make([][]float32, len(inputs))
    for index := range out {
        out[index] = []float32{1, 0}
    }
    return out, nil
}

func (*flakyEmbedder) Fingerprint() string { return "flaky-v1" }
func (*flakyEmbedder) Name() string        { return "test/flaky" }

func backfillFixture(t *testing.T) (root, store string) {
    t.Helper()
    root = t.TempDir()
    store = t.TempDir()
    body := "package alpha\n\nfunc Alpha() string { return \"alpha\" }\n"
    if err := os.WriteFile(filepath.Join(root, "alpha.go"), []byte(body), 0o644); err != nil {
        t.Fatal(err)
    }
    return root, store
}

func TestUnavailableEmbedderDoesNotReReadSource(t *testing.T) {
    root, store := backfillFixture(t)
    embedder := &flakyEmbedder{release: make(chan struct{})}
    idx, err := New(Config{RootAbs: root, StoreDir: store, Embedder: embedder})
    if err != nil {
        t.Fatal(err)
    }
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    idx.Start(ctx)
    if err := idx.IndexAll(ctx); err != nil {
        t.Fatal(err)
    }

    idx.mu.Lock()
    first := idx.manifest.Files["alpha.go"]
    idx.mu.Unlock()
    if first.Vectors != 0 {
        t.Fatalf("expected no vectors while embedder fails, got %d", first.Vectors)
    }

    // Same length, different content, original mtime restored. Only a re-read
    // can observe this change.
    path := filepath.Join(root, "alpha.go")
    info, err := os.Stat(path)
    if err != nil {
        t.Fatal(err)
    }
    tampered := "package alpha\n\nfunc Alpha() string { return \"BRAVO\" }\n"
    if len(tampered) != int(info.Size()) {
        t.Fatalf("fixture error: tampered length %d != original %d", len(tampered), info.Size())
    }
    if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
        t.Fatal(err)
    }
    if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
        t.Fatal(err)
    }

    if err := idx.IndexAll(ctx); err != nil {
        t.Fatal(err)
    }
    idx.mu.Lock()
    second := idx.manifest.Files["alpha.go"]
    idx.mu.Unlock()
    if second.Hash != first.Hash {
        t.Fatal("file was re-read and re-chunked while the embedder was unavailable")
    }
}

func TestBackfillFillsVectorsFromPersistedChunks(t *testing.T) {
    root, store := backfillFixture(t)
    embedder := &flakyEmbedder{release: make(chan struct{})}
    idx, err := New(Config{RootAbs: root, StoreDir: store, Embedder: embedder})
    if err != nil {
        t.Fatal(err)
    }
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    idx.Start(ctx)
    if err := idx.IndexAll(ctx); err != nil {
        t.Fatal(err)
    }
    idx.mu.Lock()
    before := idx.manifest.Files["alpha.go"]
    idx.mu.Unlock()
    if before.Vectors != 0 {
        t.Fatalf("expected 0 vectors, got %d", before.Vectors)
    }
    if got := idx.pendingVectorCount(); got != 1 {
        t.Fatalf("pendingVectorCount = %d, want 1", got)
    }

    close(embedder.release)
    idx.clearEmbeddingBackoffForTest()

    if err := idx.IndexAll(ctx); err != nil {
        t.Fatal(err)
    }
    idx.mu.Lock()
    after := idx.manifest.Files["alpha.go"]
    idx.mu.Unlock()
    if after.Vectors != after.Chunks {
        t.Fatalf("backfill left %d vectors for %d chunks", after.Vectors, after.Chunks)
    }
    if after.Hash != before.Hash {
        t.Fatal("backfill re-read the source instead of using persisted chunks")
    }
    if got := idx.pendingVectorCount(); got != 0 {
        t.Fatalf("pendingVectorCount = %d, want 0", got)
    }
    if _, err := os.Stat(idx.vectorFilePath(after.ID)); err != nil {
        t.Fatalf("expected vector sidecar: %v", err)
    }
    _ = time.Now
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexing/ -run 'TestUnavailableEmbedderDoesNotReRead|TestBackfillFills' -v`

Expected: FAIL to compile — `idx.pendingVectorCount undefined` and `idx.clearEmbeddingBackoffForTest undefined`.

- [ ] **Step 3: Create the backfill file**

Create `internal/indexing/vector_backfill.go`:

```go
package indexing

import (
    "context"
    "errors"
    "fmt"
    "os"
    "sort"
)

// pendingVectorFiles returns indexed files whose vectors are missing or
// incomplete. The manifest is authoritative, so this costs no file I/O beyond
// confirming the sidecar exists.
func (i *Indexer) pendingVectorFiles() []string {
    i.mu.Lock()
    defer i.mu.Unlock()
    if i.embedder == nil {
        return nil
    }
    pending := make([]string, 0)
    for rel, ent := range i.manifest.Files {
        if ent.Chunks == 0 {
            continue
        }
        if ent.Vectors != ent.Chunks {
            pending = append(pending, rel)
            continue
        }
        if _, err := os.Stat(i.vectorFilePath(ent.ID)); err != nil {
            pending = append(pending, rel)
        }
    }
    sort.Strings(pending)
    return pending
}

// pendingVectorCount reports how many indexed files are still awaiting
// vectors. Status uses it to distinguish "warming up" from "broken".
func (i *Indexer) pendingVectorCount() int {
    return len(i.pendingVectorFiles())
}

// backfillVectors embeds chunks that are already persisted. Repairing a vector
// gap must never require re-reading, re-hashing, or re-chunking the source.
func (i *Indexer) backfillVectors(ctx context.Context) error {
    if i.embedder == nil || !i.embeddingRetryReady() {
        return nil
    }
    changed := false
    for _, rel := range i.pendingVectorFiles() {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        i.mu.Lock()
        ent, ok := i.manifest.Files[rel]
        i.mu.Unlock()
        if !ok {
            continue
        }
        chunks, err := i.readChunksFile(ent.ID)
        if err != nil || len(chunks) == 0 {
            continue
        }
        vectors, err := i.embedChunks(ctx, chunks)
        if err != nil {
            if ctx.Err() != nil {
                return ctx.Err()
            }
            if !errors.Is(err, errEmbeddingBackoff) {
                i.setError(fmt.Errorf("backfill vectors for %s: %w", rel, err))
            }
            // The runtime is down. Remaining files stay pending for a later pass.
            break
        }
        if len(vectors) == 0 {
            continue
        }

        i.mu.Lock()
        current, stillPresent := i.manifest.Files[rel]
        if stillPresent && current.ID == ent.ID && current.Hash == ent.Hash {
            if writeErr := i.writeVectorsFile(ent.ID, i.embeddingFingerprint(), vectors); writeErr != nil {
                i.removeVectorFile(ent.ID)
                i.status.Error = fmt.Sprintf("persist vectors for %s: %v", rel, writeErr)
            } else {
                current.Vectors = len(vectors)
                i.manifest.Files[rel] = current
                changed = true
            }
        }
        i.mu.Unlock()
    }
    if !changed {
        return nil
    }
    return i.saveManifest()
}
```

- [ ] **Step 4: Drop the vector gap from the skip condition**

In `internal/indexing/indexer.go`, inside `indexOne`, delete the `needsVectors` block and simplify the skip. Replace:

```go
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
        _ = file.Close()
        return false, 0, nil
    }
```

with:

```go
    mod := info.ModTime().UnixNano()
    // Content freshness alone decides whether the source must be reprocessed.
    // Missing vectors are repaired by backfillVectors from the persisted
    // chunks, so a vector gap never forces a re-read or a re-chunk.
    if ok && ent.Size == info.Size() && ent.ModTime == mod {
        _ = file.Close()
        return false, 0, nil
    }
```

- [ ] **Step 5: Run backfill at the end of each index pass**

In `internal/indexing/indexer.go`, in `indexAll`, insert the backfill call immediately before the existing `if err := i.saveManifest(); err != nil {` that follows the candidate loop:

```go
    if err := i.backfillVectors(ctx); err != nil {
        if ctx.Err() != nil {
            return err
        }
        i.setError(err)
    }

    if err := i.saveManifest(); err != nil {
```

- [ ] **Step 6: Add the test-only backoff reset**

In `internal/indexing/indexer.go`, next to `recordEmbeddingSuccess`:

```go
// clearEmbeddingBackoffForTest resets the retry window so tests can simulate a
// runtime coming back without sleeping.
func (i *Indexer) clearEmbeddingBackoffForTest() {
    i.recordEmbeddingSuccess()
}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/indexing/ -run 'TestUnavailableEmbedderDoesNotReRead|TestBackfillFills' -v`

Expected: PASS, both tests.

- [ ] **Step 8: Run the full suite and vet**

Run: `gofmt -l internal/ cmd/ && go vet ./... && go test ./...`

Expected: `gofmt` prints nothing, vet is clean, all packages pass.

- [ ] **Step 9: Commit**

```bash
git add internal/indexing/vector_backfill.go internal/indexing/vector_backfill_test.go internal/indexing/indexer.go
git commit -m "fix: repair vector gaps without re-reading source files"
```

---

### Task 2: Preserve vectors when semantic is off

`embeddingFingerprint()` returns `"disabled"` when no embedder is configured. `saveManifest` writes that value, so turning semantic off discards the recorded identity and turning it back on resets every sidecar and re-embeds the repository.

**Files:**

- Modify: `internal/indexing/indexer.go` (`embeddingFingerprint`, around line 1430)
- Test: `internal/indexing/vector_backfill_test.go` (append)

**Interfaces:**

- Consumes: `manifest.EmbeddingFingerprint`.
- Produces: no new exported surface. `embeddingFingerprint()` becomes identity-preserving when the embedder is nil.

- [ ] **Step 1: Write the failing test**

Append to `internal/indexing/vector_backfill_test.go`:

```go
func TestVectorsSurviveSemanticOffToggle(t *testing.T) {
    root, store := backfillFixture(t)
    embedder := &flakyEmbedder{release: make(chan struct{})}
    close(embedder.release) // succeed immediately

    warm, err := New(Config{RootAbs: root, StoreDir: store, Embedder: embedder})
    if err != nil {
        t.Fatal(err)
    }
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    warm.Start(ctx)
    if err := warm.IndexAll(ctx); err != nil {
        t.Fatal(err)
    }
    warm.mu.Lock()
    entry := warm.manifest.Files["alpha.go"]
    wantFingerprint := warm.manifest.EmbeddingFingerprint
    warm.mu.Unlock()
    if entry.Vectors == 0 {
        t.Fatal("fixture error: expected vectors to be written")
    }
    vectorPath := warm.vectorFilePath(entry.ID)
    cancel()

    // Reopen with semantic off. Sidecars and identity must survive.
    off, err := New(Config{RootAbs: root, StoreDir: store})
    if err != nil {
        t.Fatal(err)
    }
    if _, statErr := os.Stat(vectorPath); statErr != nil {
        t.Fatalf("semantic off deleted the vector sidecar: %v", statErr)
    }
    off.mu.Lock()
    gotFingerprint := off.manifest.EmbeddingFingerprint
    gotVectors := off.manifest.Files["alpha.go"].Vectors
    off.mu.Unlock()
    if gotFingerprint != wantFingerprint {
        t.Fatalf("fingerprint = %q, want %q", gotFingerprint, wantFingerprint)
    }
    if gotVectors != entry.Vectors {
        t.Fatalf("vectors = %d, want %d", gotVectors, entry.Vectors)
    }

    // Reopen with semantic on again. Nothing should need re-embedding.
    back, err := New(Config{RootAbs: root, StoreDir: store, Embedder: embedder})
    if err != nil {
        t.Fatal(err)
    }
    if got := back.pendingVectorCount(); got != 0 {
        t.Fatalf("pendingVectorCount after toggle = %d, want 0", got)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexing/ -run TestVectorsSurviveSemanticOffToggle -v`

Expected: FAIL — the sidecar is deleted, because `New` sees `"disabled"` differ from the stored fingerprint and calls `resetVectorFiles`.

- [ ] **Step 3: Make the fingerprint identity-preserving**

In `internal/indexing/indexer.go`, replace:

```go
func (i *Indexer) embeddingFingerprint() string {
    if i.embedder == nil {
        return "disabled"
    }
    return i.embedder.Fingerprint()
}
```

with:

```go
func (i *Indexer) embeddingFingerprint() string {
    if i.embedder == nil {
        // Preserve the identity the sidecars were written with. Availability
        // and configuration must never invalidate stored vectors; only a real
        // change of embedding identity does.
        return i.manifest.EmbeddingFingerprint
    }
    return i.embedder.Fingerprint()
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/indexing/ -run TestVectorsSurviveSemanticOffToggle -v`

Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `gofmt -l internal/ cmd/ && go vet ./... && go test ./...`

Expected: all pass. If an existing test asserts the literal `"disabled"` fingerprint, update it to assert identity preservation instead and note the change in the commit body.

- [ ] **Step 6: Commit**

```bash
git add internal/indexing/indexer.go internal/indexing/vector_backfill_test.go
git commit -m "fix: keep vector sidecars when semantic retrieval is off"
```

---

### Task 3: Tri-state semantic mode

`MEMENTO_SEMANTIC_ENABLED` becomes `auto` / `true` / `false`. The default stays `off` in this task; Task 8 flips it. Splitting the parse from the flip keeps the behavior change to a one-line diff that can be reviewed and reverted on its own.

**Files:**

- Create: `internal/embedding/mode.go`
- Create: `internal/embedding/mode_test.go`
- Modify: `internal/embedding/config.go`
- Modify: `internal/embedding/embedder.go` (`RuntimeConfig`)
- Modify: `internal/feedback/feedback.go`

**Interfaces:**

- Produces: `type Mode string`; constants `ModeOff`, `ModeAuto`, `ModeRequired`; `func ParseMode(raw string) (Mode, error)`; `var DefaultMode = ModeOff`; `func (m Mode) Enabled() bool`. `RuntimeConfig.Enabled bool` is replaced by `RuntimeConfig.Mode Mode`. Tasks 4–8 depend on these names.

- [ ] **Step 1: Write the failing test**

Create `internal/embedding/mode_test.go`:

```go
package embedding

import "testing"

func TestParseMode(t *testing.T) {
    cases := []struct {
        raw  string
        want Mode
    }{
        {"", DefaultMode},
        {"auto", ModeAuto},
        {" AUTO ", ModeAuto},
        {"true", ModeRequired},
        {"1", ModeRequired},
        {"T", ModeRequired},
        {"false", ModeOff},
        {"0", ModeOff},
    }
    for _, tc := range cases {
        got, err := ParseMode(tc.raw)
        if err != nil {
            t.Fatalf("ParseMode(%q) error = %v", tc.raw, err)
        }
        if got != tc.want {
            t.Fatalf("ParseMode(%q) = %q, want %q", tc.raw, got, tc.want)
        }
    }
}

func TestParseModeRejectsGarbage(t *testing.T) {
    for _, raw := range []string{"yes", "on", "semantic", "-1"} {
        if _, err := ParseMode(raw); err == nil {
            t.Fatalf("ParseMode(%q) succeeded, want error", raw)
        }
    }
}

func TestModeEnabled(t *testing.T) {
    if ModeOff.Enabled() {
        t.Fatal("ModeOff must not be enabled")
    }
    if !ModeAuto.Enabled() || !ModeRequired.Enabled() {
        t.Fatal("auto and required must be enabled")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/embedding/ -run TestParseMode -v`

Expected: FAIL to compile — `undefined: ParseMode`.

- [ ] **Step 3: Create the mode type**

Create `internal/embedding/mode.go`:

```go
package embedding

import (
    "fmt"
    "strconv"
    "strings"
)

// Mode selects how semantic retrieval behaves.
type Mode string

const (
    // ModeOff disables semantic retrieval. No embedder is constructed.
    ModeOff Mode = "off"
    // ModeAuto uses semantic retrieval when a runtime is reachable and falls
    // back to lexical otherwise. Falling back is a healthy state.
    ModeAuto Mode = "auto"
    // ModeRequired behaves like ModeAuto but reports an unreachable runtime as
    // an error. It still serves lexical results.
    ModeRequired Mode = "required"
)

// DefaultMode is the mode used when MEMENTO_SEMANTIC_ENABLED is unset.
var DefaultMode = ModeOff

// Enabled reports whether an embedder should be constructed.
func (m Mode) Enabled() bool { return m == ModeAuto || m == ModeRequired }

// ParseMode reads the tri-state MEMENTO_SEMANTIC_ENABLED value. The legacy
// boolean spellings keep working; "true" now means "required".
func ParseMode(raw string) (Mode, error) {
    trimmed := strings.ToLower(strings.TrimSpace(raw))
    if trimmed == "" {
        return DefaultMode, nil
    }
    if trimmed == string(ModeAuto) {
        return ModeAuto, nil
    }
    value, err := strconv.ParseBool(trimmed)
    if err != nil {
        return "", fmt.Errorf("parse MEMENTO_SEMANTIC_ENABLED: %q is not auto, true, or false", raw)
    }
    if value {
        return ModeRequired, nil
    }
    return ModeOff, nil
}
```

- [ ] **Step 4: Replace Enabled with Mode on RuntimeConfig**

In `internal/embedding/embedder.go`, change:

```go
type RuntimeConfig struct {
    Enabled        bool
    Embedder       Embedder
    SemanticWeight float64
    BatchSize      int
}
```

to:

```go
type RuntimeConfig struct {
    Mode           Mode
    Embedder       Embedder
    SemanticWeight float64
    BatchSize      int
}
```

- [ ] **Step 5: Use the mode in FromEnv**

In `internal/embedding/config.go`, replace the opening of `FromEnv`:

```go
    enabled, err := envBool("MEMENTO_SEMANTIC_ENABLED", false)
    if err != nil {
        return RuntimeConfig{}, err
    }
    if !enabled {
        return RuntimeConfig{SemanticWeight: DefaultSemanticWeight, BatchSize: DefaultBatchSize}, nil
    }
```

with:

```go
    mode, err := ParseMode(os.Getenv("MEMENTO_SEMANTIC_ENABLED"))
    if err != nil {
        return RuntimeConfig{}, err
    }
    if !mode.Enabled() {
        return RuntimeConfig{Mode: mode, SemanticWeight: DefaultSemanticWeight, BatchSize: DefaultBatchSize}, nil
    }
```

and in the same function change the success return's `Enabled: true` to `Mode: mode`:

```go
    return RuntimeConfig{
        Mode:           mode,
        Embedder:       embedder,
        SemanticWeight: weight,
        BatchSize:      batchSize,
    }, nil
```

If `envBool` now has no callers, delete it; if other callers remain, leave it.

- [ ] **Step 6: Update the feedback flag**

In `internal/feedback/feedback.go`, `SemanticRetrieval: envOptIn("MEMENTO_SEMANTIC_ENABLED")` no longer reflects the tri-state. Replace with a mode-aware read:

```go
            SemanticRetrieval: semanticRetrievalEnabled(),
```

and add to the same file:

```go
// semanticRetrievalEnabled reports whether semantic retrieval is configured.
// It tolerates an invalid value by reporting false; configuration validation
// belongs to embedding.FromEnv, which fails startup on garbage.
func semanticRetrievalEnabled() bool {
    mode, err := embedding.ParseMode(os.Getenv("MEMENTO_SEMANTIC_ENABLED"))
    if err != nil {
        return false
    }
    return mode.Enabled()
}
```

Add the `memento-mcp/internal/embedding` import. If that creates an import cycle, instead duplicate the three-value check locally and note why in a comment.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/embedding/ ./internal/feedback/ -v 2>&1 | tail -20`

Expected: PASS. Update any test still referencing `RuntimeConfig.Enabled` (at minimum `internal/embedding/ollama_test.go:143`) to assert `config.Mode == ModeRequired`.

- [ ] **Step 8: Run the full suite**

Run: `gofmt -l internal/ cmd/ && go vet ./... && go test ./...`

Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add internal/embedding/ internal/feedback/feedback.go
git commit -m "feat: add tri-state semantic mode parsing"
```

---

### Task 4: The `embedding.Runtime` availability decorator

`Runtime` wraps a concrete embedder and owns availability, backoff, and failure classification. The wrapped embedder stays purely "configured", so `Fingerprint()` never changes with reachability.

**Files:**

- Create: `internal/embedding/runtime.go`
- Create: `internal/embedding/runtime_test.go`

**Interfaces:**

- Consumes: `Embedder`, `Task`, `Mode` from Task 3.
- Produces: `type Availability struct{ Available bool; Reason string; CheckedAt time.Time }`; `func NewRuntime(embedder Embedder, mode Mode) *Runtime`; methods `Embed`, `Availability() Availability`, `Mode() Mode`, `Probe(ctx context.Context) Availability`, `Fingerprint() string`, `Name() string`; `var ErrRuntimeUnavailable`. Tasks 5–7 depend on these.

- [ ] **Step 1: Write the failing test**

Create `internal/embedding/runtime_test.go`:

```go
package embedding

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"
)

type scriptedEmbedder struct {
    err   error
    calls int
}

func (e *scriptedEmbedder) Embed(_ context.Context, _ Task, inputs []string) ([][]float32, error) {
    e.calls++
    if e.err != nil {
        return nil, e.err
    }
    out := make([][]float32, len(inputs))
    for index := range out {
        out[index] = []float32{1}
    }
    return out, nil
}

func (*scriptedEmbedder) Fingerprint() string { return "scripted-v1" }
func (*scriptedEmbedder) Name() string        { return "ollama/test-model" }

func TestRuntimeFingerprintIgnoresAvailability(t *testing.T) {
    inner := &scriptedEmbedder{err: errors.New("connection refused")}
    rt := NewRuntime(inner, ModeAuto)
    before := rt.Fingerprint()
    if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
        t.Fatal("expected embed to fail")
    }
    if rt.Availability().Available {
        t.Fatal("runtime should be unavailable after a failure")
    }
    if rt.Fingerprint() != before {
        t.Fatal("fingerprint changed with availability")
    }
    if rt.Fingerprint() != inner.Fingerprint() {
        t.Fatal("fingerprint must delegate to the wrapped embedder")
    }
}

func TestRuntimeClassifiesFailureReasons(t *testing.T) {
    cases := []struct {
        err  error
        want string
    }{
        {errors.New("ollama embedding request: dial tcp 127.0.0.1:11434: connect: connection refused"), "no embedding runtime detected"},
        {errors.New(`ollama embedding request returned 404 Not Found: {"error":"model not found"}`), "is not available"},
        {context.DeadlineExceeded, "did not respond"},
    }
    for _, tc := range cases {
        rt := NewRuntime(&scriptedEmbedder{err: tc.err}, ModeAuto)
        if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
            t.Fatalf("expected failure for %v", tc.err)
        }
        reason := rt.Availability().Reason
        if !strings.Contains(reason, tc.want) {
            t.Fatalf("reason %q does not contain %q", reason, tc.want)
        }
    }
}

func TestRuntimeBackoffSuppressesRepeatedCalls(t *testing.T) {
    inner := &scriptedEmbedder{err: errors.New("connection refused")}
    rt := NewRuntime(inner, ModeAuto)
    rt.backoff = time.Minute

    for attempt := 0; attempt < 3; attempt++ {
        if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
            t.Fatal("expected failure")
        }
    }
    if inner.calls != 1 {
        t.Fatalf("wrapped embedder called %d times, want 1 while in backoff", inner.calls)
    }
    if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); !errors.Is(err, ErrRuntimeUnavailable) {
        t.Fatalf("suppressed call error = %v, want ErrRuntimeUnavailable", err)
    }
}

func TestRuntimeRecoversAfterBackoffWindow(t *testing.T) {
    inner := &scriptedEmbedder{err: errors.New("connection refused")}
    rt := NewRuntime(inner, ModeAuto)
    rt.backoff = time.Minute
    if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err == nil {
        t.Fatal("expected failure")
    }

    inner.err = nil
    rt.expireBackoffForTest()

    if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); err != nil {
        t.Fatalf("expected recovery, got %v", err)
    }
    availability := rt.Availability()
    if !availability.Available || availability.Reason != "" {
        t.Fatalf("availability = %+v, want available with no reason", availability)
    }
}

func TestRuntimeOffModeNeverCalls(t *testing.T) {
    inner := &scriptedEmbedder{}
    rt := NewRuntime(inner, ModeOff)
    if _, err := rt.Embed(context.Background(), TaskDocument, []string{"x"}); !errors.Is(err, ErrRuntimeUnavailable) {
        t.Fatalf("off mode error = %v, want ErrRuntimeUnavailable", err)
    }
    if inner.calls != 0 {
        t.Fatalf("off mode called the embedder %d times", inner.calls)
    }
}

func TestRuntimeProbeUsesSentinel(t *testing.T) {
    inner := &scriptedEmbedder{}
    rt := NewRuntime(inner, ModeAuto)
    availability := rt.Probe(context.Background())
    if !availability.Available {
        t.Fatalf("probe availability = %+v, want available", availability)
    }
    if inner.calls != 1 {
        t.Fatalf("probe made %d calls, want 1", inner.calls)
    }
    if availability.CheckedAt.IsZero() {
        t.Fatal("probe must stamp CheckedAt")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/embedding/ -run TestRuntime -v`

Expected: FAIL to compile — `undefined: NewRuntime`.

- [ ] **Step 3: Implement the decorator**

Create `internal/embedding/runtime.go`:

```go
package embedding

import (
    "context"
    "errors"
    "fmt"
    "net"
    "strings"
    "sync"
    "time"
)

// DefaultRuntimeBackoff is how long an unreachable runtime is left alone
// before the next attempt is allowed through.
const DefaultRuntimeBackoff = 30 * time.Second

// probeSentinel is the input used to test a runtime end to end. A reachability
// check would not notice a runtime whose model was never pulled.
const probeSentinel = "memento embedding runtime probe"

// ErrRuntimeUnavailable is returned when the runtime is disabled or inside its
// backoff window. Callers degrade to lexical retrieval.
var ErrRuntimeUnavailable = errors.New("embedding runtime unavailable")

// Availability describes whether embedding can be attempted right now. It is
// deliberately separate from the embedder's identity: a runtime being down
// must never change what the stored vectors were made with.
type Availability struct {
    Available bool      `json:"available"`
    Reason    string    `json:"reason,omitempty"`
    CheckedAt time.Time `json:"checkedAt,omitempty"`
}

// Runtime wraps a concrete Embedder with availability tracking. It implements
// Embedder, so consumers take it wherever an embedder is accepted.
type Runtime struct {
    mu       sync.Mutex
    embedder Embedder
    mode     Mode
    backoff  time.Duration
    now      func() time.Time

    availability Availability
    probed       bool
    retryAt      time.Time
}

// NewRuntime wraps embedder. A nil embedder yields a runtime that is always
// unavailable, which is the correct shape for ModeOff.
func NewRuntime(embedder Embedder, mode Mode) *Runtime {
    return &Runtime{
        embedder: embedder,
        mode:     mode,
        backoff:  DefaultRuntimeBackoff,
        now:      time.Now,
    }
}

func (r *Runtime) Mode() Mode { return r.mode }

// Fingerprint delegates to the wrapped embedder and never reflects
// availability.
func (r *Runtime) Fingerprint() string {
    if r.embedder == nil {
        return ""
    }
    return r.embedder.Fingerprint()
}

func (r *Runtime) Name() string {
    if r.embedder == nil {
        return ""
    }
    return r.embedder.Name()
}

// Availability returns the last known state without performing any I/O.
func (r *Runtime) Availability() Availability {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.availability
}

// Probe runs one sentinel embed and records the outcome. Doctor and status use
// it to get a fresh answer; indexing does not need it, because a real Embed
// already doubles as a probe.
func (r *Runtime) Probe(ctx context.Context) Availability {
    _, _ = r.embed(ctx, TaskQuery, []string{probeSentinel}, true)
    return r.Availability()
}

// Embed forwards to the wrapped embedder when the runtime is usable. Failures
// mark the runtime unavailable and open a backoff window, so a down runtime is
// attempted at most once per window.
func (r *Runtime) Embed(ctx context.Context, task Task, inputs []string) ([][]float32, error) {
    return r.embed(ctx, task, inputs, false)
}

func (r *Runtime) embed(ctx context.Context, task Task, inputs []string, force bool) ([][]float32, error) {
    if !r.mode.Enabled() || r.embedder == nil {
        return nil, ErrRuntimeUnavailable
    }
    r.mu.Lock()
    if !force && r.probed && !r.availability.Available && r.now().Before(r.retryAt) {
        r.mu.Unlock()
        return nil, ErrRuntimeUnavailable
    }
    r.mu.Unlock()

    vectors, err := r.embedder.Embed(ctx, task, inputs)
    if err != nil {
        // A cancelled context says nothing about the runtime.
        if ctx.Err() != nil {
            return nil, err
        }
        r.markUnavailable(err)
        return nil, err
    }
    r.markAvailable()
    return vectors, nil
}

func (r *Runtime) markAvailable() {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.probed = true
    r.retryAt = time.Time{}
    r.availability = Availability{Available: true, CheckedAt: r.now()}
}

func (r *Runtime) markUnavailable(err error) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.probed = true
    r.retryAt = r.now().Add(r.backoff)
    r.availability = Availability{
        Available: false,
        Reason:    classifyReason(r.name(), err),
        CheckedAt: r.now(),
    }
}

func (r *Runtime) name() string {
    if r.embedder == nil {
        return ""
    }
    return r.embedder.Name()
}

// expireBackoffForTest lets tests simulate the window elapsing.
func (r *Runtime) expireBackoffForTest() {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.retryAt = time.Time{}
}

// classifyReason turns a transport or protocol error into text a user can act
// on. Reason quality is what makes the status hint useful.
func classifyReason(name string, err error) string {
    if err == nil {
        return ""
    }
    message := strings.ToLower(err.Error())

    var netErr net.Error
    if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) || strings.Contains(message, "timeout") {
        return "embedding runtime did not respond in time"
    }
    if strings.Contains(message, "connection refused") || strings.Contains(message, "no such host") || strings.Contains(message, "connect:") {
        return "no embedding runtime detected"
    }
    if strings.Contains(message, "404") || strings.Contains(message, "not found") {
        if name != "" {
            return fmt.Sprintf("model %s is not available in the embedding runtime", name)
        }
        return "the configured model is not available in the embedding runtime"
    }
    return err.Error()
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/embedding/ -run TestRuntime -v`

Expected: PASS, all six tests.

- [ ] **Step 5: Run the full suite**

Run: `gofmt -l internal/ cmd/ && go vet ./... && go test ./...`

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/embedding/runtime.go internal/embedding/runtime_test.go
git commit -m "feat: add embedding runtime availability decorator"
```

---

### Task 5: Wire the runtime through the server

`FromEnv` returns the decorated runtime, so the indexer receives availability-aware embedding without knowing about probes.

**Files:**

- Modify: `internal/embedding/config.go`
- Modify: `internal/indexing/indexer.go` (the embed error paths)
- Test: `internal/embedding/ollama_test.go` (adjust existing assertions)

**Interfaces:**

- Consumes: `NewRuntime`, `ErrRuntimeUnavailable`, `Mode` from Tasks 3–4.
- Produces: `RuntimeConfig.Embedder` now always holds a `*Runtime` when the mode is enabled. Tasks 6–7 type-assert on it.

- [ ] **Step 1: Write the failing test**

Append to `internal/embedding/ollama_test.go`:

```go
func TestFromEnvWrapsEmbedderInRuntime(t *testing.T) {
    t.Setenv("MEMENTO_SEMANTIC_ENABLED", "auto")
    t.Setenv("MEMENTO_OLLAMA_URL", "http://127.0.0.1:11434")
    config, err := FromEnv()
    if err != nil {
        t.Fatal(err)
    }
    if config.Mode != ModeAuto {
        t.Fatalf("mode = %q, want auto", config.Mode)
    }
    runtime, ok := config.Embedder.(*Runtime)
    if !ok {
        t.Fatalf("embedder type = %T, want *Runtime", config.Embedder)
    }
    if runtime.Mode() != ModeAuto {
        t.Fatalf("runtime mode = %q, want auto", runtime.Mode())
    }
    if runtime.Availability().Available {
        t.Fatal("availability must start false until something is attempted")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/embedding/ -run TestFromEnvWrapsEmbedderInRuntime -v`

Expected: FAIL — `embedder type = *embedding.Ollama, want *Runtime`.

- [ ] **Step 3: Wrap the embedder in FromEnv**

In `internal/embedding/config.go`, change the final return of `FromEnv` from `Embedder: embedder` to the wrapped runtime:

```go
    return RuntimeConfig{
        Mode:           mode,
        Embedder:       NewRuntime(embedder, mode),
        SemanticWeight: weight,
        BatchSize:      batchSize,
    }, nil
```

- [ ] **Step 4: Stop treating unavailability as a fault in the indexer**

`ErrRuntimeUnavailable` means "degrade quietly", exactly like the existing `errEmbeddingBackoff`. In `internal/indexing/indexer.go`, update the two places that record an error after a failed embed.

In `indexOne`:

```go
        if embedErr != nil {
            vectors = nil
            if !errors.Is(embedErr, errEmbeddingBackoff) && !errors.Is(embedErr, embedding.ErrRuntimeUnavailable) {
                i.setError(fmt.Errorf("embed %s: %w", rel, embedErr))
            }
        }
```

In the query path of the search function, replace the first failure branch:

```go
        vectors, err := i.embedder.Embed(ctx, embedding.TaskQuery, []string{q})
        if err != nil {
            if ctxErr := ctx.Err(); ctxErr != nil {
                return nil, ctxErr
            }
            i.recordEmbeddingFailure()
            if !errors.Is(err, embedding.ErrRuntimeUnavailable) {
                i.setError(fmt.Errorf("embed search query: %w", err))
            }
        } else if len(vectors) == 1 {
```

In `internal/indexing/vector_backfill.go`, widen the same guard:

```go
            if !errors.Is(err, errEmbeddingBackoff) && !errors.Is(err, embedding.ErrRuntimeUnavailable) {
                i.setError(fmt.Errorf("backfill vectors for %s: %w", rel, err))
            }
```

Add the `memento-mcp/internal/embedding` import to `vector_backfill.go`.

- [ ] **Step 5: Update existing assertions**

`internal/embedding/ollama_test.go:143` asserts `config.Embedder.Name() != "ollama/all-minilm"`. `Runtime.Name()` delegates, so the assertion still holds; change only `config.Enabled` to `config.Mode == ModeRequired`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/embedding/ ./internal/indexing/ -v 2>&1 | tail -20`

Expected: PASS.

- [ ] **Step 7: Run the full suite**

Run: `gofmt -l internal/ cmd/ && go vet ./... && go test ./...`

Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/embedding/ internal/indexing/
git commit -m "feat: wire the embedding runtime through server configuration"
```

---

### Task 6: Report semantic state in status

Status gains a derived `semantic` block. Everything in it is computed on read from the manifest and the runtime, matching how `filesIndexed` was fixed — nothing is cached and allowed to drift.

**Files:**

- Create: `internal/indexing/semantic_status.go`
- Create: `internal/indexing/semantic_status_test.go`
- Modify: `internal/indexing/indexer.go` (`Status` struct and `Status()`)

**Interfaces:**

- Consumes: `pendingVectorCount()` from Task 1; `Availability`, `Mode` from Tasks 3–4.
- Produces: `type SemanticStatus struct`; `Status.Semantic *SemanticStatus`; `func (i *Indexer) semanticStatus() *SemanticStatus`. Task 7 reuses `SemanticStatus`.

- [ ] **Step 1: Write the failing test**

Create `internal/indexing/semantic_status_test.go`:

```go
package indexing

import (
    "context"
    "strings"
    "testing"

    "memento-mcp/internal/embedding"
)

func TestSemanticStatusAutoUnavailableIsNotAnError(t *testing.T) {
    root, store := backfillFixture(t)
    inner := &flakyEmbedder{release: make(chan struct{})} // always fails
    runtime := embedding.NewRuntime(inner, embedding.ModeAuto)

    idx, err := New(Config{RootAbs: root, StoreDir: store, Embedder: runtime})
    if err != nil {
        t.Fatal(err)
    }
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    idx.Start(ctx)
    if err := idx.IndexAll(ctx); err != nil {
        t.Fatal(err)
    }

    status := idx.Status()
    if status.Error != "" {
        t.Fatalf("auto mode set status.Error = %q, want empty", status.Error)
    }
    if status.Semantic == nil {
        t.Fatal("expected a semantic block")
    }
    if status.Semantic.Mode != "auto" || status.Semantic.State != "lexical" {
        t.Fatalf("semantic = %+v, want mode auto state lexical", status.Semantic)
    }
    if status.Semantic.Available {
        t.Fatal("semantic.available must be false")
    }
    if status.Semantic.Reason == "" {
        t.Fatal("semantic.reason must explain the fallback")
    }
    if status.Semantic.VectorsPending != 1 {
        t.Fatalf("vectorsPending = %d, want 1", status.Semantic.VectorsPending)
    }
    if !strings.Contains(status.Semantic.Hint, "Semantic retrieval") {
        t.Fatalf("hint = %q, want an actionable sentence", status.Semantic.Hint)
    }
}

func TestSemanticStatusRequiredUnavailableIsAnError(t *testing.T) {
    root, store := backfillFixture(t)
    inner := &flakyEmbedder{release: make(chan struct{})}
    runtime := embedding.NewRuntime(inner, embedding.ModeRequired)

    idx, err := New(Config{RootAbs: root, StoreDir: store, Embedder: runtime})
    if err != nil {
        t.Fatal(err)
    }
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    idx.Start(ctx)
    if err := idx.IndexAll(ctx); err != nil {
        t.Fatal(err)
    }

    status := idx.Status()
    if status.Error == "" {
        t.Fatal("required mode must report an error when the runtime is unavailable")
    }
    if status.Semantic.Mode != "required" {
        t.Fatalf("mode = %q, want required", status.Semantic.Mode)
    }
}

func TestSemanticStatusHybridWhenAvailable(t *testing.T) {
    root, store := backfillFixture(t)
    inner := &flakyEmbedder{release: make(chan struct{})}
    close(inner.release)
    runtime := embedding.NewRuntime(inner, embedding.ModeAuto)

    idx, err := New(Config{RootAbs: root, StoreDir: store, Embedder: runtime})
    if err != nil {
        t.Fatal(err)
    }
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    idx.Start(ctx)
    if err := idx.IndexAll(ctx); err != nil {
        t.Fatal(err)
    }

    status := idx.Status()
    if status.Semantic.State != "hybrid" || !status.Semantic.Available {
        t.Fatalf("semantic = %+v, want hybrid and available", status.Semantic)
    }
    if status.Semantic.Hint != "" {
        t.Fatalf("hint = %q, want empty while hybrid", status.Semantic.Hint)
    }
    if status.Semantic.VectorsPending != 0 {
        t.Fatalf("vectorsPending = %d, want 0", status.Semantic.VectorsPending)
    }
}

func TestSemanticStatusAbsentWhenNoEmbedder(t *testing.T) {
    root, store := backfillFixture(t)
    idx, err := New(Config{RootAbs: root, StoreDir: store})
    if err != nil {
        t.Fatal(err)
    }
    if idx.Status().Semantic != nil {
        t.Fatal("no embedder configured must not produce a semantic block")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indexing/ -run TestSemanticStatus -v`

Expected: FAIL to compile — `status.Semantic undefined`.

- [ ] **Step 3: Create the semantic status file**

Create `internal/indexing/semantic_status.go`:

```go
package indexing

import (
    "fmt"
    "strings"

    "memento-mcp/internal/embedding"
)

// SemanticStatus reports how retrieval is currently operating. Every field is
// derived on read from the manifest and the runtime, so it cannot drift from
// what search actually does.
type SemanticStatus struct {
    Mode           string `json:"mode"`
    State          string `json:"state"`
    Provider       string `json:"provider,omitempty"`
    Model          string `json:"model,omitempty"`
    Available      bool   `json:"available"`
    Reason         string `json:"reason,omitempty"`
    VectorsPending int    `json:"vectorsPending"`
    Hint           string `json:"hint,omitempty"`
}

// semanticReporter is the part of embedding.Runtime the indexer needs. Taking
// an interface keeps indexing free of a hard dependency on the concrete type.
type semanticReporter interface {
    Mode() embedding.Mode
    Availability() embedding.Availability
    Name() string
}

// semanticStatus builds the status block, or nil when semantic retrieval is
// not configured at all.
func (i *Indexer) semanticStatus() *SemanticStatus {
    reporter, ok := i.embedder.(semanticReporter)
    if !ok || !reporter.Mode().Enabled() {
        return nil
    }
    availability := reporter.Availability()
    provider, model := splitEmbedderName(reporter.Name())

    status := &SemanticStatus{
        Mode:           string(reporter.Mode()),
        State:          "lexical",
        Provider:       provider,
        Model:          model,
        Available:      availability.Available,
        Reason:         availability.Reason,
        VectorsPending: i.pendingVectorCount(),
    }
    if availability.Available {
        status.State = "hybrid"
        return status
    }
    if status.Reason == "" {
        status.Reason = "embedding runtime has not been reached yet"
    }
    status.Hint = fmt.Sprintf(
        "Semantic retrieval is off (%s). Start a local Ollama and run 'ollama pull %s' to enable it.",
        status.Reason, model,
    )
    return status
}

// splitEmbedderName turns "ollama/nomic-embed-text:v1.5" into its provider and
// model halves.
func splitEmbedderName(name string) (provider, model string) {
    provider, model, found := strings.Cut(name, "/")
    if !found {
        return "", name
    }
    return provider, model
}
```

- [ ] **Step 4: Add the field and populate it**

In `internal/indexing/indexer.go`, add to the `Status` struct:

```go
type Status struct {
    Ready         bool            `json:"ready"`
    LastIndexedAt string          `json:"lastIndexedAt,omitempty"`
    FilesIndexed  int             `json:"filesIndexed"`
    BytesIndexed  int64           `json:"bytesIndexed"`
    Partial       bool            `json:"partial"`
    Error         string          `json:"error,omitempty"`
    Semantic      *SemanticStatus `json:"semantic,omitempty"`
}
```

Then change `Status()` to derive the block. `semanticStatus` and `pendingVectorCount` take the mutex themselves, so build it before taking the lock:

```go
func (i *Indexer) Status() Status {
    semantic := i.semanticStatus()
    i.mu.Lock()
    defer i.mu.Unlock()
    status := i.status
    status.Semantic = semantic
    if semantic != nil && semantic.Mode == string(embedding.ModeRequired) && !semantic.Available && status.Error == "" {
        status.Error = semantic.Reason
    }
    return status
}
```

Confirm `internal/indexing/indexer.go` already imports `memento-mcp/internal/embedding`; it does.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/indexing/ -run TestSemanticStatus -v`

Expected: PASS, all four tests.

- [ ] **Step 6: Run the full suite**

Run: `gofmt -l internal/ cmd/ && go vet ./... && go test ./...`

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/indexing/semantic_status.go internal/indexing/semantic_status_test.go internal/indexing/indexer.go
git commit -m "feat: report semantic mode and availability in index status"
```

---

### Task 7: Report semantic state in doctor

`doctor` is where a human goes when something looks wrong, so it carries the actionable guidance.

**Files:**

- Create: `internal/app/doctor_semantic.go`
- Create: `internal/app/doctor_semantic_test.go`
- Modify: `internal/app/doctor.go` (`doctorClients`, after the binary check around line 60)

**Interfaces:**

- Consumes: `embedding.FromEnv`, `embedding.Runtime.Probe`, `embedding.Mode` from Tasks 3–5.
- Produces: `func doctorSemantic(ctx context.Context, stdout io.Writer) int` returning the failure count to add.

- [ ] **Step 1: Write the failing test**

Create `internal/app/doctor_semantic_test.go`:

```go
package app

import (
    "bytes"
    "context"
    "strings"
    "testing"
)

func TestDoctorSemanticOffReportsSkip(t *testing.T) {
    t.Setenv("MEMENTO_SEMANTIC_ENABLED", "false")
    var out bytes.Buffer
    failures := doctorSemantic(context.Background(), &out)
    if failures != 0 {
        t.Fatalf("failures = %d, want 0", failures)
    }
    if !strings.Contains(out.String(), "semantic: disabled") {
        t.Fatalf("output = %q, want a disabled line", out.String())
    }
}

func TestDoctorSemanticAutoUnreachableWarnsWithoutFailing(t *testing.T) {
    t.Setenv("MEMENTO_SEMANTIC_ENABLED", "auto")
    // Port 1 is reserved and refuses connections.
    t.Setenv("MEMENTO_OLLAMA_URL", "http://127.0.0.1:1")
    var out bytes.Buffer
    failures := doctorSemantic(context.Background(), &out)
    if failures != 0 {
        t.Fatalf("auto mode failures = %d, want 0", failures)
    }
    text := out.String()
    if !strings.Contains(text, "[WARN]") {
        t.Fatalf("output = %q, want a WARN line", text)
    }
    if !strings.Contains(text, "ollama pull") {
        t.Fatalf("output = %q, want remediation guidance", text)
    }
}

func TestDoctorSemanticRequiredUnreachableFails(t *testing.T) {
    t.Setenv("MEMENTO_SEMANTIC_ENABLED", "true")
    t.Setenv("MEMENTO_OLLAMA_URL", "http://127.0.0.1:1")
    var out bytes.Buffer
    failures := doctorSemantic(context.Background(), &out)
    if failures != 1 {
        t.Fatalf("required mode failures = %d, want 1", failures)
    }
    if !strings.Contains(out.String(), "[FAIL]") {
        t.Fatalf("output = %q, want a FAIL line", out.String())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestDoctorSemantic -v`

Expected: FAIL to compile — `undefined: doctorSemantic`.

- [ ] **Step 3: Implement the section**

Create `internal/app/doctor_semantic.go`:

```go
package app

import (
    "context"
    "fmt"
    "io"
    "time"

    "memento-mcp/internal/embedding"
)

// doctorSemantic reports how semantic retrieval is configured and whether the
// runtime answers. It returns the number of failures to add to doctor's total:
// an unreachable runtime only fails in required mode, because falling back to
// lexical is a healthy state in auto.
func doctorSemantic(ctx context.Context, stdout io.Writer) int {
    config, err := embedding.FromEnv()
    if err != nil {
        fmt.Fprintf(stdout, "[FAIL] semantic: %v\n", err)
        return 1
    }
    if !config.Mode.Enabled() {
        fmt.Fprintln(stdout, "[PASS] semantic: disabled (MEMENTO_SEMANTIC_ENABLED=false)")
        return 0
    }

    runtime, ok := config.Embedder.(*embedding.Runtime)
    if !ok {
        fmt.Fprintf(stdout, "[FAIL] semantic: embedder is %T, expected a runtime\n", config.Embedder)
        return 1
    }

    probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()
    availability := runtime.Probe(probeCtx)

    if availability.Available {
        fmt.Fprintf(stdout, "[PASS] semantic: %s reachable (mode %s)\n", runtime.Name(), config.Mode)
        return 0
    }

    label := "WARN"
    failures := 0
    if config.Mode == embedding.ModeRequired {
        label = "FAIL"
        failures = 1
    }
    fmt.Fprintf(stdout, "[%s] semantic: %s (mode %s)\n", label, availability.Reason, config.Mode)
    _, model := splitRuntimeModel(runtime.Name())
    fmt.Fprintf(stdout, "       install a local runtime, then run: ollama pull %s\n", model)
    if config.Mode == embedding.ModeAuto {
        fmt.Fprintln(stdout, "       retrieval continues to work using lexical ranking")
    }
    return failures
}

// splitRuntimeModel mirrors the provider/model split used in index status.
func splitRuntimeModel(name string) (provider, model string) {
    for index := 0; index < len(name); index++ {
        if name[index] == '/' {
            return name[:index], name[index+1:]
        }
    }
    return "", name
}
```

- [ ] **Step 4: Call it from doctor**

In `internal/app/doctor.go`, inside `doctorClients`, immediately after the binary check block closes and before `selected := clients`:

```go
    failures += doctorSemantic(context.Background(), stdout)
```

Add the `context` import if it is not already present.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/app/ -run TestDoctorSemantic -v`

Expected: PASS, all three tests.

- [ ] **Step 6: Run the full suite**

Run: `gofmt -l internal/ cmd/ && go vet ./... && go test ./...`

Expected: all pass. Existing doctor output tests may need their expected line counts updated.

- [ ] **Step 7: Commit**

```bash
git add internal/app/doctor_semantic.go internal/app/doctor_semantic_test.go internal/app/doctor.go
git commit -m "feat: report semantic runtime state in doctor"
```

---

### Task 8: Flip the default to auto

Everything before this task is inert for existing users. This is the behavior change.

**Files:**

- Modify: `internal/embedding/mode.go` (`DefaultMode`)
- Modify: `internal/app/cli.go` (`defaultMCPEnv`)
- Modify: `docs/adr/ADRs.md`
- Modify: `docs/README.md`
- Test: `internal/embedding/mode_test.go`, `internal/app/cli_test.go`

**Interfaces:**

- Consumes: everything from Tasks 1–7.
- Produces: no new API.

- [ ] **Step 1: Write the failing test**

Append to `internal/embedding/mode_test.go`:

```go
func TestDefaultModeIsAuto(t *testing.T) {
    if DefaultMode != ModeAuto {
        t.Fatalf("DefaultMode = %q, want auto", DefaultMode)
    }
    mode, err := ParseMode("")
    if err != nil {
        t.Fatal(err)
    }
    if mode != ModeAuto {
        t.Fatalf("unset mode = %q, want auto", mode)
    }
}
```

Append to `internal/app/cli_test.go`, inside the existing default-env test or as a new test following its style:

```go
func TestDefaultMCPEnvEnablesSemanticAuto(t *testing.T) {
    if defaultMCPEnv["MEMENTO_SEMANTIC_ENABLED"] != "auto" {
        t.Fatalf("expected MEMENTO_SEMANTIC_ENABLED=auto, got %#v", defaultMCPEnv)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/embedding/ ./internal/app/ -run 'TestDefaultModeIsAuto|TestDefaultMCPEnvEnablesSemanticAuto' -v`

Expected: FAIL — `DefaultMode = "off"` and the env key is missing.

- [ ] **Step 3: Flip the default**

In `internal/embedding/mode.go`:

```go
// DefaultMode is the mode used when MEMENTO_SEMANTIC_ENABLED is unset.
// Semantic retrieval is attempted by default and degrades to lexical when no
// runtime is reachable.
var DefaultMode = ModeAuto
```

- [ ] **Step 4: Write the default into generated configs**

In `internal/app/cli.go`, add to `defaultMCPEnv`:

```go
    "MEMENTO_SEMANTIC_ENABLED":           "auto",
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/embedding/ ./internal/app/ -v 2>&1 | tail -20`

Expected: PASS. Tests that assumed semantic was off by default may now construct a runtime; fix any that break by setting `MEMENTO_SEMANTIC_ENABLED=false` explicitly in the test.

- [ ] **Step 6: Record the ADR**

In `docs/adr/ADRs.md`, add an entry to the index list at the top, mark ADR 0006's status line as `Accepted (superseded in part by ADR 00NN)` using the next free number, and append a new ADR at the end of the file following the existing section format, with these decisions:

- Default becomes `auto`. Lexical fallback is a healthy reported state, not an error.
- Availability is separated from configuration. The embedding fingerprint records what vectors were made with and never reflects reachability.
- Vector sidecars survive runtime outages and an off toggle. Only a genuine identity change resets them.
- `MEMENTO_SEMANTIC_ENABLED` is tri-state; `true` now means "required".
- Unchanged from ADR 0006: Memento still does not install Ollama or pull models. Consented provisioning is deferred to a later ADR.

- [ ] **Step 7: Update the documentation**

Four references exist. Update each:

- `docs/README.md:82` — currently ``- `MEMENTO_SEMANTIC_ENABLED` (default `false`)``. Replace with a description of the three values and the new default:

```markdown
- `MEMENTO_SEMANTIC_ENABLED` (default `auto`) — `auto` uses semantic retrieval
  when a local embedding runtime is reachable and falls back to lexical
  ranking otherwise, which is a healthy state and is not reported as an
  error. `true` requires it: an unreachable runtime is reported as an error
  and fails `doctor`, though retrieval still degrades to lexical. `false`
  disables semantic retrieval entirely.
```

- `docs/README.md:75` — the `MEMENTO_SEMANTIC_ENABLED=true ./bin/memento-mcp` example no longer demonstrates enabling, since auto is the default. Change the surrounding text to show `MEMENTO_SEMANTIC_ENABLED=false` as the opt-out, or drop the variable from the example.
- `docs/README.md:186` — `MEMENTO_SEMANTIC_ENABLED=true make retrieval-eval` still reads correctly, because the eval wants semantic asserted rather than auto-detected. Leave it, and confirm the surrounding sentence does not describe it as "enabling an off-by-default feature".
- `docs/clients.md:227` — the sample client config sets `"MEMENTO_SEMANTIC_ENABLED": "true"`. Change it to `"auto"` so generated and documented configs agree with `defaultMCPEnv`.

- [ ] **Step 8: Verify the end-to-end behavior manually**

```bash
go build -o /tmp/memento-check ./cmd/server
```

Then, in a scratch git repository with no Ollama running, start the server over stdio, call `repo_index_status`, and confirm: `error` is absent, `semantic.state` is `lexical`, `semantic.reason` is populated, and `filesIndexed` is non-zero. Run `/tmp/memento-check doctor` and confirm a `[WARN]` semantic line with remediation, and exit status 0.

- [ ] **Step 9: Run the full suite and the retrieval fixtures**

Run: `gofmt -l internal/ cmd/ && go vet ./... && go test ./...`

Then run the retrieval evaluation with no runtime reachable and confirm ranking matches the stored lexical baseline in `evaluation/baselines/retrieval-ci-v1.json`. The fallback must be a no-op, not a different algorithm. See `docs/evaluation.md` for the runner invocation.

- [ ] **Step 10: Commit**

```bash
git add internal/embedding/mode.go internal/app/cli.go internal/embedding/mode_test.go internal/app/cli_test.go docs/
git commit -m "feat: default semantic retrieval to auto-detect"
```

---

## Self-Review Notes

- **Spec coverage.** Section 1 → Tasks 3 and 8. Section 2 → Task 4. Section 3 → Task 4 (probe, classification, backoff) and Task 5 (demand-driven, because a real `Embed` doubles as the probe). Section 4 → Task 1. Section 5 → Task 2. Section 6 → Task 6. Section 10 → Task 7. ADR impact → Task 8.
- **Deviation from the spec, deliberate.** Spec section 4 says `needsVectors` should force re-indexing only when the runtime is available. The plan removes the vector gap from the skip condition entirely and repairs gaps through backfill instead. Same outcome, one fewer concept, and it makes Task 1 independent of the decorator, so phase 1 can land before phase 2 exactly as the spec's phasing intends.
- **Not covered here, by design.** Spec sections 7, 8, and 9 (llama.cpp provider, provisioning, sticky provider selection) belong to phases 5–6 and their own plan. Sticky identity is not needed yet because there is only one provider; the Task 2 fingerprint fix is the foundation it will build on.
