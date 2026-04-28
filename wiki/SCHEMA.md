# Aura Wiki Schema

Every wiki page stored under `/wiki/` must conform to this schema.

## Page Structure

Each wiki page is a YAML file with the following fields:

```yaml
title: string (required) - Short descriptive title for the page
content: string (required) - Main content of the wiki page
tags: list of strings (optional) - Tags for categorization
schema_version: integer (required) - Must be 1
prompt_version: string (required) - Version of the prompt that generated this page
created_at: string (required) - ISO 8601 timestamp
updated_at: string (required) - ISO 8601 timestamp
```

## Validation Rules

- `title`: Non-empty string, max 200 characters
- `content`: Non-empty string
- `tags`: List of strings, each max 50 characters, max 10 tags
- `schema_version`: Must be exactly 1
- `prompt_version`: Non-empty string, must match pattern `v[0-9]+` or `ingest_v[0-9]+`
- `created_at`: Valid ISO 8601 datetime string
- `updated_at`: Valid ISO 8601 datetime string

## File Naming

- Files are named by slug derived from the title: lowercase, spaces replaced with hyphens
- Example: title "Machine Learning Basics" -> file `machine-learning-basics.yaml`
- File extension: `.yaml`

## Atomic Writes

- All writes use temp file + rename pattern to prevent corruption
- A file-level mutex ensures only one write at a time per wiki page

## Git Versioning

- All wiki changes are committed to Git automatically
- Each commit message follows: `wiki: <action> <page-slug>`