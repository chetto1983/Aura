# QA Bug Log

Append-only log of QA baseline failures, triage, and resolution.

---

## 2026-05-18T11:30 — agent-note-roundtrip — run-1 — P1
**Git HEAD**: 3652fdf2
**Repro**: `go run ./cmd/probe_chat -case=agent-note-roundtrip` (from D:/Aura repo root with AURA_CHAT_TOKEN set)
**Flakiness**: 4/4-fail (single run; deterministic)
**Symptoms**: DB: agent_notes row missing after set (conversation_id='chat-cli')
**Ground-truth investigation**:
  - `agent_notes` table is empty (0 rows) — no row keyed by `'chat-cli'` and none by any other key from this probe run.
  - `conversations` table shows the probe authenticates as `chat_id=1148481707` (the bearer's user_id), not anonymous.
  - In `internal/api/chat.go:88-92`, `userID = "chat-cli"` is only the fallback for unauthenticated/empty `req.UserID`; authenticated requests use the bearer's resolved user_id.
  - In `cmd/aura/web_chat.go:401`, the web tool executor sets `toolCtx = toolregistry.WithConversationID(toolCtx, e.userID)` — i.e. conversation_id is bound to the **authenticated user_id**, so for this probe it would be `"1148481707"`, NOT `"chat-cli"`.
**Root cause**: Test-vs-production key mismatch. The web `/api/chat` path keys agent_notes by the authenticated user_id (`e.userID`), but the probe's Verify hard-codes `conversation_id='chat-cli'`, a value only reachable on anonymous calls. The tool wiring is fine; the probe asserts against the wrong key. (Secondary observation: the current wiring conflates user_id with conversation_id — there is only one scratchpad per user, not per conversation — which contradicts the tool's per-conversation contract. That is a deeper design smell to file separately; it doesn't change this case's triage.)
**Fix**: `cmd/probe_chat/cases.go:746` — replace the hard-coded `'chat-cli'` with the probe's actual authenticated user_id. Diff sketch:
```go
- dbErr := env.DB.QueryRow(
-     `SELECT content FROM agent_notes WHERE conversation_id = 'chat-cli'`,
- ).Scan(&noteContent)
+ dbErr := env.DB.QueryRow(
+     `SELECT content FROM agent_notes WHERE conversation_id = ?`,
+     env.UserID, // or the strconv.FormatInt of the authenticated telegram user_id used by the probe
+ ).Scan(&noteContent)
```
Apply the same substitution to the turn-2/turn-3 lookups further down in the same Verify block. If `env.UserID` is not already exposed on `Env`, plumb it from the token-mint step (the probe already knows the user_id it minted the bearer for: `1148481707`).
**Status**: fixed in cases.go (commit pending). The orchestrator chose an operator-agnostic fix using an `updated_at >= unixepoch('now', '-2 minutes')` window instead of hardcoding the user_id, so the probe works for any bearer's user_id. Re-run 2026-05-18T11:36 → **PASS** in 10399ms (1 tool call, 2 LLM calls). Stderr preview confirmed: `conversation_id="1148481707" content="TODO: verifica X, verifica Y, verifica Z"`.

**Follow-up filed (P2 backlog, next QA run as US-QA21)**: `cmd/aura/web_chat.go:401` collapses per-conversation scratchpad to one-note-per-user by binding conversation_id to user_id. Tool docstring promises per-conversation scoping; web wiring violates that. Design smell — file separately.

---

## 2026-05-18T11:43 — web-fetch-summarize-context-engineering — run-1 — NOISE
**Git HEAD**: 3652fdf2
**Repro**: `go run ./cmd/probe_chat -case=web-fetch-summarize-context-engineering` (D:/Aura repo root, AURA_CHAT_TOKEN set)
**Flakiness**: 1/4 fail (originale post-fix FAIL + 3/3 reruns PASS) → severity `NOISE`
**Symptoms**: tool_calls=0 (atteso ≥1 web_fetch); reply mancante summary keywords; "likely phantom"
**Ground-truth investigation**: 3 reruns identici hanno tutti tools=1, llm=2, completion 22-26s — il modello in 3 occasioni su 3 ha chiamato il tool correttamente. La probe FAIL nel run originario era una scelta del modello di NON chiamare il tool (LLM non-determinism, possibilmente per drift dello stato dei tool sets del contesto precedente).
**Root cause**: LLM stochasticity (occasional decision to skip a tool call when the prompt is borderline-clear). Non un bug del codice.
**Fix**: None. Monitor in successive runs; se pattern persiste (≥2/4 across multiple sessions) promote to FLAKE and investigate prompt clarity or phantom-guard.
**Status**: monitoring (NOISE classification, no fix per skill rule "1/4 fail → mark for monitoring")
**Determinism notes**: investigate whether `web` tool prompt overlay in TOOLS.md is unambiguous about when web_fetch must fire vs may use general knowledge. Possibly model self-decided summary was sufficient without fetch.

## 2026-05-18T11:43 — doc-pdf-roundtrip — run-1 — FLAKE
**Git HEAD**: 3652fdf2
**Repro**: `go run ./cmd/probe_chat -case=doc-pdf-roundtrip` (D:/Aura repo root, AURA_CHAT_TOKEN set)
**Flakiness**: 2/4 fail (originale post-fix FAIL + 1/3 rerun FAIL + 2/3 reruns PASS) → severity `FLAKE`
**Symptoms**: model mis-typed source_id in reply — "reply quotes 'src_89ceda2013a00d59' but actual source is 'src_89ceda2013d00a59'" (one digit off: a vs d). Second rerun: "src_e44489bc819ffaa3 vs src_ca249a97bdbdbec4" (totally different — wholesale hallucination).
**Ground-truth investigation**: The PDF was created and stored correctly each run (artifact persisted, source_id valid in DB). The failure is exclusively in the reply's quotation of the source_id — the LLM mis-copies the hex string when echoing it back.
**Root cause**: LLM hex-string quotation drift. The model can OCR-correctly handle the workflow but cannot reliably echo an arbitrary 16-char hex back. NOT a product bug — the underlying tool + storage works correctly.
**Fix**: None per skill rule "2/4 fail → genuine flake. Do NOT propose a fix yet. Investigate determinism next pass."
**Status**: flake-monitoring (FLAKE classification)
**Determinism notes**: investigate (a) prompting the model to copy-paste source_id via a delimited copy-block, (b) relaxing the assertion to substring-similarity rather than exact match, (c) adding a probe instruction "echo source_id verbatim, no transformation, no rephrasing". Likely the right fix is (c) — make the prompt require verbatim echo with a clear example.
