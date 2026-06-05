---
spike: 006
name: xlsx-skill-dry-run
type: standard
validates: "Given anthropics/skills/xlsx installed via the 004 winner, when its SKILL.md is parsed (frontmatter tolerance) and its bundled scripts run by path in the sandbox image, then openpyxl resolves and a real .xlsx is produced and read back"
verdict: VALIDATED
related: [003, 004b, 005]
tags: [skills, xlsx, sandbox, north-star, phase-11]
---

# Spike 006: xlsx-skill-dry-run

## What This Validates

The Phase-11 North-Star's ingredients (D-35, `11-CONTEXT.md`): the REAL `anthropics/skills/xlsx` — discovered via the 003 API, installed via the 004b native path, materialized into the 005 ro `/skills` mount — parses under Aura's planned tolerance rules and physically produces a verified `.xlsx` artifact in the sandbox, including bundled-script by-path execution.

## How to Run

```bash
# 1. Build the dep-baked spike image + swap the service (keeps the /skills mount):
docker build -t aura-sandbox-agent:spike006 .planning/spikes/006-xlsx-skill-dry-run
docker compose -f compose.yaml -f .planning/spikes/006-xlsx-skill-dry-run/compose.spike006.yaml up -d aura-sandbox-agent
# 2. Materialize the skill (from the 004b clone or a fresh one):
cp -r /d/tmp/spike-004b/xlsx .planning/spikes/005-skills-ro-mount/export/xlsx
# 3. Harness:
go run ./.planning/spikes/006-xlsx-skill-dry-run
# 4. Restore production:
rm -rf .planning/spikes/005-skills-ro-mount/export/xlsx
docker compose -f compose.yaml up -d aura-sandbox-agent
```

## What to Expect

4 PASS lines (frontmatter, openpyxl import, produce+readback with marker/float/formula, bundled `pack.py --help`), `[SUMMARY] VALIDATED`, exit 0.

## Investigation Trail

1. **PLAN-CHANGING FINDING (first probe):** the production `aura-sandbox-agent:py3` image has **NO openpyxl and NO pandas** — the Phase-5 batteries-included curated bake (numpy/pandas/.../openpyxl) **never carried over to the sandbox-agent pivot** (its Dockerfile only adds python3+pip). The runtime bridge is non-masquerating (egressless) → runtime `pip install` is impossible. The xlsx North-Star CANNOT run on today's image.
2. Fix proven: derived image baking deps at BUILD time (host network). PEP 668 gotcha: Debian 12's python3.11 is externally-managed → `pip3 install --break-system-packages` required (the sandbox IS the isolation boundary, no venv needed).
3. First harness run 3/4: bundled `pack.py` failed at `import defusedxml`. Scanned ALL skill scripts for top-level imports → non-stdlib set: **openpyxl, defusedxml, lxml, validators**. Rebaked → 4/4.
4. Frontmatter reality check: xlsx's `description` is a double-quoted YAML scalar with escaped inner quotes (`\"...\"`) — **a naive `key: value` splitter (picobot-style) breaks on it; 7a needs a real YAML lib.** `license: Proprietary…` field present (tolerance confirmed necessary). Files are CRLF on Windows checkouts (004b) — parser must normalize.
5. Artifact ground truth: `.xlsx` written to `/workspace`, re-loaded with openpyxl, marker + float + formula (`=B2*2`) all read back exactly.

## Results

**VALIDATED** — every North-Star ingredient is live: API discovery (003) → native install (004b) → ro-mount materialization (005) → tolerant parse → sandboxed artifact production + bundled-script by-path (006).

**Plan-changing obligations for Phase 11:**
- **7e MUST extend `docker/sandbox-agent/Dockerfile`** with a hash-pinned curated dep set (Phase-5 D-20 discipline; floor: openpyxl + defusedxml + lxml + validators + the original Phase-5 list — pandas/numpy/etc.) — without it the North-Star E2E cannot pass. Image rebuild = `make sandbox-up` docs note.
- 7a parser = real YAML lib (quoted-scalar reality) + CRLF normalization.
- Egressless runtime is a FEATURE (snippet `deps:` frontmatter is documentation, never runtime pip — consistent with D-20 docs-only).
