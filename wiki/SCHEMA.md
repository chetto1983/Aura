# Aura Wiki Schema

Every wiki page stored under `/wiki/` uses markdown with YAML frontmatter.

## Page Structure

Each wiki page is a `.md` file with YAML frontmatter and markdown body:

```markdown
---
title: Machine Learning Basics
tags:
  - ml
  - beginners
category: engineering
related:
  - neural-networks
  - deep-learning
sources:
  - https://example.com/ml-intro
schema_version: 2
prompt_version: ingest_v1
created_at: "2026-04-28T12:00:00Z"
updated_at: "2026-04-28T12:00:00Z"
---

# Machine Learning Basics

Machine learning is a subset of AI. See [[neural-networks]] for more.
```

## Frontmatter Fields

- `title` (required) — Short descriptive title, max 200 chars
- `tags` (optional) — List of strings, each max 50 chars, max 10 tags
- `category` (optional) — Category string, max 50 chars
- `related` (optional) — List of page slugs this page links to, each max 100 chars
- `sources` (optional) — List of source URLs, max 10, each max 200 chars
- `schema_version` (required) — Must be `2`
- `prompt_version` (required) — Must match `v[0-9]+` or `ingest_v[0-9]+`
- `created_at` (required) — ISO 8601 timestamp
- `updated_at` (required) — ISO 8601 timestamp

## Body

The markdown body follows the closing `---` delimiter. Use `[[slug]]` syntax to create wiki-links between pages.

## File Naming

- Files named by slug: lowercase, spaces replaced with hyphens
- Extension: `.md`
- Example: title "Machine Learning Basics" -> `machine-learning-basics.md`

## Special Files

- `index.md` — Auto-generated catalog of all pages, grouped by category
- `log.md` — Append-only audit trail of all wiki operations

## Legacy Format

Older pages may exist as `.yaml` files with a `content` field. These are migrated to `.md` on startup via `MigrateYAMLToMD`. The `ReadPage` method falls back to `.yaml` if no `.md` exists.

## Validation Rules

- `title`: Non-empty, max 200 chars
- `body`: Non-empty (markdown after frontmatter)
- `tags`: Max 10, each max 50 chars
- `category`: Max 50 chars
- `related`: Each slug max 100 chars
- `sources`: Max 10, each max 200 chars
- `schema_version`: Must be `2`
- `prompt_version`: Must match `v[0-9]+` or `ingest_v[0-9]+`
- `created_at` / `updated_at`: Valid ISO 8601

## Atomic Writes

All writes use temp file + rename. Per-file mutex prevents concurrent writes.

## Git Versioning

All changes auto-committed: `wiki: <action> <slug>`