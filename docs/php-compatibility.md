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

The structural and Composer gates are intentionally exact. Framework relationship thresholds leave a narrow allowance for conservative omissions. Ninety-one natural-language retrieval queries with 97 answer-line judgments are scored independently within their corpus using the versioned `terms-v9+php-relationships-v1` adapter. It combines deterministic term and syntax scoring with at most one direct-edge boost among the top 20-100 lexically matched paths; relationships never introduce a candidate. The evaluator reports 42 training, 11 validation, and 38 advisory holdout queries. Independently authored Composer packages and WordPress plugins use retrieval-only holdout corpora so equally valid mappings or hooks cannot invalidate base training labels. Training and validation are blocking at 95% recall@5, 90% MRR and nDCG@5, and zero hard-negative wins.

The final blind six-query terms-v8 generation established recall@5 `1.000`, MRR `0.722`, nDCG@5 `0.794`, and three definition-versus-consumer hard-negative wins. Terms-v9 promotes those measured misses and uses the existing PHP relationship graph to distinguish provider targets from binding sources. Training now scores recall@5 and MRR `1.000`, nDCG@5 `0.998`, and zero hard-negative wins; validation is perfect. A fresh six-query post-terms-v9 generation was authored after the scorer freeze and evaluated once without tuning: recall@5 `1.000`, MRR `0.917`, nDCG@5 `0.938`, and one preserved enum-definition hard-negative win. The expanded advisory holdout scores recall@5 `1.000`, MRR `0.961`, nDCG@5 `0.971`, and one hard-negative win. The PHP 7.4–8.4 roots include distractor files so language-version retrieval is not a one-file check.

`make test` includes this evaluator, and the blocking PHP Compatibility workflow runs it for pull requests and pushes to `main`. Its uploaded JSON report contains aggregate corpus metrics only; query IDs and ranked paths are available solely through the explicit local details flag.

## Accuracy roadmap

The highest-leverage next improvements are:

1. Preserve the post-terms-v9 enum miss, then evaluate a generalized serialized-domain-value role on a new scorer version and a new unseen generation.
2. Measure cold and cached PHP graph latency on large repositories and expose provider build/cache telemetry before increasing the 100-path window.
3. Upgrade the pinned PHP grammar after its valid grouped-import trailing-comma recovery gap is fixed, then rerun malformed-source and six-target cross-build checks.
4. Add scoped function and constant import/reference resolution, dynamic include diagnostics, and ambiguity reporting for duplicate class declarations.
5. Replace bounded YAML, Twig, Blade, Drupal, and WordPress conventions with dedicated parsers only when their per-framework confusion matrices justify the added complexity.
6. Add mutation and property-based negative cases for comments, strings, heredocs, alias reuse, path traversal, and missing Composer candidates.

This keeps accuracy work evidence-driven: each new resolver rule should add a positive expectation, a forbidden edge, and a per-corpus metric before it ships.
