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

The structural and Composer gates are intentionally exact. Framework relationship thresholds leave a narrow allowance for conservative omissions. Fifty-two natural-language retrieval queries with 57 answer-line judgments are scored independently within their corpus using the versioned `terms-v3` adapter. The evaluator reports 30 training, 11 validation, and 11 fresh holdout queries separately; validation and holdout cover every corpus and include range-bounded hard negatives. The holdout was authored after the scorer was frozen at `da8e60b`, without running retrieval. Training and validation are blocking at 95% recall@5, 90% MRR and nDCG@5, and zero hard-negative wins. The unseen holdout is advisory and establishes an honest baseline of recall@5 `0.909`, MRR `0.773`, nDCG@5 `0.808`, and one hard-negative win. The PHP 7.4–8.4 roots include distractor files so language-version retrieval is not a one-file check.

`make test` includes this evaluator, and the blocking PHP Compatibility workflow runs it for pull requests and pushes to `main`. Its uploaded JSON report contains aggregate corpus metrics only; query IDs and ranked paths are available solely through the explicit local details flag.

## Accuracy roadmap

The highest-leverage next improvements are:

1. Grow future holdout generations from minimized, original reproductions of observed production misses, with matched hard negatives and no copied application source.
2. Address the measured holdout misses: declaration-vs-reference intent for Laravel configuration, attribute/metadata intent in PHP 8.4, and exact callable/entity-mapping rank in PHP 8.1 and Symfony.
3. Upgrade the pinned PHP grammar after its valid grouped-import trailing-comma recovery gap is fixed, then rerun malformed-source and six-target cross-build checks.
4. Add scoped function and constant import/reference resolution, dynamic include diagnostics, and ambiguity reporting for duplicate class declarations.
5. Replace bounded YAML, Twig, Blade, Drupal, and WordPress conventions with dedicated parsers only when their per-framework confusion matrices justify the added complexity.
6. Add mutation and property-based negative cases for comments, strings, heredocs, alias reuse, path traversal, and missing Composer candidates.

This keeps accuracy work evidence-driven: each new resolver rule should add a positive expectation, a forbidden edge, and a per-corpus metric before it ships.
