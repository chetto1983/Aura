# Aura Live Skills xlsx North-Star E2E (CAP-07 / CAP-08 / D-35, #51/D-40, #52/D-41) — 2026-06-11T09:06:01Z

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

Host run workspace (open the produced .xlsx here): `D:\tmp\aura-run\aura-e2e-3914561311`

## Hard floor (artifact-not-reply ground truth, D-35 #51/D-40, #52/D-41 host surface)

| Signal | Target | Observed | Pass |
|---|---|---|---|
| Natural prompt (no skill/install hint) | true | true | true |
| self-install `npx skills add` (structured args) | ran | true | true |
| self-install targeted a concrete skills source | true | true | true |
| self-install included xlsx/excel/spreadsheet capability | true | true | true |
| .xlsx produced FRESH in host workspace | newer-than-start | true | true |
| .xlsx re-opens via openpyxl | opens | true | true |
| .xlsx contains today's date | present | true | true |
| Artifact path | — | D:\tmp\aura-run\aura-e2e-3914561311\mercato_yahoo_finance.xlsx | — |
| Action-aware tool calls | — | shell_exec(cd ~/.aura/skills/export && npx skills find "xlsx" 2>&1 | head -40) → shell_exec(npx skills find "yahoo finance" 2>&1 | head -30) → shell_exec(cd ~/.aura/skills/export && npx skills add anthropics/skills -… | — |

## Judge rubric (≥90% average gate over 2 dims, D-35 #51/D-40)

| Dimension | Score /5 |
|---|---|
| capability_gap_recognition | 5 |
| skill_output_quality | 5 |

Skills-judge mean (capability-gap / output-quality): **1.00** (gate ≥0.90) → true

## Notes

- selfInstall=true target=true selector=true xlsx(fresh=true opens=true today=true) judgeMean=1.00

## Overall verdict: PASS
