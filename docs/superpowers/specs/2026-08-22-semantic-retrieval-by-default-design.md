# Semantic retrieval by default — design

- Date: 2026-08-22
- Status: Draft

## Problem

Semantic retrieval is gated behind `MEMENTO_SEMANTIC_ENABLED`, default `false` (`internal/embedding/config.go`). Most installs therefore run lexical-only retrieval and never discover the hybrid path. ADR 0006 chose that default deliberately, to keep startup model-free and avoid owning model lifecycle.

Flipping the default is not a one-line change. Three defects are latent today and only trigger once an embedder is configured on machines without a runtime:

1. **Permanent re-index churn.** `indexOne` computes `needsVectors = ent.Vectors != ent.Chunks` (`internal/indexing/indexer.go`). A configured but unreachable embedder never writes vectors, so the condition never clears. Every file is re-read, re-hashed, and re-chunked on every poll cycle, indefinitely.
2. **Vector wipe on fingerprint flip.** `embeddingFingerprint()` returns `"disabled"` when the embedder is nil. Probing and nilling an absent runtime therefore makes `New()` call `resetVectorFiles()` and delete every sidecar, then re-embed the whole repository when the runtime returns.
3. **False error reporting.** A missing runtime writes `status.Error`, so `repo_index_status` reports a healthy lexical install as faulty.

`NewOllama` never opens a socket. It builds an HTTP client, so availability is unknowable until an embed fails.

Separately, enabling semantic retrieval today requires the user to install Ollama (~1 GB class install) and pull a 274 MB model by hand. Nothing in the product offers to do it for them.

## Scope

In scope:

- Default semantic retrieval to auto-detect, with lexical fallback as a healthy reported mode.
- Separate "configured" from "available" and fix the three defects above.
- A second provider: llama.cpp `llama-server` over loopback.
- Consented provisioning of that runtime and a GGUF model, offered through both a CLI verb and an MCP tool.

Out of scope:

- Changing the embedding model family. The provisioned model is the quantized form of the current default.
- Replacing Ollama. It remains a first-class, preferred provider when already running.
- Any unconsented network fetch.

## Decision

### 1. Configuration contract

`MEMENTO_SEMANTIC_ENABLED` becomes tri-state. `RuntimeConfig.Enabled bool` is replaced by `RuntimeConfig.Mode`.

| Value | Mode | Behavior |
| --- | --- | --- |
| unset, `auto` | `ModeAuto` (new default) | Construct embedder, probe on demand. Hybrid when available, lexical otherwise. Fallback is healthy. |
| `true`, `1`, `t` | `ModeRequired` | As auto, but unavailability sets `status.Error` and fails `doctor`. Still serves lexical. |
| `false`, `0`, `f` | `ModeOff` | No embedder, no probe, pure lexical. Current default behavior. |

- `ParseMode` accepts the legacy boolean spellings `strconv.ParseBool` already handles, plus `auto`.
- Unrecognized values remain a fatal startup error. `embedding.FromEnv` errors propagate through `NewServer` (`internal/mcp/server.go`). A config typo must not silently downgrade retrieval.
- `ModeRequired` degrades rather than refusing to boot. ADR 0006's guarantee that the server stays useful without a runtime is preserved; the failure is loud, not fatal.
- `runSetup` writes `MEMENTO_SEMANTIC_ENABLED=auto` into generated client configs, matching how the other defaults in `defaultMCPEnv` are written.

### 2. `embedding.Runtime` decorator

New type in `internal/embedding`, wrapping a concrete `Embedder`:

```go
type Availability struct {
    Available bool
    Reason    string
    CheckedAt time.Time
}

type Runtime struct { /* wrapped Embedder, mode, probe policy, availability */ }

func (r *Runtime) Embed(ctx context.Context, task Task, inputs []string) ([][]float32, error)
func (r *Runtime) Availability() Availability
func (r *Runtime) Mode() Mode
func (r *Runtime) Fingerprint() string // delegates to the wrapped embedder
func (r *Runtime) Name() string
```

- **Configured** is a property of the wrapped embedder. **Available** is state on the wrapper. Nothing about availability reaches the fingerprint.
- `Runtime` implements `Embedder`, so `indexing.Config.Embedder` accepts it unchanged.
- Probe policy, backoff, and reason classification live here, not in `internal/indexing`. `indexer.go` is 42 KB and already owns chunking, search, manifest, vectors, trigrams, and lifecycle. Provider and probe logic must not be added to it.

