# Turn recall — end-to-end on the lab VM, 2026-10-07

Plan 2 of 3 (`docs/superpowers/plans/2026-10-07-turn-recall-2-core.md`, Task 8 Steps 6-9).
Written before the first paid turn, so the candidate that is used cannot be chosen after the fact.

## Frame

- VM: lab VM 192.168.101.158, the operator's own cockpit account, driven through the cockpit API.
- Image: `aura` revision `2882a46bf913dce4bfc0531ce61c98528156cb9c`, started 2026-10-07T10:22:10Z through the
  updater. caddy (10:22:57Z), ingest (10:23:35Z) and cloudflared (10:23:49Z) restarted after it.
- Boot: Postgres migration head 137, not dirty. Zero ArcadeDB version refusals. `ConversationTurn` carries
  `recall_context_key`, `effort`, `effort_requested`, `effort_source`, `effort_route_key`,
  `effort_policy_version` and `effort_origin_ref`.
- Primary route: backend `chatgpt` (the operator's ChatGPT plan), model `gpt-5.6-sol`, base URL
  `https://api.openai.com/v1`. `GET /api/composer/reasoning-capabilities` returns levels
  `auto, low, mid, high, extra, max`, default `auto`, so `off` is not offered.

### Observed before the first turn

Prometheus on the VM, `max_over_time(...[24h])`:

- `aura_agent_teacher_attempt_total` is `invalid` = 1 and `timeout` = 2, with no `success`.
- `aura_agent_turn_decision_total` is `seeds` = 2.

One turn that is not part of this run (thread `01a115e3-…`, 10:22:51Z, 40 s after boot) logged `source=seeds`,
`teacher=invalid`, `teacher_ms=2002`, `seed_ms=5315` and `recall_miss=no_compatible_label`.

The teacher is bounded by `reasoningRouterTimeout()`, which is at most 2 s. It asks for no reasoning with
`max_tokens=32`. This route cannot turn reasoning off, and its measured TTFT is 1.8-2.5 s (memory note
of 2026-10-02). A `teacher_ms` of 2002 labelled `invalid` reads like the deadline cutting the stream
before any text arrived. This is a hypothesis that the turns below test; it is not established yet.

## Candidates (declared before measuring)

Turn A. Each candidate needs a deferred tool and names the chat as the destination:

1. `che temperatura ci sarà sabato mattina a Bra? dimmelo qui in chat`
2. `cerca quanto costa questa settimana l'olio extravergine al litro e scrivimelo qui`
3. `trovami gli orari del museo egizio di domenica e dimmeli qui in chat`

Paraphrase B, in the same order:

1. `sabato mattina a Bra che temperature sono previste? rispondi qui`
2. `quanto viene al litro l'olio extravergine in questi giorni? scrivilo qui`
3. `domenica il museo egizio a che ora apre e chiude? dimmelo qui`

Warm/cold workload (Step 9). Five fresh families, each a cold turn followed by its warm paraphrase, and each
turn in a new conversation. Declared outcome for every turn: a web tool runs and the answer gives the item
asked for, not a refusal or a question back.

- W1: `che film danno stasera al cinema di Cuneo? dimmelo qui in chat` / `stasera al cinema a Cuneo cosa c'è in programmazione? rispondi qui`
- W2: `cerca le ultime notizie sulla fiera del tartufo di Alba e riassumile qui` / `cosa si dice in questi giorni della fiera del tartufo di Alba? riassumilo qui`
- W3: `a che ora apre domani l'ufficio postale di Borgo San Dalmazzo? dimmelo qui` / `domani l'ufficio postale di Borgo San Dalmazzo che orari fa? scrivilo qui`
- W4: `quanto costa un abbonamento mensile alla palestra comunale di Cuneo? cercalo e dimmelo qui` / `la palestra comunale di Cuneo quanto chiede al mese? cerca e rispondi qui`
- W5: `che temperatura c'è adesso sul Monviso? dimmelo qui in chat` / `in questo momento sul Monviso quanti gradi ci sono? rispondi qui`

## Step 8 — end-to-end checks (first pass, image 2882a46bf)

Left out of every measurement: the parallel session's web_fetch turns `01a115e1-…`, `01a115e3-…`,
`01a115e5-2d7d-…` and `01a115e6-b6c2-…` (10:20–10:28Z). These share this identity's memory.

Every turn below has decision fields `route1:ba300909…` and `policy1:4089af05…`. Times come from the
AG-UI stream. `ttft` is the first answer token, which arrives after the tools have run.

| Turn | Conversation | source | seed margin | teacher (ms) | recall_miss | tool turn (distance) | preloaded | tools called | ttft / total s | in / out / cached tokens |
|---|---|---|---|---|---|---|---|---|---|---|
| A1 | 01a115e5-d027… | seeds (low) | 0.0704 | invalid (2001) | no_compatible_label | — | — | tool_search, web_fetch ×2 | 24.4 / 24.4 | 66,336 / 449 / 0 |
| A2 | 01a115e6-9c71… | seeds (low) | 0.0053 | invalid (2001) | no_compatible_label | — | — | tool_search, web_search ×7, web_fetch ×3 (1 error) | 41.8 / 41.8 | 103,186 / 891 / 21,376 |
| A3 | 01a115e7-7cbb… | seeds (low) | 0.0561 | timeout (2001) | no_compatible_label | — | — | tool_search, web_search, web_fetch | 28.1 / 28.1 | 63,328 / 248 / 33,408 |
| B1 | 01a115e8-a53d… | seeds (low) | 0.0866 | not asked | no_compatible_label | `postgres://[REDACTED]` (0.0738) | web_fetch | tool_search (web_search), web_search, web_fetch | 27.3 / 27.4 | 64,767 / 436 / 0 |
| C5a | 01a115e9-c51c… seq 1 | seeds (none → low) | 0.0050 | invalid (2003) | no_compatible_label | — | — | — | 6.0 / 7.3 | — |
| C5b | 01a115e9-c51c… seq 3 | seeds (low) | 0.0866 | not asked | no_compatible_label | — | — | tool_search, web_fetch | 21.9 / 21.9 | — |

Cost: `total_cost_usd` is 0 on every conversation. The ChatGPT plan is a subscription, so this means
"not billed per token", not "free".

1. **Turn A learns: FAIL.** No candidate reached a teacher answer. Each of the three candidates fell below
   the 0.075 margin and asked the teacher, and every attempt ended at the 2 s `reasoningRouterTimeout`:
   two as `invalid` and one as `timeout`, all at 2001 ms. So no label was written. The decision was
   persisted correctly on the user row (`reasoning_effort_source=seeds`, context key, route, policy), and
   the tool ledger shows `tool_search` before the deferred tool.
2. **The turn reaches memory: OK.** By 10:28:36Z all three A user turns were in ArcadeDB with
   `effort_source=seeds`, the context key, and the answer traces listing their tools. A3 finished at
   10:28:02Z, so the lag is at most 34 s; A1's lag was at most 2.5 min (first poll). The decision was there
   on the first poll. The `status` read from the `INVOKED` edge came back null: this query read it from
   the wrong element.
3. **Paraphrase B reuses it: PARTIAL.** There was no label to reuse (see 1), so the source is `seeds`. The
   tool preload worked: `preloaded=[web_fetch]` came from a tool turn at distance 0.0738, and `web_fetch`
   ran without a `tool_search` for it. A1 had loaded `web_search` but never called it, so B1's model still
   ran `tool_search` for `web_search`. The origin is unreadable: `redact.Line` blanks every `postgres://`
   string.
4. **Deleting A forgets it: NOT RUN.** The check compares origins, and the log redacts them (see 3).
5. **A different context does not reuse it: OK.** After a poem turn, the same paraphrase has context key
   `ctx1:43e4e246…` instead of the fresh-conversation key `ctx1:8a8f89cb…`. Nothing was recalled
   (`preloaded=null`, tool-turn distance 0), and the source is `seeds`.
6. Route change and outages: not run (operator-driven).

### Findings from the first pass

- **F1 — the teacher cannot answer on this route.** Of six attempts (four in this run and two from
  Prometheus over 24 h), none succeeded. The teacher gets at most 2 s, asks for no reasoning with
  `max_tokens=32`, and this route cannot turn reasoning off. Its TTFT alone is 1.8–2.5 s. The teacher
  feeds the label pool, so on the ChatGPT plan memory never learns an effort label.
- **F2 — a silent deadline is counted as `invalid`.** Three of the four attempts ended at 2001–2003 ms
  and were labelled `invalid`. The deadline closed the stream with no error and no text.
- **F3 — origins are redacted.** `label_origin` and `tool_turn_origin` log as `postgres://[REDACTED]`.
- **Final review's Critical #1, checked on this VM.** The recall query embeds the model message, which
  carries the knowledge catalog whenever the identity has an indexed document. These turns ran with an
  empty catalog: the logged A1–B1 distance of 0.0738 matches the local plain-text 0.0752, whereas a
  2-document catalog prefix gives 0.4633. On an identity with any indexed document, memory would never
  match.

## Step 8 — second pass (image d633f8b9d, background teacher)

Code under test, pushed as `1337e50ba..d633f8b9d`:
- the final-review fixes: typed text for the reading; Telegram typed/composed; teacher deadline counted as `timeout`; origins logged verbatim;
- the background teacher (spec amendment 2026-10-07, plan Task 9).

The new `selectionRules` give a new policy version, so no label written before this image is compatible.

Frame:
- `aura` revision `d633f8b9d53a02a90366298104ef8bf1394e1bb9` started at 12:47:13Z through the updater, with caddy restarting after it.
- Postgres head is 137 and not dirty. There were no ArcadeDB refusals and no ERROR lines at boot.
- CI run 37622128275 on that SHA was still running when this pass began. Its result is recorded under Step 4.
- The first pass's five conversations (A1, A2, A3, B1, C5) were deleted through the cockpit at 12:48:30Z (HTTP 204 ×5) before any turn of this pass. Their ArcadeDB rows still had `deleted_at = null` 35 s later. The delete reconciliation lag is recorded below.
- Delete reconciliation lag: by 12:49:23Z, 12 live rows remained; by 12:49:38Z, none. So the delete was reflected in recall within 68 s.
- CI run 37622128275 on `d633f8b9d`: success, 35 of 35 jobs. That includes the coverage gate, Agent Memory MRS, db_integration, and the three Go mutation groups. `turn_reading` killed 100% of mutants, measured in 907 s on the shipped file.

Fields in every row: route `route1:a9d52cfa…` (efforts are now sorted, so this key differs from the first pass) and policy `policy1:043e1c68…`.

| Turn | Conversation | source | seed margin | ask_teacher | label origin (Postgres) / distance | tool turn distance | preloaded | tools called | ttft / total s |
|---|---|---|---|---|---|---|---|---|---|
| A1b | 01a11669-cc8b… | seeds | 0.0704 | true | — | — | — | tool_search, web_fetch, then **RUN_ERROR** | — / 26.5 |
| A1c | 01a1166a-a17b… | seeds | 0.0704 | true | — | — | — | tool_search, web_fetch | 24.6 / 24.6 |
| B1b | 01a1166c-6319… | **memory** | — | false | `…/01a1166a-a17b…/turns/1` (A1c) / 0.0738 | 0.0738 | web_fetch | tool_search (web_search), web_fetch | 24.8 / 24.8 |
| C4 | 01a1166f-3127… | seeds | 0.0866 | false | — | 0 (B1b, same text) | web_fetch | tool_search (web_search), web_fetch | 28.1 / 28.2 |
| A2b | 01a1166f-eaa9… | seeds | 0.0053 | true | — | — | — | tool_search, web_search ×3, web_fetch ×4 | 49.7 / 49.8 |
| C5a2 | 01a11671-d663… seq 1 | seeds (none → low) | 0.0050 | true | — | — | — | — | 8.3 / 9.6 |
| C5b2 | 01a11671-d663… seq 3 | seeds | 0.0386 | true | — | — | — | tool_search, web_search ×3 | 40.8 / 40.8 |

Background teacher, as logged on the `adaptive reasoning: teacher label` line:

| Turn | outcome | tier / requested / effort | teacher_ms | logged | Postgres row | ArcadeDB `ConversationTurn` |
|---|---|---|---|---|---|---|
| A1c | success | low / low / low | 6774 | 12:51:20Z | `teacher` | `teacher` by 12:51:35Z (first poll) |
| A2b | success | low / low / low | 8310 | 12:57:33Z | `teacher` | `teacher` at 12:58:28Z: the row had not been projected yet, and it arrived already labelled, at most 55 s after the label |

1. **Turn A learns: PASS.** A1c was decided by `seeds` without waiting, and its turn ended in 24.6 s. About 30 s after the turn ended, the background teacher answered in 6.8 s, and the row became a `teacher` label in Postgres and then in ArcadeDB. Two attempts out of two succeeded, at 6.8 s and 8.3 s, against the old synchronous budget of 2 s.
   - A1b was lost to the provider. OpenAI `service_unavailable_error` / `server_is_overloaded` arrived mid-stream, the round failed, and it recorded no decision. As designed, no teacher ran.
2. **The label reaches memory: PASS.** The label was in ArcadeDB 15 s (A1c) or 55 s (A2b) after it was logged. For A2b the whole turn row first appeared already labelled.
3. **Paraphrase B reuses it: PASS.** B1b was decided by `memory`. Its persisted origin is A1c's user turn, at distance 0.0738, which is within 0.10. `web_fetch` was preloaded and called with no `tool_search` for it. The model still ran `tool_search` for `web_search`, a tool A1c had loaded but never called.
4. **Deleting A forgets it: PASS.** A1c was deleted at 12:54:33Z and left recall within 65 s. The same paraphrase (C4) then found no label: `label_origin` was empty and the decision came from `seeds`, with margin 0.0866 above the threshold. Its preload came from B1b at distance 0, not from A1c. B1b's `memory` decision is not itself a label, as specified.
5. **A different context does not reuse it: PASS.** A live teacher label existed (A2b). In a conversation that opened with a poem, the oil paraphrase had a different context key (`ctx1:2842131a…`), recalled nothing (`preloaded=null`, no label), and was decided by `seeds`.
6. Route change and outages: not run, because they are operator-driven.

Findings in this pass:
- **F4: an interrupted round does not say why.** The provider error above surfaced in the cockpit only as "[run interrupted before it produced an answer; …]". The operator noted: "qui non si capisce il perchè".
- **F5: origins are still redacted in production.** The production slog `ReplaceAttr` runs `redact.String` over every string attribute, and the earlier fix was tested with a plain handler. The Postgres `reasoning_effort_origin_ref` column was readable throughout, so it supplied the origins above.

### Check 6 — route change, on ollama / gemma4:31b-cloud (the operator switched the cockpit at 13:06:43Z)

| Turn | Conversation | route | source | label origin / distance | preloaded | tools called | total s |
|---|---|---|---|---|---|---|---|
| R6 (oil paraphrase) | 01a11679-9dd2… | `route1:cc40cd89…` | seeds (ask_teacher) | — (A2b's gpt label not reused) | — | tool_search, web_search | 10.1 |
| R6b (A2b's exact text) | 01a1167a-4bdf… | ollama | seeds (ask_teacher) | — (A2b at distance 0 is on another route; R6 is more than 0.10 away) | web_fetch, web_search (A2b's tool turn) | web_search, with **no tool_search** | 9.1 |
| OA1 (weather) | 01a1167a-cd00… | ollama | seeds (ask_teacher) | — | web_fetch | tool_search, web_search ×2 | 12.8 |
| OB1 (weather paraphrase) | 01a1167b-f3ed… | ollama | **memory** | OA1 / 0.0738 | web_fetch | tool_search (web_search), web_search ×2 | 14.4 |

- **The change of route invalidates labels: PASS.** A label learned on gpt was never reused on ollama, including the exact-text match at distance 0.
- **The background teacher learns on a second provider: PASS.** R6, OA1, W1, W3, W4 and W5 were all labelled. R6's teacher took 365 ms.
- **The tool pool is not filtered by route.** Tools do not depend on the model, so A2b's tool turn from gpt preloaded `web_search` for gemma.
- **The preload follows the recalled turn's tools, not the current model's habits.** OB1 was handed `web_fetch` from a gpt-era turn, but gemma used `web_search`, so it still ran `tool_search`. This is a carryover for plan 3, "intelligent tools".

## Step 9 — what memory saves (ollama / gemma4:31b-cloud)

Five cold/warm pairs, each turn in a new conversation, with the cold turn's label or projection awaited before its warm paraphrase. An earlier attempt on gpt-5.6-sol lost W1-cold to `server_is_overloaded` and was stopped before the operator switched routes. Its row is left out.

| Turn | source | ask_teacher | label origin / distance | preloaded | tools called | tool_search | ttft s | total s | in / out / cached tokens | declared outcome |
|---|---|---|---|---|---|---|---|---|---|---|
| W1-cold | seeds | true | — | — | tool_search, web_search | 1 | 6.7 | 7.6 | 55,492 / 258 / 25,280 | met (films listed) |
| W1-warm | **memory** | false | W1-cold / 0.0997 | web_search | web_search | **0** | 8.8 | 11.0 | 36,709 / 822 / 12,864 | met |
| W2-cold | seeds (margin ≥ 0.075) | false | — | — | tool_search, web_search | 1 | 9.8 | 11.9 | 54,429 / 863 / 38,464 | met (news summarised) |
| W2-warm | seeds | true | — (no label: the cold turn was confident) | — | tool_search, web_search | 1 | 4.2 | 6.2 | 54,218 / 496 / 25,280 | met |
| W3-cold | seeds | true | — | — | tool_search, web_search | 1 | 6.8 | 7.1 | 55,163 / 719 / 39,680 | met (08:20–13:35) |
| W3-warm | **memory** | false | W3-cold / 0.0367 | web_search | web_search | **0** | 4.4 | 4.6 | 36,813 / 492 / 23,872 | met |
| W4-cold | seeds | true | — | — | tool_search, web_search, web_fetch, web_search, web_fetch | 1 | 24.1 | 25.8 | 137,165 / 3,424 / 104,288 | **not met** (no single price found) |
| W4-warm | **memory** | false | W4-cold / 0.0461 | web_fetch, web_search | web_search, web_fetch, web_search ×2, skill, tool_search, browser open/snapshot/click, shell_exec, web_search, web_fetch | 1 | 78.4 | **80.8** | 361,928 / 8,498 / 275,584 | not met (same conclusion) |
| W5-cold | seeds | true | — | — | tool_search, web_search, tool_search, web_fetch | 2 | 13.9 | 14.4 | 103,945 / 1,356 / 69,920 | met (−4.1 °C) |
| W5-warm | seeds | true | — (the paraphrase is more than 0.10 from W5-cold) | — | tool_search, web_search, tool_search, web_fetch | 2 | 14.6 | 15.1 | 103,952 / 1,682 / 69,920 | met |

- **Decisions.** Memory decided 3 of the 5 warm turns: W1, W3 and W4. W2 had no label to reuse, because its cold turn was confident and the teacher was not asked. W5's paraphrase fell outside the 0.10 radius.
- **Preload.** In W1 and W3 the preload removed `tool_search` entirely. Across all turns, `tool_search` was called 6 times on the cold side and 4 times on the warm side.
- **Latency.** The p50 total is 11.9 s cold and 11.0 s warm. With five pairs, the slowest turn stands in for p95: **25.8 s cold against 80.8 s warm.**
- **Tokens.** Input plus output was 412,814 cold and 605,610 warm. Without W4 it is 272,225 cold and 235,184 warm, which is −13.6%.
- **Cost is unknown.** `total_cost_usd` is 0 for this route, which means it is not billed per token here. It does not mean the turns were free.
- **Outcomes.** No warm turn missed an outcome its cold pair met. W4 missed on both sides: no public monthly price was found.
- **Workload gate: FAIL**, on the slowest completion and on total tokens, because of pair W4. Both W4 turns ran at effort `low`. The warm turn's extra work came after its preloaded tools, when it opened a skill, the browser and a shell on a question that has no published answer. One sample cannot attribute this to memory or to the model's run-to-run variance, and this run does not claim either.

### W4 repeated three times, each from a clean slate (operator's request)

Before each pair, the previous W4 conversations were deleted through the cockpit, and the run waited until recall no longer returned them. So every cold turn was cold.

Around r2, the cockpit's sign-in rate limit (30 per window) refused the driver's per-turn logins. The driver was changed to sign in once and reuse the session cookie, and the missing runs then completed. No turn was lost to Aura.

| Pair | Cold total s | Cold tools | Warm total s | Warm source / distance | Warm preloaded | Warm tool_search | Tokens in+out (cold → warm) |
|---|---|---|---|---|---|---|---|
| original | 25.8 | 5 | 80.8 | memory / 0.0461 | web_fetch, web_search | 1 | 140,589 → 370,426 |
| r1 | 19.9 | 4 | 22.7 | memory / 0.0461 | web_fetch, web_search | 0 | 106,601 → 64,442 |
| r2 | 46.6 | 6 | 11.4 | memory / 0.0461 | web_fetch, web_search | 0 | 147,380 → 62,906 |
| r3 | 15.1 | 3 | 23.7 | memory / 0.0461 | web_fetch, web_search | 0 | 81,545 → 132,238 |

- **The 80.8 s turn did not reproduce.** Across r1–r3 the slowest warm turn was 23.7 s and the slowest cold turn was 46.6 s.
- **Warm turns used 23% fewer tokens in total:** 335,526 cold against 259,586 warm.
- **Single pairs still went either way.** r1's warm turn was slower than its cold turn, and r3's warm turn was both slower and costlier.
- **The answers did not change.** Every cold and warm answer concluded that no single public monthly price exists.
- **What this does not show:** four samples of one question do not tell model variance apart from memory's effect. They show that this pair's regression is not systematic, and nothing more.
