---
name: aura-runtime-safety
description: Use when a task touches Aura's runtime workspace, wiki, skills, MCP config, database-backed state, mail tools, or other user data. Keeps actions bounded, consent-based, and privacy-safe.
---

# Aura Runtime Safety

Use this skill before mutating runtime data in Aura.

## Rules

- Treat `/workspace` as the only normal filesystem boundary for runtime work.
- Read the exact target file before editing it.
- Keep edits small and directly tied to the user's request.
- Never expose secrets, app passwords, API keys, mail dumps, raw OCR text, or private documents in chat.
- Do not mutate database state, mail, MCP config, skills, wiki pages, or source evidence unless the user asked for that mutation.
- For destructive operations, ask for explicit confirmation first.
- After a write, run the smallest useful verification: reread the file, check the specific status, or report the exact patch that was applied.

## Runtime Memory

- Keep source evidence under `wiki/raw/`.
- Keep durable synthesized knowledge in `wiki/*.md`.
- Keep links in `[[slug]]` form.
- Update `wiki/index.md` and `wiki/log.md` when a wiki change changes navigability or audit history.
