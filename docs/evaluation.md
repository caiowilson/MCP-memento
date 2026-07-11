# Helpfulness evaluation

Memento's existing retrieval evaluation answers whether relevant code ranks well. The helpfulness contract adds the question that matters to users: does an agent complete repository work better with Memento than without it?

The contract lives in `evaluation/fixtures/helpfulness.json`. It is versioned and strict: unknown fields and invalid task shapes fail loading through `evaluation.LoadHelpfulnessFixtureFile`.

## Paired experiment

Run every task twice against the same checkout:

1. Baseline: repository tools without Memento.
2. Treatment: the same tools plus Memento.

Keep the prompt, starting state, model, model settings, tokenizer configuration, budgets, and validation rubric identical. Do not compare runs across a model or fixture revision without clearly recording that change.

The contract currently describes tasks; it does not execute agents. The automated runner will consume the same fixture format and write the reports used by CI.

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

The benchmark is local and fixture-backed. It must not upload source code, paths, prompts, queries, notes, or memory contents. Any future user feedback remains opt-in and aggregate-only.

For qualitative tasks, export only the response and declared rubric to a blinded reviewer. The reviewer must not know whether the response came from the baseline or Memento condition.

## Validate the contract

```bash
go test ./evaluation
```

This loads the seed benchmark, checks that it has 20 scenarios including five fresh-session memory-recovery tasks, and verifies strict schema validation behavior.
