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

Having the underlying libraries installed (openpyxl, pandas, a node package) is NOT
having a method: the skill is the battle-tested playbook for the format — conventions,
edge cases, bundled scripts — not the library. For any deliverable in a packaged task
family, run the search below FIRST; hand-code only when it comes up empty.

## How to search

Run the skills CLI in your terminal — it only prints, it installs nothing:

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

Install with the skill tool, never with the CLI:

    skill action=install source=<owner/repo>

Add the skill name when a repository ships several: `source=<owner/repo>@<skill-name>`.

That one call fetches the skill, validates it and puts it in your library — it is listed
by `skill action=list` and usable with `skill action=use` on this same turn. No approval
round-trip, nothing to wait for.

**Do not install with `npx skills add`.** It writes into whatever directory your shell is
standing in, which is not the library: the CLI prints "Installation complete" and the
skill does not exist as far as Aura is concerned. If you have already run it, do not try
to move the files into the skills mount — that mount is a read-only mirror and any copy
you make there is erased on the next refresh. Just run the install action.

After installing:

1. Read the skill with `skill action=info name=<name>` and follow what it says.
2. Run any bundled script BY PATH with its interpreter (e.g. `python3 <path>`) — never
   rely on the exec bit.

## If nothing fits

Say that no existing skill covers the task and proceed with your own capabilities.
