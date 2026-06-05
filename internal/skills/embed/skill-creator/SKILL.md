---
name: skill-creator
description: Guide for authoring or revising an Aura skill. Use this when the operator wants to create a new skill, update an existing one, write a trigger-rich description, decide whether a skill should always be active, or define an executable snippet skill.
metadata:
  short-description: Create or update an Aura skill
---

# Skill Creator

This skill teaches how to write an effective Aura skill: a self-contained directory
holding a `SKILL.md` (required frontmatter + markdown body) plus optional bundled
resources. A skill is an onboarding guide that gives you procedural knowledge for a
specific domain or workflow you could not fully possess from training alone.

## Anatomy

```
skill-name/
├── SKILL.md            (required)
│   ├── YAML frontmatter (required: name, description)
│   └── Markdown body   (the instructions, loaded only after the skill is applied)
└── bundled resources   (optional: scripts/, references/, assets/)
```

- `name` — lowercase letters, digits and hyphens, 1..64 chars, and MUST equal the
  directory name (e.g. directory `pdf-fill/` ⇒ `name: pdf-fill`).
- `description` — the ONLY field consulted to decide when a skill applies. Make it
  trigger-rich: name the task, the file formats, and the verbs an operator would use.
  Double-quote it if it contains a colon or quotes.

## Writing a trigger-rich description

The description is matched against the operator's intent, so spell out the triggers:

- Weak: `description: Works with spreadsheets.`
- Strong: `description: Read, edit, and create .xlsx Excel workbooks — set cell values, formulas, and formatting, then read the result back. Use when the operator mentions Excel, spreadsheets, or .xlsx files.`

Lead with the concrete capability, then list the surface forms ("Excel", ".xlsx",
"spreadsheet") an operator might say.

## When `always: true` is appropriate

Set `always: true` ONLY for a skill whose guidance must steer every turn regardless
of the request — house style, a standing safety rule, a persistent persona note. An
always-on skill's body is injected into context for every turn, so it costs tokens
continuously: keep it short and reserve it for genuinely cross-cutting guidance. A
task-specific skill must NOT be always-on; let it be discovered by its description.

## Body discipline

The context window is shared with everything else. Add only what the model does not
already know. Prefer a concise example over a verbose explanation. Match the level of
specificity to the task's fragility: give narrow step-by-step guardrails for fragile,
error-prone operations; give high-level guidance where many approaches are valid.

## Snippet skills (`type: snippet`)

A snippet skill ships an executable code body run in the sandbox by the runtime,
instead of (or alongside) instructions. Declare it in the frontmatter:

```yaml
---
name: csv-stats
description: Compute summary statistics for a CSV file in the workspace.
type: snippet
language: python
needs_workspace: true
deps: [pandas]
---
```

- `language` — the interpreter the runtime invokes (e.g. `python`).
- `needs_workspace` / `needs_network` — declare the resources the snippet requires.
- `deps` — documentation of the libraries the snippet expects; it is NEVER a runtime
  install. The sandbox image provides the dependency set; an unmet dep is a packaging
  task, not something the snippet installs at run time.

Keep the snippet body deterministic and side-effect-scoped to the workspace.
