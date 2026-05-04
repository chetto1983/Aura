# Skill Proposal Lifecycle

Date: 2026-05-04

Status: Phase 19 decision

## Decision

Phase 19 closes skill proposals as review-only procedural-memory drafts.

Approving a proposal in `/summaries` means:

- the draft was reviewed by a human;
- Aura must not write, update, or delete any local `SKILL.md` file;
- Aura must not run the proposal smoke prompt automatically;
- the API returns `skill_lifecycle.mode=review_only` so the dashboard and future clients can distinguish "reviewed" from "installed".

Install and smoke remain an explicit admin workflow for a follow-up slice. This avoids silent skill mutation through the generic summaries queue.

## Proposal States

1. `pending`
   - Created by `propose_skill_change`.
   - Contains action, name, optional allowed tools, smoke prompt, reason, and full `SKILL.md` content for create/update.

2. `approved`
   - Marks the draft reviewed only.
   - Does not touch `SKILLS_PATH`, `.claude/skills`, or any skill catalog install path.
   - `skill_lifecycle.review_status=reviewed`.

3. `rejected`
   - Closes the draft with no file writes.
   - `skill_lifecycle.review_status=rejected`.

## Manual Handoff

After approval, the operator may install or apply the skill manually:

- create/update: place the reviewed `SKILL.md` under the chosen skill root;
- delete: remove the reviewed skill only through an explicit admin delete path;
- smoke: run the stored `smoke_prompt` in a bounded session and record the result in the tracker or a future skill-install workflow.

## Follow-Up Workflow

A future admin-gated implementation can replace the manual handoff if it:

- validates `SKILL.md` again before writing;
- writes only through the existing admin skill boundary;
- runs smoke prompts with a bounded fake or explicitly requested live harness;
- records pass/fail without hiding failed proposals;
- leaves normal `/summaries` approval review-only.
