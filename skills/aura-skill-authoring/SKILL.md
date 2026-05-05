---
name: aura-skill-authoring
description: Use when Aura should create, improve, evaluate, or propose a local SKILL.md procedure for repeated workflows or agent behavior.
---

# Aura Skill Authoring

## Overview

Aura skills are procedural memory. They should be created when a workflow will repeat, when agents keep making the same mistake, or when a successful pattern should become reusable.

Use `propose_skill_change` for skill mutations. Do not directly install, edit, or delete skills from chat unless the user is operating outside Aura with filesystem tools.

## Skill Shape

Each skill lives under `skills/<skill-name>/SKILL.md`:

```markdown
---
name: skill-name
description: Use when the trigger condition applies.
---

# Skill Name

Instructions...
```

## Authoring Rules

- Name uses letters, numbers, and hyphens.
- Description starts with `Use when` and describes trigger conditions, not the whole workflow.
- Keep the body focused on reusable procedure, not a one-off story.
- Include non-negotiables when the agent must not improvise.
- Include a short quality bar when output quality matters.
- Include smoke prompts or examples only when they help future verification.
- Prefer small Aura-specific skills over copying large generic skills.

## Proposal Flow

When creating or changing a skill inside Aura:

1. Identify the repeated workflow or failure.
2. Draft the full `SKILL.md` content.
3. Call `propose_skill_change` with action `create`, `update`, or `delete`.
4. Include `origin_reason`, evidence, and a smoke prompt.
5. Wait for human review before the skill becomes durable.

## Common Mistakes

| Mistake | Fix |
| --- | --- |
| Creating a skill for one task | Put one-off details in the answer or wiki instead |
| Generic description | Use concrete triggers the agent will recognize |
| Huge copied skill body | Keep Aura runtime skills compact and local |
| Direct mutation from chat | Use review-gated `propose_skill_change` |
