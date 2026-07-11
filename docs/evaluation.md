# Helpfulness evaluation

Memento's existing retrieval evaluation answers whether relevant code ranks well. The helpfulness contract adds the question that matters to users: does an agent complete repository work better with Memento than without it?

The contract lives in `evaluation/fixtures/helpfulness.json`. It is versioned and strict: unknown fields and invalid task shapes fail loading through `evaluation.LoadHelpfulnessFixtureFile`.

## Paired experiment

Run every task twice against the same checkout:

1. Baseline: repository tools without Memento.
2. Treatment: the same tools plus Memento.

Keep the prompt, starting state, model, model settings, tokenizer configuration, budgets, and validation rubric identical. Do not compare runs across a model or fixture revision without clearly recording that change.

The local runner consumes the same fixture format. It runs every selected task in baseline then Memento order through a `TaskExecutor`, applies local command/evidence validators to each condition's private workspace, and writes deterministic JSON plus Markdown. A real adapter must prepare isolated but revision-matched workspaces for its two conditions. The included `RecordedExecutor` accepts locally captured agent telemetry so the runner is deterministic in CI and does not need credentials or network access; a client can implement `TaskExecutor` to execute an actual local agent.

## Task fixtures

Each task declares:

- a stable `id` and one of `discovery`, `impact-analysis`, `implementation`, `onboarding`, or `memory-recovery`;
- its checkout and session state in `start`;
- the allowed tools;
- expected code or documentation evidence, plus expected patches/tests when applicable; and
- one or more `command`, `evidence`, or blinded `review` validators.

Use repo-relative paths only. An implementation task must declare its expected tests. A memory-recovery task must start a fresh session and include its explicit, non-secret `memorySeeds`; that makes context-loss recovery reproducible rather than relying on hidden state.

Add a task only when it represents a real workflow and its evidence will remain understandable after the task is executed. Prefer symbol-centered evidence and deterministic command validators. If a source move makes a line-bounded retrieval judgment stale, refresh the judgment before interpreting the metric as a ranking change.

## Scorecard

The fixture declares metrics in four layers:

- Retrieval: precision@k, recall@k, MRR, nDCG, and stale-result rate.
- Agent outcomes: task success, review score, regressions, elapsed time, turns, input tokens, output tokens, total tokens, and unnecessary context reads.
- Durable memory: correct-recovery and code-anchor-grounding rates.
- User value: helpful-session rate and explicit helpful ratings.

Token savings must use model/client usage records. Reports must show input, output, and total counts separately, along with absolute and paired percentage savings. If a client does not provide trustworthy usage data, report `unavailable`; never substitute payload bytes or another tokenizer's estimate and call it actual usage.

Aggregate token savings from paired total-token deltas, not an average of per-task percentages. Keep task-success reporting separate from token availability: a successful run with no usage record still counts for task outcomes but is excluded from the token aggregate.

## Privacy and review

The benchmark is local and fixture-backed. It makes no network calls and reports only fingerprints, outcomes, usage telemetry, and validator statuses: never source code, paths, prompts, queries, notes, memory, command output, or agent responses. Any future user feedback remains opt-in and aggregate-only.

For qualitative tasks, `NewBlindedReviewItem` provides a local, condition-free export shape with an opaque review ID, rubric, and caller-supplied redacted response. The reviewer must not know whether the response came from the baseline or Memento condition. The runner neither writes these exports nor transmits them; the caller is responsible for redaction and any later review channel.

## Run a selected local fixture set

Capture local paired observations using your client adapter, then run this deterministic example fixture set:

```bash
make helpfulness-eval HELPFULNESS_ARGS='-tasks discover-workspace-resolution,onboard-local-validation -runs evaluation/fixtures/helpfulness-runs.example.json -out /tmp/memento-helpfulness-report'
```

The command writes `helpfulness-report.json` and `helpfulness-report.md` under the selected output directory and also prints the Markdown summary. Every observation must provide a task ID, condition, success/failure/invalid/timeout outcome, a matched-run fingerprint (model, prompt, budgets, and starting state), a condition configuration fingerprint, elapsed milliseconds, turns, and aggregate tool/context counters. Input, output, and total token fields are optional; omitted fields remain explicitly `unavailable` in the report.

