# PHP compatibility and accuracy

Memento uses the PHP grammar embedded in the pinned pure-Go `gotreesitter v0.32.0` dependency. Successful parses drive `repo_outline`, declaration-aligned index chunks, namespace-scoped related-file relationships, and the full hidden declaration extents used by durable-note anchors. Syntax errors and grammar gaps fail closed to the bounded PHP scanner.

## Supported source shapes

The compatibility corpus covers representative syntax from PHP 7.4 through PHP 8.4, including typed and promoted properties, attributes, union/intersection/DNF types, enums, readonly classes, typed constants, asymmetric visibility, and property hooks. It also includes original repository-shaped Composer, Laravel, Symfony, WordPress, and Drupal microprojects; no framework packages or generated `vendor` trees are checked in.

Default indexing and PHP parsing recognize `.php`, `.php3`, `.php4`, `.php5`, `.phps`, `.phpt`, `.phtml`, `.inc`, `.module`, `.install`, `.theme`, `.profile`, and `.engine`. Blade templates remain indexed and relationship-aware through their `.php` suffix but deliberately use the bounded scanner until a Blade grammar is selected.

## Composer autoloading

Root `composer.json` analysis combines `autoload` and `autoload-dev` for development-time repository navigation and supports:

- PSR-4 longest-prefix and empty-prefix fallback mappings, including ordered directory arrays.
- PSR-0 namespace and PEAR-style underscore mappings.
- Explicit file, directory, and wildcard classmaps for `.php` and `.inc`, with `exclude-from-classmap` handling.
- Composer `files` relationships from `composer.json` to each bounded repository file.
- Deterministic classmap, PSR-4, PSR-0, then declaration-fallback resolution without escaping the repository.

Memento does not execute Composer, emulate optimized/authoritative loaders, or index ignored `vendor` packages. These boundaries keep analysis deterministic and prevent project code execution.

## See the metrics

Run the checked-in compatibility evaluator with:

```bash
make php-compat-eval
```

The command reports strict parse success, required symbol and signature recall, declaration-boundary recall, exact anchor extents, Composer resolution accuracy, relationship precision/recall, and term-aware retrieval precision@5, recall@5, MRR, and nDCG@5. Pass `PHP_COMPAT_ARGS=-retrieval-details` to print each query's metrics and ranked chunks. Expected and forbidden relationships are stored outside each corpus root in `evaluation/php-compat/suite.v2.json`, so fixture source cannot teach the relationship resolver its own answers.

The structural and Composer gates are intentionally exact. Framework relationship thresholds leave a narrow allowance for conservative omissions. Seventy-four natural-language retrieval queries with 79 answer-line judgments are scored independently within their corpus using the versioned `terms-v6` adapter. The evaluator reports 35 training, 11 validation, and 28 advisory holdout queries; validation and holdout judgments use range-bounded hard negatives. Training and validation are blocking at 95% recall@5, 90% MRR and nDCG@5, and zero hard-negative wins. The original 11-query holdout established a terms-v3 baseline of recall@5 `0.909`, MRR `0.773`, nDCG@5 `0.808`, and one hard-negative win. After terms-v4 was frozen at `cffc091`, that same generation scored recall@5 `1.000`, MRR `0.955`, nDCG@5 `0.966`, and zero hard-negative wins. An isolated post-terms-v4 generation ranked all eight first in isolation but exposed a tie in an existing `never`-termination training query when added to the full corpus, leading to terms-v5. A second isolated six-query generation then recorded recall@5 `1.000`, MRR `0.917`, nDCG@5 `0.938`, and one hard-negative win; its deferred-callable paraphrase miss was promoted before terms-v6. The first blind four-query terms-v6 generation recorded recall@5 `0.750`, MRR `0.625`, nDCG@5 `0.658`, and one hard-negative win: framework-neutral parent-to-collection wording missed the Eloquent relationship method and is reserved for terms-v7 training. The PHP 7.4–8.4 roots include distractor files so language-version retrieval is not a one-file check.

`make test` includes this evaluator, and the blocking PHP Compatibility workflow runs it for pull requests and pushes to `main`. Its uploaded JSON report contains aggregate corpus metrics only; query IDs and ranked paths are available solely through the explicit local details flag.

## Accuracy roadmap

The highest-leverage next improvements are:

1. Repeat the isolated post-freeze protocol with broader phrasing, more framework configuration shapes, and non-PHP consumers while keeping each generation advisory.
2. Evaluate a bounded relationship-provider reranker that only promotes lexically matched candidates through explicit direct edges, with the production MCP path and evaluator sharing the same adapter.
3. Upgrade the pinned PHP grammar after its valid grouped-import trailing-comma recovery gap is fixed, then rerun malformed-source and six-target cross-build checks.
4. Add scoped function and constant import/reference resolution, dynamic include diagnostics, and ambiguity reporting for duplicate class declarations.
5. Replace bounded YAML, Twig, Blade, Drupal, and WordPress conventions with dedicated parsers only when their per-framework confusion matrices justify the added complexity.
6. Add mutation and property-based negative cases for comments, strings, heredocs, alias reuse, path traversal, and missing Composer candidates.

This keeps accuracy work evidence-driven: each new resolver rule should add a positive expectation, a forbidden edge, and a per-corpus metric before it ships.