### 3. Availability probing

- The probe is a single-input `Embed` of a fixed sentinel string. A reachability check would not detect a running Ollama whose model was never pulled, which is the expected common failure. Probe results are never persisted as vectors.
- Probing is demand-driven. A probe fires only when embedding work is pending: files changed by the git or fs change detectors (`internal/mcp/server.go`), or a non-empty vector backlog. An idle repository with no backlog performs no probes.
- At most one probe per backoff window. The existing 30 s embedding backoff (`recordEmbeddingFailure`) is reused as that window; no new goroutine or ticker.
- A runtime that appears mid-session is picked up on the next pending-work probe, and the backlog is backfilled per section 4.
- Failures are classified into `Availability.Reason`, because reason text drives the nudge:
  - connection refused → `no embedding runtime detected at <endpoint>`
  - model 404 → `model <name> is not available in <provider>`
  - timeout → `runtime did not respond within <timeout>`
  - other → the underlying error string

### 4. Index churn and vector backfill

- `needsVectors` only forces re-indexing when the runtime is currently available. Content freshness (`ent.Size == info.Size() && ent.ModTime == mod`) remains the sole skip condition otherwise. An unavailable runtime no longer causes repeated re-reads.
- New `backfillVectors` pass. When availability returns and manifest entries have `Vectors != Chunks`, chunks are read from the persisted JSONL sidecars and embedded directly. No source re-read, no re-chunk, no re-hash.
- The pending set is derived from the manifest, so computing it costs no I/O.
- This is an improvement for existing opt-in users. Today a single failed embed makes that file re-read and re-chunk on every pass until it succeeds.

### 5. Vector preservation

- `embeddingFingerprint()` no longer collapses to `"disabled"` based on runtime state.
- In `ModeOff`, the vector-reset comparison in `New()` is skipped entirely and the stored fingerprint is preserved. Turning semantic off no longer discards sidecars, so turning it back on does not force a full re-embed.
- An unreachable provider never resets sidecars. Only a change of embedding identity does, and identity is sticky per section 9.
- A genuine model or embedding-input change still resets sidecars, as ADR 0006 requires.

### 6. Status and the nudge

`indexing.Status` gains a derived `semantic` block. Every field is computed from the manifest and the runtime, never cached, consistent with `filesIndexed`.

```json
"semantic": {
  "mode": "auto",
  "state": "lexical",
  "provider": "ollama",
  "model": "nomic-embed-text:v1.5",
  "available": false,
  "reason": "no embedding runtime detected at 127.0.0.1:11434",
  "vectorsPending": 412,
  "hint": "Semantic retrieval is off: no embedding runtime detected. Run 'memento-mcp install-semantic' or ask your agent to install it."
}
```

- `status.Error` stays reserved for genuine faults. In `ModeAuto`, a missing runtime is `state: "lexical"` plus a `reason`, not an error. In `ModeRequired` it is an error.
- `hint` is a single sentence, present only while `state` is `lexical` and mode is not off. It names the install path.
- `vectorsPending` is a count of **files** whose manifest entry has `Vectors != Chunks`. It distinguishes "warming up" from "broken".
- `DebugInfo` reports the same availability data alongside its existing fields.
- No plumbing is needed to surface this: `withWorkspaceResultContext` marshals tool results generically (`internal/mcp/server.go`).

### 7. llama.cpp provider

New `embedding.LlamaCPP`, an HTTP client for a supervised `llama-server`.

- Same shape as the Ollama client: loopback HTTP, loopback-only URL validation, no redirect following.
- `llama-server` is started with embedding mode enabled on a loopback port chosen by Memento.
- Endpoint and request format to be confirmed against the pinned release during implementation; the OpenAI-compatible `/v1/embeddings` path is the expected target.
- Fingerprint covers provider, embedding input version, model identity, and quantization.

Process supervision:

- Spawned on first use, not at startup.
- Health-checked before the first embed is trusted.
- Shut down after an idle timeout. The model loads once per active window, so bulk indexing and per-query embedding both stay fast.
- Crash detection marks the runtime unavailable with a reason and respects the normal backoff before respawn.
- The child process is terminated when the server's background context is cancelled.

A one-shot `llama-embedding` CLI was rejected: it reloads the model on every invocation, so every `repo_search` would pay a model load.

