# PHP compatibility corpus

This directory contains deterministic, dependency-free fixtures for measuring
Memento's PHP parsing, declaration boundaries, outlines, durable-note anchors,
Composer resolution, framework relationships, and retrieval behavior.

The source is original fixture code maintained in this repository. It models
documented language and framework conventions; it is not copied from framework
repositories and does not require `composer install`, a `vendor/` directory, a
database, or network access.

## Matrix

| Corpus | Target | Representative coverage |
| --- | --- | --- |
| `php-7.4` | PHP 7.4 | Typed properties, variance, arrow functions, `??=`, numeric separators |
| `php-8.0` | PHP 8.0 | Attributes, union types, property promotion, named arguments, `match`, nullsafe access |
| `php-8.1` | PHP 8.1 | Enums, readonly properties, intersections, `never`, first-class callables |
| `php-8.2` | PHP 8.2 | Readonly classes, DNF types, literal return types, trait constants |
| `php-8.3` | PHP 8.3 | Typed class constants, `Override`, dynamic class-constant fetch |
| `php-8.4` | PHP 8.4 | Property hooks, asymmetric setter visibility, `Deprecated` |
| `composer-autoload` | Composer 2 | PSR-4, PSR-0, classmaps, exclusions, files, autoload-dev, longest prefixes |
| `laravel-app` | Laravel 11-shaped app | Routes, controllers, bindings, Eloquent relations, policies, config, Blade |
| `symfony-app` | Symfony 7-shaped app | Attributes, services/routes YAML, Doctrine, Messenger, subscribers, Twig |
| `wordpress-plugin-theme` | WordPress 6-shaped plugin/theme | Bootstrap includes, hooks, shortcodes, legacy classes, template parts |
| `drupal-module` | Drupal 11-shaped module | PHP-bearing Drupal extensions, services/routes YAML, blocks, theme hooks, Twig |

`suite.v2.json` is the strict source of truth. It records required and forbidden
symbols, declaration starts, exact anchor extents, directed relationship
judgments, Composer class resolutions, autoload files, retrieval queries, and
the accuracy floors used by evaluators. Paths are relative to each corpus root.

## Run the checks

Validate the manifest and corpus layout:

```bash
go test ./internal/testutil/phpcompat -count=1
```

Measure parser-backed compatibility with the pinned PHP grammar:

```bash
go run -tags="grammar_subset,grammar_subset_php" ./cmd/php-compat-eval
```

Write a machine-readable report:

```bash
go run -tags="grammar_subset,grammar_subset_php" ./cmd/php-compat-eval \
  -json-out /tmp/memento-php-compat.json
```

The standalone evaluator measures parse success, required symbol recall,
signature-fragment recall, declaration-boundary recall, exact anchor extents,
forbidden body-symbol leakage, and 64 natural-language retrieval queries with
69 answer-line relevance judgments. Retrieval uses the deterministic,
versioned `terms-v5` scorer against each corpus independently. The 34-query
training split contains the original benchmark, promoted measured misses, and
four independent structural-role cases. The 11-query validation split covers
every corpus. The 19 advisory holdout queries combine the original 11-query
post-terms-v3 generation with eight new cases authored in isolation only after
terms-v4 was frozen at `cffc091`. Adding that generation exposed a new
training-corpus tie for explicit `never` termination; the miss was promoted to
training and fixed under a new terms-v5 fingerprint before any terms-v5 holdout
was authored. Validation and holdout queries declare precise hard-negative
ranges. Training and validation are blocking; holdout generations remain
advisory so their first unseen results are preserved instead of tuned away.

JSON contains aggregate overall, per-corpus, and train/validation/holdout
metrics only.
Pass `-retrieval-details` to print query IDs, metrics, and ranked declaration
ranges locally. MCP tests separately consume relationship and Composer
judgments because they require repository graph state.

Lint all PHP-bearing fixture files with the active PHP CLI:

```bash
find evaluation/php-compat/corpora -type f \
  \( -name '*.php' -o -name '*.module' -o -name '*.install' \
     -o -name '*.theme' -o -name '*.inc' \) \
  -print0 | sort -z | xargs -0 -n1 php -l
```

Version-specific CI should lint each language corpus with its declared
`phpVersion`. Framework fixtures declare the minimum language line they model;
they are structural microprojects and are not booted as applications.

## Accuracy policy

Parser, symbol, signature, declaration-boundary, anchor, and Composer judgments
are deterministic and target 100%. Framework relationship recall starts at 95%
with 98% precision, while blocking retrieval targets recall@5 of 95%, MRR of
90%, nDCG@5 of 90%, and zero hard-negative wins. Holdouts are reported against
the same targets but remain advisory. The original 11-query generation recorded
a `terms-v3` baseline of recall@5 `0.909`, MRR `0.773`, nDCG@5 `0.808`, and one
hard-negative win; frozen terms-v4 improves the same generation to recall@5
`1.000`, MRR `0.955`, nDCG@5 `0.966`, and zero hard-negative wins. The isolated
eight-query post-terms-v4 generation scores recall@5, MRR, and nDCG@5 `1.000`
with zero hard-negative wins in isolation. Evaluate framework and language corpora
independently before macro-averaging so a large corpus cannot hide a Drupal- or
WordPress-specific regression.

Additional valid declarations are not automatically failures. Add them to the
manifest when they are part of the intended public structure. Locals from
method bodies, closures, heredocs, comments, or template text must never appear
as declarations.

## Updating fixtures

1. Keep fixture code original, small, and repository-shaped. Do not add vendor
   trees, generated caches, downloaded framework sources, or credentials.
   Minimize observed production misses into original fixtures instead of
   copying production or framework source.
2. Preserve LF line endings and normalized relative paths.
3. Run the active PHP linter and the strict loader test.
4. Update exact declaration starts and anchor extents when source lines move.
5. Assign observed misses to `train`. Use `validation` for repeatable local
   checks, and add independently authored cases to `holdout` only after the
   scorer is frozen. Promote a measured holdout generation to training before
   tuning, then replace it with a fresh unseen generation.
6. Run `php-compat-eval` with `grammar_subset_php`; inspect every per-corpus and
   per-split failure rather than accepting a better aggregate.
7. Add a written benchmark rationale when lowering a threshold or changing the
   meaning of an existing judgment.

The pinned pure-Go grammar is intentionally strict. If a valid construct is
known to produce a recovered/error tree, keep it as an explicitly documented
fallback canary rather than silently weakening global parse-success scoring.
