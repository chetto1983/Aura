# Aura Skills

Local Aura skills live in subdirectories here:

```text
skills/
  example-skill/
    SKILL.md
```

Each `SKILL.md` uses YAML frontmatter:

```markdown
---
name: example-skill
description: When Aura should use this skill.
---

Skill instructions go here.
```

Aura loads these local skills into the system prompt on each turn and exposes
read-only `list_skills` / `read_skill` tools. Discover installable community
skills with `search_skill_catalog`, which uses https://skills.sh/.
