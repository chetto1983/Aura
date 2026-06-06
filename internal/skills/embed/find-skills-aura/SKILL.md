---
name: find-skills-aura
description: Discover and install agent skills from the open skills ecosystem (skills.sh) when your current capabilities don't cover a task — file formats (xlsx, pdf, docx), integrations, or specialized workflows.
always: true
---

# Find Skills

You can extend yourself with skills from the open agent skills ecosystem. When a task
needs a capability you don't have a battle-tested method for (file formats like
xlsx/pdf/docx, integrations, specialized workflows), search the ecosystem BEFORE
hand-coding the deliverable — a vetted skill ships tested instructions and bundled
scripts that beat ad-hoc code.

## How to search

Run the skills CLI in your terminal:

    npx skills find <query>

- Query by the artifact FORMAT or capability family ("xlsx", "pdf", "docx"), not the
  data topic — skills package capabilities; the data comes from your other tools.
- Output lines look like `owner/repo@skill-name  N installs` followed by a
  skills.sh URL, ranked by installs.
- To inspect what a multi-skill repository contains: `npx skills add <owner/repo> --list`
  (lists skills with descriptions; it does NOT install anything).

## How to choose

- Prefer the most-installed result from a reputable source (anthropics, vercel-labs,
  microsoft). Be skeptical of low-install results and name-clones of popular skills —
  name-squatting exists in the ecosystem.

## How to install and use

Install your chosen skill so it persists and loads. Your skills home is `~/.aura/skills/export`
(the directory the loader scans for installed skills); install there so the skill survives
across sessions and is loadable by name:

    cd ~/.aura/skills/export && npx skills add <owner/repo> --skill <skill-name> --copy -y

The skill lands under `~/.aura/skills/export/.agents/skills/<skill-name>/`. Then:

1. Read its `SKILL.md` and follow the instructions it gives you.
2. Run any bundled scripts BY PATH with the interpreter (e.g.
   `python3 ~/.aura/skills/export/.agents/skills/<name>/scripts/...`) — never rely on the
   exec bit.

No approval round-trip is needed — install directly in your terminal.

## If nothing fits

Say that no existing skill covers the task and proceed with your own capabilities.
