# Aura Live Swarm E2E (CAP-03 / SC#5 / D-22) — 2026-06-04T13:08:38Z

Model: `deepseek/deepseek-v4-flash:exacto` (via OpenRouter). Live, paid, non-deterministic MANUAL gate — NOT CI.

## Reproduce

```bash
set -a; . ./.env; set +a
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export AURA_EVAL_SELF_MAIL=... AURA_EVAL_SELF_PHONE=... AURA_EVAL_WA_CHAT_SELF=...@s.whatsapp.net
go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/
```

## Hard floor (deterministic ground truth)

| Signal | Target | Observed | Pass |
|---|---|---|---|
| Workers spawned (tool_use) | ≥2 | 2 | true |
| Expected facts in answer | present | true | true |
| Self-mail read-back (MCP) | found | true | true |
| Self-WhatsApp read-back (MCP) | found | true | true |
| Fan-out wall-clock vs single-worker (SC#1) | < 1.5× | 15877/12200 ms | true |
| End-to-end turn incl. parent LLM turns (advisory) | — | 27833 ms | — |

## Judge rubric (≥90% average gate, D-22)

| Dimension | Score /5 |
|---|---|
| autonomous_parallelization | 5 |
| sub_answer_correctness | 5 |
| aggregation_quality | 5 |
| no_over_spawn | 5 |

Swarm-judge mean (autonomous/sub-answer/aggregation): **1.00** (gate ≥0.90) → true

Control over-spawn: false (must be false); no_over_spawn judge: 5/5

## Mounted MCP tools

- mail: mail__fetch_emails, mail__get_thread, mail__search_emails, mail__send_email
- whatsapp: whatsapp__list_chats, whatsapp__list_messages, whatsapp__search_contacts, whatsapp__send_message

## Notes

- workers=2 facts=true timing=true(fanout 15877 / baseline 12200 / e2e 27833 ms) mail=true wa=true judgeMean=1.00
- control over-spawn=false judge=5/5

## Overall verdict: PASS
