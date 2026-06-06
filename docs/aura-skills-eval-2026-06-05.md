# Aura Live Skills xlsx North-Star E2E (CAP-07 / CAP-08 / D-35, #51/D-40, #52/D-41) — 2026-06-06T10:06:05Z

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

Host run workspace (open the produced .xlsx here): `D:/tmp/aura-run\aura-e2e-3314272960`

## Hard floor (artifact-not-reply ground truth, D-35 #51/D-40, #52/D-41 host surface)

| Signal | Target | Observed | Pass |
|---|---|---|---|
| Natural prompt (no skill/install hint) | true | true | true |
| self-install `npx skills add` (structured args) | ran | false | false |
| self-install targeted anthropics/skills | true | false | false |
| self-install carried --skill xlsx | true | false | false |
| .xlsx produced FRESH in host workspace | newer-than-start | false | false |
| .xlsx re-opens via openpyxl | opens | false | false |
| .xlsx contains today's date | present | false | false |
| Artifact path | — |  | — |
| Action-aware tool calls | — | current_time → tool_search → shell_exec(echo $SHELL && which python3 && python3 --version && pip list 2>/dev/null | grep -iE "openpyxl|xlsxwriter|pandas|yfinance") → fs_write → shell_exec(cd /tmp && python3 marke… | — |

## Judge rubric (≥90% average gate over 2 dims, D-35 #51/D-40)

| Dimension | Score /5 |
|---|---|
| capability_gap_recognition | 1 |
| skill_output_quality | 1 |

Skills-judge mean (capability-gap / output-quality): **0.20** (gate ≥0.90) → false

## Notes

- self-install did not target anthropics/skills
- self-install did not carry the --skill xlsx selector
- no .xlsx found under the host workspace D:/tmp/aura-run\aura-e2e-3314272960
- selfInstall=false target=false selector=false xlsx(fresh=false opens=false today=false) judgeMean=0.20

## Overall verdict: FAIL (a dual-gate signal below threshold — see table)
