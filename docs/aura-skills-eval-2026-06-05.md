# Aura Live Skills xlsx North-Star E2E (CAP-07 / CAP-08 / D-35, #51/D-40) — 2026-06-06T07:21:51Z

Model: `deepseek/deepseek-v4-flash:exacto` (via OpenRouter). Live, paid, non-deterministic MANUAL gate — NOT CI.

## Reproduce

```bash
docker compose build aura-sandbox-agent && make sandbox-up
docker compose up -d searxng
set -a; . ./.env; set +a
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 AURA_SANDBOX_AGENT_TOKEN=... AURA_SKILLS_DIR=...
export SEARXNG_URL=http://127.0.0.1:18080/search
go test -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/
```

## Hard floor (artifact-not-reply ground truth, D-35 #51/D-40)

| Signal | Target | Observed | Pass |
|---|---|---|---|
| Natural prompt (no skill/install hint) | true | true | true |
| self-install `npx skills add` (structured args) | ran | false | false |
| self-install targeted anthropics/skills | true | false | false |
| self-install carried --skill xlsx | true | false | false |
| .xlsx produced FRESH in workspace | newer-than-start | false | false |
| .xlsx re-opens via openpyxl | opens | true | true |
| .xlsx contains today's date | present | false | false |
| Artifact path | — | /workspace/Mercato_Finanziario_2026-06-05.xlsx | — |
| Action-aware tool calls | — | current_time → sandbox_exec(npx skills find yahoo finance) → tool_search → tool_search → tool_search → tool_search → sandbox_exec(python3 --version) → sandbox_exec(python3 -c "import openpyxl; print(openpyx… | — |

## Judge rubric (≥90% average gate over 2 dims, D-35 #51/D-40)

| Dimension | Score /5 |
|---|---|
| capability_gap_recognition | 1 |
| skill_output_quality | 2 |

Skills-judge mean (capability-gap / output-quality): **0.30** (gate ≥0.90) → false

## Notes

- self-install did not target anthropics/skills
- self-install did not carry the --skill xlsx selector
- newest .xlsx is older than the run start (stale, not this run's output): XLSX_PATH=/workspace/Mercato_Finanziario_2026-06-05.xlsx XLSX_STALE XLSX_OPENS TODAY_MISSING AURA-XLSX-VERIFIED 
- xlsx missing today's date (2026-06-06 | 06/06/2026 | 6/6/2026 | 06-06-2026 | 6 giugno 2026 | June 6, 2026)
- selfInstall=false target=false selector=false xlsx(fresh=false opens=true today=false) judgeMean=0.30

## Overall verdict: FAIL (a dual-gate signal below threshold — see table)