Invalid and timeout pairs (including a mismatched matched-run fingerprint) are retained in the per-task JSON but excluded from aggregate deltas. Successful and failed matched runs count in outcome deltas. Aggregate paired deltas include a deterministic normal-approximation 95% confidence interval when at least five pairs supply a metric.

## Visualize paired results

Render the report locally as static HTML plus an accessible Markdown table alternative:

```bash
make helpfulness-visualize HELPFULNESS_VISUAL_ARGS='-report /tmp/memento-helpfulness-report/helpfulness-report.json -supplement evaluation/fixtures/helpfulness-visual.example.json -out /tmp/memento-helpfulness-visual'
```

The visual report makes the three regression decisions explicit: task success, paired token efficiency, and retrieval quality. It includes category-grouped paired dumbbell charts for total tokens and elapsed time, task-success Wilson intervals, retrieval small multiples, a committed-baseline trend, and opt-in aggregate-only feedback. Every value remains traceable to the report version and fingerprints; no prompts, paths, source code, raw feedback, notes, or memory are rendered.

The optional supplement is strict versioned JSON for aggregate retrieval snapshots, committed trend points, and aggregate opted-in feedback. It never accepts raw events. When local feedback is enabled, generate a compatible feedback-only supplement without network access:

```bash
./bin/memento-mcp feedback export --evaluation > /tmp/memento-feedback-supplement.json
```

Pass that file through `-supplement` to the visualizer or evaluation gate. A usage record can include its `usageSource` and `tokenizerFingerprint`; otherwise the visual marks those labels `unavailable`. Token savings are calculated from summed paired totals, never by averaging task percentages. Invalid pairs and token-unavailable pairs remain visibly separate from task-success samples and token aggregates.

## Reproduce CI and regression gates

Run the same pinned paired subset, privacy-safe retrieval summary, visual artifacts, and baseline comparison used by CI:

```bash
make evaluation-ci
```

Outputs are written under `/tmp/memento-evaluation-ci` by default; set `EVALUATION_OUT` to choose another local directory. The `Helpfulness Evaluation` workflow uploads the current JSON, Markdown, HTML, gate report, and exact committed baselines for 30 days. Reports contain fingerprints, aggregate metrics, outcomes, and fixture task IDs, but no prompts, queries, repository paths, retrieved chunks, source, notes, memory, or raw feedback.

The gate report distinguishes these outcomes:

- `product-regression`: a measured metric crossed an enforced threshold;
- `infrastructure-failure`: an enforced metric was unavailable or incomparable because a run, fixture, or configuration could not be trusted;
- `advisory-regression`: an initial target was missed but is not mature enough to block CI; and
- `pass`: every enforced comparison passed and no advisory target regressed.

The deterministic lexical retrieval baseline is the first blocking gate: recall may not decrease from the committed, fingerprint-matched baseline. The initial 95% recall@5 floor, no task-success decrease, 20% median token or elapsed-time reduction on successful context-heavy tasks, and 80% aggregate opt-in helpful rating remain advisory until their configured sample requirements are met.

Thresholds and sample requirements live in `evaluation/fixtures/regression-gates.json`; every rule requires a non-blank rationale. Changing a threshold, toggling enforcement, or replacing a committed file under `evaluation/baselines/` must include an explicit benchmark rationale in the same change. A release must have no enforced regression or infrastructure outcome. Promote an advisory rule only after repeated matched baselines demonstrate that its fixture, client/model configuration, and sample size are stable.

Baseline refresh rationale for issue #30: the new feedback documentation moved the `MAX_MCP_OUTPUT_TOKENS` passage in `docs/README.md`, and review found that the paired `docs/clients.md` judgment also retained an older line number. Both judgments were moved to the current exact passage without changing their query or relevance meaning. The committed lexical baseline was regenerated after that anchor-only correction; no retrieval scoring behavior changed.

## Validate the contract

```bash
go test ./evaluation
```

This loads the seed benchmark, checks that it has 20 scenarios including five fresh-session memory-recovery tasks, and verifies strict schema validation behavior.
