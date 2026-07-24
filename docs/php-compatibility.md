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

The structural and Composer gates are intentionally exact. Framework relationship thresholds leave a narrow allowance for conservative omissions. Ninety-seven natural-language retrieval queries with 103 answer-line judgments are scored independently within their corpus using the versioned `terms-v15+php-relationships-v1` adapter. It combines deterministic term and syntax scoring with at most one direct-edge boost among the top 20-100 lexically matched paths; relationships never introduce a candidate. The evaluator reports 48 training, 11 validation, and 38 advisory holdout queries. Independently authored Composer packages and WordPress plugins use retrieval-only holdout corpora so equally valid mappings or hooks cannot invalidate base training labels. Training and validation are blocking at 95% recall@5, 90% MRR and nDCG@5, and zero hard-negative wins.

The final blind six-query terms-v8 generation established recall@5 `1.000`, MRR `0.722`, nDCG@5 `0.794`, and three definition-versus-consumer hard-negative wins. Terms-v9 promotes those measured misses and uses the existing PHP relationship graph to distinguish provider targets from binding sources. A fresh six-query post-terms-v9 generation was authored after scorer freeze and evaluated once without tuning: recall@5 `1.000`, MRR `0.917`, nDCG@5 `0.938`, and one enum-definition hard-negative win. Terms-v10 promotes that miss and recognizes serialized closed-domain definitions compositionally. Its independent post-freeze durable-catalog-spelling query retained recall@5 `1.000` but ranked the enum second, with MRR `0.500`, nDCG@5 `0.631`, and one advisory win. Terms-v11 promotes that evidence and adds exact-token synonym coverage plus a narrowly gated backed-enum-header score. Its independent post-freeze every-domain singular-spelling query again ranked the enum second. Terms-v12 promotes that miss and filters high-confidence catalog intent vocabulary from lexical competition. Its independent post-freeze authoritative-closed-set/stored-token query ranked the enum second behind a serializer. Terms-v13 promotes that miss, recognizes exact stored-token domain values behind an enum-concept guard, and scopes consumer detection to the provider-definition clause. Its independent post-freeze membership-freeze-cause query ranked the enum second behind a presenter under unseen authoritative-domain-declaration/allowed-code language. Terms-v14 promotes that evidence, adds a provider-scoped authoritative-domain intent, and filters only its classifier vocabulary while retaining the domain terms. Its independent post-freeze cold-chain disposition query ranked the enum second behind a presenter under unseen permitted/stored-values language. Terms-v15 promotes that evidence, adds exact `permitted` closed-set coverage inside the full authoritative-domain conjunction, and filters `permitted` and `stored` only for that role. On the 96-query pre-holdout suite, overall recall@5 is `1.000`, MRR is `0.990`, nDCG@5 is `0.991`, and hard-negative wins are zero; the 48-query training split has recall@5 and MRR `1.000`, nDCG@5 `0.998`, and zero wins, while validation remains perfect. A new independent post-terms-v15 orchard-frost query preserves recall@5 `1.000` but ranks its enum third behind a runbook and an unrelated orchard endpoint under unseen canonical-list/available-actions language. The full 97-query suite records recall@5 `1.000`, MRR `0.983`, nDCG@5 `0.986`, and one advisory win; the 38-query holdout records recall@5 `1.000`, MRR `0.956`, and nDCG@5 `0.967`. The PHP 7.4–8.4 roots include distractor files so language-version retrieval is not a one-file check.

`make test` includes this evaluator, and the blocking PHP Compatibility workflow runs it for pull requests and pushes to `main`. Its uploaded JSON report contains aggregate corpus metrics only; query IDs and ranked paths are available solely through the explicit local details flag.

## Accuracy roadmap

The highest-leverage next improvements are:

1. Preserve the post-terms-v15 orchard-frost miss, then generalize canonical-list/available-actions provider language and orchard-name collision handling without promoting arbitrary lists or consumer requests.
2. Measure cold and cached PHP graph latency on large repositories and expose provider build/cache telemetry before increasing the 100-path window.
3. Upgrade the pinned PHP grammar after its valid grouped-import trailing-comma recovery gap is fixed, then rerun malformed-source and six-target cross-build checks.
4. Add scoped function and constant import/reference resolution, dynamic include diagnostics, and ambiguity reporting for duplicate class declarations.
5. Replace bounded YAML, Twig, Blade, Drupal, and WordPress conventions with dedicated parsers only when their per-framework confusion matrices justify the added complexity.
6. Add mutation and property-based negative cases for comments, strings, heredocs, alias reuse, path traversal, and missing Composer candidates.

This keeps accuracy work evidence-driven: each new resolver rule should add a positive expectation, a forbidden edge, and a per-corpus metric before it ships.