### 8. Provisioning

Artifacts are shared across repositories, under the existing `~/.memento-mcp/` convention:

- `~/.memento-mcp/runtime/llama.cpp/<version>/`
- `~/.memento-mcp/models/<model-id>/`

Model: `nomic-embed-text-v1.5`, GGUF `Q4_K_M`. Approximately 84 MB, 8192 context, 768 dimensions. It is the quantized form of the current default, so retrieval behavior stays continuous. Exact size and asset URL are verified during implementation. Chunks are capped at 200 lines / 8 KiB (`internal/indexing/chunk.go`), roughly 2–2.6k tokens, so any candidate model needs at least 2048 context. That rules out `all-MiniLM-L6-v2` (256) and `bge-small-en-v1.5` (512).

Quantized vectors differ from f16, so this is a new fingerprint and one re-embed for users switching to it.

Trust requirements, all mandatory:

- llama.cpp release version and model revision are pinned in code. Nothing is resolved dynamically at runtime.
- SHA-256 of every downloaded artifact is pinned in code and verified before the binary is executed or the model is loaded.
- HTTPS only. Cross-host redirects are rejected.
- Install is atomic: download to a temp directory, verify, then rename into place.
- On macOS the quarantine xattr is cleared after verification, otherwise exec fails.
- A failed verification deletes the artifact and reports the failure. An unverified binary is never executed.

Consent, required in both paths, with no auto-download anywhere:

- **CLI**: `memento-mcp install-semantic`. Prints provider, versions, sources, and total download size, then prompts for explicit confirmation. `--yes` for non-interactive use.
- **MCP tool**: `repo_semantic_install`, with mutating annotations. Lets the agent offer the install in-session and perform it once the user agrees. Returns the same summary the CLI prints.
- The `hint` in section 6 names both paths.

### 9. Provider selection in auto mode

Selection is lazy and sticky.

**Lazy.** No provider is probed at startup. Selection resolves on the first demand-driven probe, so an idle repository still performs no network activity. `Runtime` holds an ordered candidate list and resolves it on first use.

Candidate order:

1. `MEMENTO_EMBEDDING_PROVIDER`, if set. Pins the choice and skips detection.
2. The identity already recorded in the manifest, if its provider is reachable. See sticky, below.
3. An already-running Ollama with the model present. It costs nothing and respects the user's existing setup.
4. A provisioned llama.cpp runtime, if installed.
5. Lexical, with the install offer.

**Sticky.** Embedding identity is per index, and a provider switch is not a free operation. Ollama's f16 `nomic-embed-text:v1.5` and the provisioned Q4_K_M GGUF produce different vectors, so they carry different fingerprints. Without stickiness, a user whose Ollama is running on Monday and stopped on Tuesday would flip identity, reset every sidecar, and re-embed the repository — the same defect as landmine 2, arriving through provider selection instead of nil-embedder.

Therefore:

- Once an index holds vectors under identity X, X is preferred for that index for as long as its provider is reachable.
- If X's provider is unreachable, the runtime reports unavailable and serves lexical. It does **not** silently switch to another provider and invalidate the vectors.
- Switching identity happens only when the index holds no vectors, or when the user acts explicitly: setting `MEMENTO_EMBEDDING_PROVIDER`, changing the model, or running the install command. Those paths re-embed knowingly.
- `Availability.Reason` names the pinned identity when it is unreachable, so the cause is legible rather than appearing as a generic outage.

### 10. Doctor and setup

`memento-mcp doctor` gains a semantic section reporting mode, selected provider, reachability, model presence, `vectorsPending`, and the exact remediation command when unavailable. It fails in `ModeRequired` when no runtime is reachable.

## Data flow

Startup: `FromEnv` parses mode → builds the ordered candidate list per section 9 → wraps it in `Runtime` → passed as `indexing.Config.Embedder`. No provider is resolved and no probe runs yet.

Index pass: change detector fires → indexer collects candidates → if embedding work is pending, `Runtime` probes when the backoff window allows → available: embed and write sidecars; unavailable: chunk and store without vectors, leaving them pending.

Availability returns: next pending-work probe succeeds → `backfillVectors` embeds pending chunks from the JSONL sidecars → `vectorsPending` drains to zero.

Query: `repo_search` asks `Runtime` for a query vector. Available: hybrid ranking. Unavailable: `queryVector` stays nil, `semanticUsed` is false, and lexical ordering is returned unchanged.

