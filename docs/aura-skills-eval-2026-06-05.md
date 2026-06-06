# Aura Live Skills xlsx North-Star E2E (CAP-07 / CAP-08 / D-35, #51/D-40, #52/D-41) — 2026-06-06T08:52:10Z

Model: `deepseek/deepseek-v4-flash:exacto` (via OpenRouter). Live, paid, non-deterministic MANUAL gate — NOT CI.

## Reproduce

```bash
# #52/D-41: the skills loop runs on the HOST terminal (shell_exec) — no sandbox-up.
docker compose up -d searxng
set -a; . ./.env; set +a
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export AURA_SKILLS_DIR=... AURA_RUN_DIR=...
export SEARXNG_URL=http://127.0.0.1:18080/search
go test -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/
```

Host run workspace (open the produced .xlsx here): `D:/tmp/aura-run\aura-skills-e2e-603978956`

## Hard floor (artifact-not-reply ground truth, D-35 #51/D-40, #52/D-41 host surface)

| Signal | Target | Observed | Pass |
|---|---|---|---|
| Natural prompt (no skill/install hint) | true | true | true |
| self-install `npx skills add` (structured args) | ran | true | true |
| self-install targeted anthropics/skills | true | true | true |
| self-install carried --skill xlsx | true | true | true |
| .xlsx produced FRESH in host workspace | newer-than-start | false | false |
| .xlsx re-opens via openpyxl | opens | false | false |
| .xlsx contains today's date | present | false | false |
| Artifact path | — |  | — |
| Action-aware tool calls | — | current_time → shell_exec(npx skills find xlsx 2>&1 | head -40) → tool_search → shell_exec(mkdir -p ~/.aura/skills/export && cd ~/.aura/skills/export && npx skills add anthropics/skills --skill xlsx --copy -y 2>&1)… | — |

## Judge rubric (≥90% average gate over 2 dims, D-35 #51/D-40)

| Dimension | Score /5 |
|---|---|
| capability_gap_recognition | 4 |
| skill_output_quality | 2 |

Skills-judge mean (capability-gap / output-quality): **0.60** (gate ≥0.90) → false

## Notes

- no .xlsx found under the host workspace D:/tmp/aura-run\aura-skills-e2e-603978956
- selfInstall=true target=true selector=true xlsx(fresh=false opens=false today=false) judgeMean=0.60

## Overall verdict: FAIL (a dual-gate signal below threshold — see table)
