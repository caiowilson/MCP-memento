# Paired helpfulness visual report

Report v1 · fixture `sha256:fixture`

## Decisions

| Decision | Result | Evidence |
| --- | --- | --- |
| Task-success regression | no regression | baseline 50.0% (9.5%–90.5%); Memento 100.0% (34.2%–100.0%); 0 invalid pairs; 0 timeout pairs |
| Efficiency/token-cost regression | no regression | 1200 → 880 total tokens; 1 paired run; 1 unavailable total-token pairs |
| Retrieval-quality regression | no regression | 2 configuration snapshots |

## Token usage

| Task (category) | Input baseline → Memento | Output baseline → Memento | Total baseline → Memento | Total saved |
| --- | ---: | ---: | ---: | ---: |
| discovery-task (discovery) | 1000 → 700 | 200 → 180 | 1200 → 880 | 320 (26.7%) |
| onboarding-task (onboarding) | unavailable | unavailable | unavailable | unavailable |
| Aggregate (input 1 / output 1 / total 1 paired runs) | 1000 → 700 | 200 → 180 | 1200 → 880 | 320 (26.7%) |

1 task pairs have unavailable total-token usage and are excluded from total-token aggregates.

## Usage provenance

| Task | Condition | Usage source | Model fingerprint | Tokenizer fingerprint | Configuration fingerprint |
| --- | --- | --- | --- | --- | --- |
| discovery-task | baseline | usage-client | sha256:model | sha256:tokenizer-example | sha256:baseline-config |
| discovery-task | Memento | usage-client | sha256:model | sha256:tokenizer-example | sha256:memento-config |
| onboarding-task | baseline | unavailable | unavailable | unavailable | sha256:baseline-config |
| onboarding-task | Memento | unavailable | unavailable | unavailable | sha256:memento-config |

## Task success

| Condition | Success rate (95% Wilson interval) | Sample |
| --- | ---: | ---: |
| Baseline | 50.0% (9.5%–90.5%) | 2 |
| Memento | 100.0% (34.2%–100.0%) | 2 |

## Retrieval small multiples

| Configuration | Mode | Precision@k | Recall@k | MRR | nDCG |
| --- | --- | ---: | ---: | ---: | ---: |
| sha256:lexical | lexical | 0.200 | 0.300 | 0.300 | 0.250 |
| sha256:semantic | semantic | 0.300 | 0.400 | 0.400 | 0.350 |

## Benchmark trend

| Revision | Task success | Total tokens saved | Fixture / model / prompt / tokenizer / scoring | Change annotation |
| --- | ---: | ---: | --- | --- |
| abc123 | 75.0% | 320 | sha256:fixture / sha256:model / sha256:prompt / sha256:tokenizer-example / sha256:score | baseline |

## Opt-in aggregate feedback

| Respondents | Helpful | Neutral | Unhelpful |
| ---: | ---: | ---: | ---: |
| 3 | 2 | 1 | 0 |