## Error handling

- Every failure path degrades to lexical and keeps serving. No embedding failure fails a tool call.
- Failures set `Availability.Reason` and start the backoff window. Only `ModeRequired` also sets `status.Error`.
- Provisioning failures (network, checksum, exec, quarantine) are reported to the caller and leave no partial install.
- `llama-server` crashes mark the runtime unavailable and respawn no faster than the backoff window.

## Security

- Loopback-only endpoints for both providers, with redirect following disabled.
- Pinned versions and pinned checksums. No dynamic resolution of download targets.
- No binary executes before its checksum verifies.
- Redaction is unchanged: chunks are redacted before embedding, and provisioning sends no repository content anywhere.
- Downloading a model is a network fetch of a public artifact, not source egress. The local-first guarantee on source code is unaffected.

## Testing

Unit, `internal/embedding`:

- `ParseMode` across auto, legacy booleans, and invalid values.
- Probe classification for refused, 404, timeout, and success.
- Backoff windows: at most one probe per window; no probe without pending work.
- Fingerprint stability across availability transitions.
- llama.cpp client request/response handling against an `httptest` server.
- Supervisor: spawn, health check, idle shutdown, crash-marks-unavailable.

Unit, `internal/indexing`:

- No re-read churn while unavailable. Assert read/chunk counts do not grow across repeated passes.
- `backfillVectors` fills vectors without re-chunking or re-hashing.
- Vectors survive an outage across a process restart.
- Vectors survive an off → on toggle.
- Vectors survive an unreachable pinned provider. An index embedded under Ollama, reopened with Ollama down and a provisioned llama.cpp present, serves lexical and keeps its sidecars. It must not switch identity and re-embed.

Unit, `internal/mcp`:

- `ModeAuto` + unavailable → no `status.Error`, `state: lexical`, `hint` present.
- `ModeRequired` + unavailable → `status.Error` set.
- `vectorsPending` reflects the manifest.
- `repo_semantic_install` requires confirmation and refuses to fetch without it.

Provisioning tests use a local test server and fixture artifacts. No test performs a real network download.

Evaluation:

- Run the retrieval fixtures with the runtime unavailable and assert ranking is identical to the current lexical baseline (`evaluation/baselines/retrieval-ci-v1.json`). The fallback must be a no-op, not a different algorithm.
- Run the fixtures against the provisioned Q4_K_M model and record the result, satisfying ADR 0006's requirement that a default change be justified by fixtures.

## Implementation phases

The work is sequenced so each phase is independently shippable and testable.

1. **Safety fixes.** Sections 4 and 5: churn fix, backfill, vector preservation. No behavior change for existing users, and it removes the defects that make any default flip unsafe. Landable on its own.
2. **Runtime decorator and probing.** Sections 2 and 3, plus the tri-state config in section 1, with the default still `off`. Introduces the abstraction without changing anyone's mode.
3. **Status and doctor.** Sections 6 and 10. Makes availability observable before it becomes load-bearing.
4. **Default flip.** Change the default to `auto`. Small diff, meaningful only after phases 1–3.
5. **llama.cpp provider and supervision.** Section 7.
6. **Provisioning.** Sections 8 and 9, the CLI verb, and the MCP tool.

Phases 1–4 deliver semantic-by-default on the existing Ollama provider. Phases 5–6 add the provisioned runtime.

## ADR impact

A new ADR supersedes ADR 0006 in part, and ADR 0006 is marked accordingly. It records:

- Default becomes auto-detect; lexical fallback is a healthy state.
- Availability is separated from configuration; vectors survive outages and toggles.
- **Reversal**: "Memento does not install Ollama or pull models" no longer holds. Memento provisions a llama.cpp runtime and a GGUF model on explicit consent, and accepts model lifecycle ownership for that path. Ollama is still never installed by Memento.
- CGO_ENABLED=0 is preserved. llama.cpp runs as an external process, never linked in. ADR 0013's constraint is unaffected.

## Compatibility and rollout

- Existing configs setting `true` or `false` keep working. `true` gains the stricter "required" meaning, which is a superset of "on".
- Users who never set the variable and happen to run Ollama will start receiving hybrid ranking. This is the intended change and must be called out in release notes.
- No index rebuild is required by this change. Users who later adopt the provisioned Q4_K_M model re-embed once, driven by the fingerprint change.
