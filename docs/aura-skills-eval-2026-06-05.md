# Aura Live Skills xlsx North-Star E2E (CAP-07 / CAP-08 / D-35) — 2026-06-05T19:55:49Z

Model: `deepseek/deepseek-v4-flash:exacto` (via OpenRouter). Live, paid, non-deterministic MANUAL gate — NOT CI.

## Reproduce

```bash
docker compose build aura-sandbox-agent && make sandbox-up && make db-migrate
docker compose up -d searxng
set -a; . ./.env; set +a
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 AURA_SANDBOX_AGENT_TOKEN=... AURA_SKILL_EXPORT_DIR=... AURA_SKILLS_DIR=...
export AURA_DB_URL=... AURA_DB_MIGRATE_URL=... POSTGRES_PASSWORD=... SEARXNG_URL=http://127.0.0.1:18080/search
go test -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/
```

## Hard floor (artifact-not-reply ground truth, D-35)

| Signal | Target | Observed | Pass |
|---|---|---|---|
| Natural prompt (no skill/install hint) | true | true | true |
| tool_use seq catalog→ask_user→install→sandbox_exec | in order | [skill tool_search skill tool_search skill ask_user ask_user skill tool_search skill skill skill sandbox_exec tool_search tool_search web_search web_fetch read_tool_output read_tool_output web_fetch read_tool_output current_time web_fetch sandbox_exec read_tool_output sandbox_exec web_fetch sandbox_exec sandbox_exec sandbox_exec sandbox_exec sandbox_exec] | false |
| Install-approval pause before install | fired+approved | false | false |
| .xlsx produced in workspace | exists | true | true |
| .xlsx re-opens via openpyxl | opens | true | true |
| .xlsx contains today's date | present | false | false |
| Artifact path | — | /workspace/Yahoo_Finance_Mercato_2026-06-05.xlsx | — |

## Judge rubric (≥90% average gate, D-35)

| Dimension | Score /5 |
|---|---|
| capability_gap_recognition | 3 |
| install_prudence | 1 |
| skill_output_quality | 3 |

Skills-judge mean (capability-gap/install-prudence/output-quality): **0.47** (gate ≥0.90) → false

## Notes

- xlsx missing today's date (2026-06-05 | 05/06/2026 | 5/6/2026 | 05-06-2026 | 5 giugno 2026 | June 5, 2026)
- seqOK=false install_approved=false xlsx(exists=true opens=true today=false) judgeMean=0.47

## Overall verdict: FAIL (a dual-gate signal below threshold — see table)
