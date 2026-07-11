# Opt-in local aggregate feedback

Memento feedback is disabled by default. It has no analytics SDK, remote endpoint, or upload path. Setting `MEMENTO_FEEDBACK_ENABLED=true` opts this local process into recording a deliberately small aggregate event schema:

```bash
MEMENTO_FEEDBACK_ENABLED=true ./bin/memento-mcp
```

Generated MCP configurations explicitly set `MEMENTO_FEEDBACK_ENABLED=false`. Enabling feedback adds the `feedback_submit` tool and records coarse operational buckets after tool calls. Disabling feedback stops new events but does not silently delete existing local data; use the explicit deletion command below.

## Exact collection contract

Each version 1 event contains only:

- a tool category: `repository`, `memory`, `index`, `workspace`, or `unavailable`;
- a duration bucket: under 100 ms, 100 ms–1 s, 1–10 s, 10 s or more, or unavailable;
- a result-size bucket: empty, under 4 KiB, 4–32 KiB, 32 KiB or more, or unavailable;
- a failure class: none, tool error, canceled, timeout, or unavailable;
- two boolean feature flags: semantic retrieval enabled and redaction enabled; and
- an optional explicit rating: `helpful`, `not_helpful`, or `unsure`.

The event type has no free-text, metadata-extension, timestamp, task ID, session ID, repository ID, or correlation field. It cannot represent source code, prompts, queries, absolute or relative paths, note or memory content, raw arguments/results, model names, usernames, or identifiers/hashes derived from any of those values. Tool names are reduced to the category before serialization. Result content is measured for a size bucket and then discarded. The machine-readable contract is `docs/schemas/feedback-event-v1.schema.json` and rejects additional properties at the event and feature-flag levels.

`feedback_submit` accepts only the fixed choices above and rejects unknown fields. A client should submit at most one explicit rating for a completed session; Memento intentionally stores no session identifier with which to enforce or reconstruct that rule.

## Local storage and controls

Events are appended as JSON Lines to `~/.memento-mcp/feedback/events-v1.jsonl`. The directory and file are created with user-only permissions (`0700` and `0600`). Set `MEMENTO_FEEDBACK_DIR` to choose another local directory. Memento never reads repository content when exporting feedback and never sends feedback over the network.

All controls work offline:

```bash
./bin/memento-mcp feedback status
./bin/memento-mcp feedback export
./bin/memento-mcp feedback export --evaluation
./bin/memento-mcp feedback delete --confirm
```

`status` shows whether collection is enabled, the local file location, the event count, and `network: never`. The default export contains only grouped operational counts and aggregate rating counts—not raw events. The `--evaluation` export emits the strict aggregate feedback supplement accepted by the helpfulness visualizer and regression gate. Deletion is idempotent and requires `--confirm`.

If the local file cannot be created or appended, the original MCP tool result is returned unchanged and a content-free diagnostic is written to the local server log. An explicit `feedback_submit` call reports that its local write failed, but the server and all other tools remain available.

## Helpful-session rate

The export maps `unsure` to the evaluation contract's neutral count:

```text
helpful-session rate = helpful / (helpful + not-helpful + unsure)
```

No rate is emitted until at least one explicit rating exists. The evaluation gate currently treats the 80% target as advisory until the configured minimum of 20 voluntary ratings is reached. Because Memento stores no person or session identity, these counts are aggregate signals rather than unique-user analytics.

## Privacy review checklist

Any event-schema change must answer all of these before merge:

- Is feedback still explicitly opt-in and fully functional without network access?
- Is every new value a bounded enum, boolean, or aggregate count rather than free text?
- Can the field reveal or correlate source, prompts, queries, paths, notes, raw arguments/results, a repository, a person, or a session?
- Are unknown JSON fields rejected in stored events and `feedback_submit` input?
- Do schema tests whitelist every serialized field and verify representative source/path/query values never reach disk?
- Do export tests prove only aggregate counts leave the local event file?
- Do failure-injection tests prove recording cannot change an ordinary MCP tool result?
- Are opt-out, disabling, export, and confirmed deletion still documented and tested?

If any answer is uncertain, do not add the field. A future remote collection service is explicitly outside this contract and requires a separate privacy and product decision.
