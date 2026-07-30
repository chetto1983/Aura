# Aura Direct Production Tool Audit

Date: 2026-07-30  
Runtime revision: `b303e125a8d8382ea2146ae090d500d96b4523bb`  
Image: `sha256:6474c3d02048323b2f637ecd7f41f5b825f260ba08859a18f96b3b62dbf827f4`  
Method: direct calls through `aura toolpipe`; no Cockpit, LLM routing, mocks, or
package-test substitution.

## Verdict

**Aura runtime implementation: PASS, 9.8/10.** The deployed image is healthy,
all 58 production-registry tools are directly callable, the corrected core
lifecycles work, and observed domain failures are no longer reported as success.

**Fully configured external-delivery certification: CONDITIONAL.** Calendar has
no configured account and WhatsApp is healthy but waiting for QR pairing. Those
28 tools were directly invoked and failed honestly where a provider/device was
required, but real event/email/message delivery cannot be certified without an
authorized test account and paired device.

| Registry group | Tools | Directly called | Live/positive evidence | Honest guard or environment gate |
|---|---:|---:|---:|---:|
| Aura native/conditional | 22 | 22 | 19 | 3 contextual |
| Memory MCP | 8 | 8 | 8 | 0 |
| Calendar MCP | 14 | 14 | 1 | 13 no-account |
| WhatsApp MCP | 14 | 14 | 7 local reads | 7 unpaired/invalid fixture |
| **Total** | **58** | **58** | **35** | **23** |

“Positive” means a semantically useful result or a completed reversible
lifecycle. A guard result is not counted as proof of external provider success.

## Score

This score measures the Aura implementation and deployed runtime, not whether
the operator has connected optional external accounts.

| Dimension | Weight | Score | Production evidence |
|---|---:|---:|---|
| Functional correctness | 40% | 9.7 | 58/58 routed; native and Memory lifecycles complete; external configuration is the remaining gate |
| Failure honesty | 20% | 10.0 | `success:false`, top-level `error`, `deleted:null + reason`, and missing execution context all become typed failures |
| Performance | 20% | 9.8 | warm document p95 559 ms; local tools 0–7 ms; Calendar 168 ms total; WhatsApp 192 ms total |
| Operability | 10% | 10.0 | one production-composed direct runner; doctor and registry both see 3/3 MCP servers and 58 tools |
| Static/concurrency quality | 10% | 9.2 | focused tests, build, vet, lint, and pre-commit hooks pass; full repository suite retains two unrelated known Windows/service-composition failures |
| **Weighted implementation score** | **100%** | **9.76 → 9.8** | **PASS above 9.5 target** |

## Production performance

| Path | Result | Measured latency |
|---|---|---:|
| Native filesystem/text/task/skill operations | Correct result/lifecycle | 0–7 ms |
| Tool discovery | Correct top result for Calendar, Web, and exact Memory queries | 0–5 ms |
| Web search | Relevant SearXNG result | 776 ms |
| Web fetch, Wikipedia | 25,629 bytes; complete; not truncated | 229 ms |
| Web fetch, B&R English product page | Full motor technical table, 3,464 bytes | 413 ms |
| Web fetch, B&R Italian product page | Full motor technical table, 2,740 bytes | 3,395 ms |
| Web fetch timeout guard | 20-second endpoint rejected at configured budget | 10,004 ms |
| Document search cold | Correct cited Constitution result | 4,071 ms |
| Document search warm, n=20 | min 472; p50 497; p95 559; max 671 ms | p95 559 ms |
| Memory semantic operations | warm 93–680 ms; cold entity 2,292 ms | operation-dependent |
| Calendar, 14 calls | total 168; max 81 ms | avg 12.0 ms |
| WhatsApp, 14 calls | total 192; max 65 ms | avg 13.7 ms |

The document result was correct on 21/21 direct queries. Warm p95 improved from
4,346 ms to 559 ms, approximately 7.8×.

## Native and conditional tools — 22/22

| Tool | Verdict | Direct production result | Latency |
|---|---|---|---:|
| `ask_user` | Context pass | Typed `awaiting user input` pause | 0 ms |
| `current_time` | Pass | Valid current RFC-3339 time | 0 ms |
| `document_index` | Pass | Indexed one real workspace file; searchable job completed | 2,514 ms |
| `document_search` | Pass | Exact content with citation; 21/21 correct | cold 4,071 ms; warm p95 559 ms |
| `fs_edit` | Pass | Exact replacement persisted | 0 ms |
| `fs_glob` | Pass | Located exact fixture | 0 ms |
| `fs_grep` | Pass | Returned exact content match | 0 ms |
| `fs_read` | Pass | Returned exact file content | 0 ms |
| `fs_write` | Pass | Created exact fixture | 0 ms |
| `read_tool_output` | Pass | Read offset 30,000/limit 1,000 from a spilled 40 KB result | 0 ms |
| `send_file` | Context pass | Correctly queued; channel delivery was not authorized/proven | 0 ms |
| `shell_exec` | Pass | Foreground, background, and 40 KB spill paths worked | 1–23 ms |
| `shell_kill` | Pass | Terminated the real background process | 0 ms |
| `shell_poll` | Pass | Returned marker output and running state | 0 ms |
| `skill` | Pass | Create → immediate info → delete in one runtime lifecycle | 0–4 ms |
| `swarm_spawn` | Context pass | Typed `swarm context unavailable`; no false OK | 0 ms |
| `task` | Pass | Schedule → list → cancel → absence verified | 1–7 ms |
| `text_response` | Pass | Returned exact audit marker | 0 ms |
| `todo_write` | Pass | Stored and returned supplied state | 0 ms |
| `tool_search` | Pass | Correct Calendar/Web/Memory tools ranked first | 0–5 ms |
| `web_fetch` | Pass | Complete bounded content plus deterministic 10 s timeout | 229–3,395 ms live pages |
| `web_search` | Pass | Relevant official result | 776 ms |

## Memory tools — 8/8

The direct lifecycle created two distinct entities, a fact, a preference, and
two relationships; updated and retrieved them; found them through search; then
deleted every fixture and verified both entities absent.

| Tool | Verdict | Direct production result | Latency |
|---|---|---|---:|
| `memory__memory_add_entity` | Pass | Exact entity created/merged; no semantic-neighbor substitution | 93 ms warm; 2,292 ms cold |
| `memory__memory_add_fact` | Pass | Fact created and retrievable | within 93–680 ms warm range |
| `memory__memory_add_preference` | Pass | Preference created and retrievable | within 93–680 ms warm range |
| `memory__memory_create_relationship` | Pass | Distinct and self relationships created with live schema | 517 ms warm; 1,521 ms cold |
| `memory__memory_get_entity` | Pass | Correct entity/neighbors; nonexistent name returns `found:false` | 152–1,243 ms |
| `memory__memory_search` | Pass | Temporary records found | 441 ms |
| `memory__memory_update` | Pass | Exact entity updated | 93 ms |
| `memory__memory_forget` | Pass | Real deletions OK; malformed/no-op deletions are typed errors | 14–76 ms |

The last false-success shape found during cleanup,
`{"deleted":null,"reason":"..."}`, is fixed in the deployed image. The
malformed call now returned `status:error` in 4 ms, while the two valid
relationship deletions returned `status:ok` in 19 and 16 ms.

## Calendar tools — 14/14 directly called

`aura mcp doctor calendar` reports 14 mounted tools. The account inventory is
genuinely empty. No email was sent and no event was created or changed.

| Tool | Result | Latency |
|---|---|---:|
| `calendar__list_accounts` | Pass: `accounts: []` | 81 ms |
| `calendar__get_calendar_events` | Honest gate: no accounts | 54 ms |
| `calendar__get_contacts` | Honest gate: no accounts | 3 ms |
| `calendar__get_emails` | Honest gate: no accounts | 3 ms |
| `calendar__list_calendars` | Honest gate: no accounts | 2 ms |
| `calendar__search_contacts` | Honest gate: no accounts | 2 ms |
| `calendar__search_emails` | Honest gate: no accounts | 2 ms |
| `calendar__get_calendar_event_details` | Honest error: audit account absent | 3 ms |
| `calendar__get_contact_details` | Honest error: audit account absent | 2 ms |
| `calendar__get_email_details` | Honest error: audit account absent | 2 ms |
| `calendar__create_event` | Failed closed on nonexistent audit account | 3 ms |
| `calendar__respond_to_event` | Failed closed on nonexistent audit account | 2 ms |
| `calendar__send_email` | Failed closed on nonexistent audit account | 2 ms |
| `calendar__update_event` | Failed closed on nonexistent audit account | 2 ms |

## WhatsApp tools — 14/14 directly called

`aura mcp doctor whatsapp` reports 14 mounted tools and a reachable bridge:
`state=waiting_qr`, `paired=false`, `qr_available=true`. Queries used unique,
nonmatching audit values. Mutation probes used invalid recipients or nonexistent
files, so no external message or media could be delivered.

| Tool | Result | Latency |
|---|---|---:|
| `whatsapp__list_chats` | Pass: empty filtered result | 65 ms |
| `whatsapp__list_messages` | Pass: empty filtered result | 6 ms |
| `whatsapp__search_contacts` | Pass: empty filtered result | 11 ms |
| `whatsapp__get_contact` | Pass: `resolved:false` | 9 ms |
| `whatsapp__get_chat` | Honest not-found error; sidecar message is overly technical | 9 ms |
| `whatsapp__get_contact_chats` | Pass: no matching chats | 5 ms |
| `whatsapp__get_direct_chat_by_contact` | Honest not-found error; sidecar message is overly technical | 2 ms |
| `whatsapp__get_last_interaction` | Pass: `{}` | 4 ms |
| `whatsapp__get_message_context` | Honest message-not-found error | 3 ms |
| `whatsapp__download_media` | Honest `success:false` error; previous false OK fixed | 50 ms |
| `whatsapp__send_audio_message` | Honest missing-file error | 8 ms |
| `whatsapp__send_file` | Honest missing-file error | 4 ms |
| `whatsapp__send_message` | Honest unpaired-bridge error | 6 ms |
| `whatsapp__send_reaction` | Honest missing-device-JID error | 6 ms |

## Corrections now in production

1. The direct runner composes the real production registry, stores, identity,
   MCP transports, routing, and all 58 tools.
2. Doctor probes the same resolved MCP configuration as the runtime.
3. Memory relationship arguments match the mounted server schema; semantic
   nearest neighbors are not accepted as exact endpoint identity.
4. MCP domain failures are rejected for `success:false`, nonempty top-level
   `error`, and `deleted:null` with a failure reason.
5. Missing swarm context is a typed error.
6. Skills invalidate the live loader after mutation; task direct runs use a
   valid reserved conversation UUID.
7. Tool search remains useful without an embedder through deterministic BM25.
8. Document graph sessions, candidate windows, rerank reuse, and degraded
   rerank handling reduced warm retrieval p95 to 559 ms.
9. Memory retrieval buckets run concurrently and repeated provider embeddings
   are coalesced/cached.
10. Web fetch now has a 10-second budget, preserves complete bounded direct
    output, and uses a browser-compatible default User-Agent. The actual B&R
    pages that timed out now return full technical data.

## Remaining gates and quality notes

- Calendar provider success needs an authorized Microsoft/Google test account.
- WhatsApp delivery/media success needs the operator to scan the available QR
  with an authorized test device.
- Native `send_file` still needs a real channel delivery receipt for full E2E.
- The WhatsApp sidecar returns technical Pydantic `DictModel` text for two
  not-found reads. Failure status is correct; message quality should be improved
  in the sidecar.
- `web_fetch` intentionally rejects PDF content; a PDF URL must use the document
  ingestion path. This is a capability boundary, not a timeout.

## Cleanup

- Deleted both temporary Memory relationships, both entities, the fact, and the
  preference; entity lookups returned `found:false`.
- Deleted the indexed audit document and chunk from Neo4j, deleted its exact
  Postgres ingestion job, removed the exact workspace file, and verified all
  three absent.
- Deleted the temporary skill; cancelled the temporary scheduled task;
  terminated the background shell process.
- No Calendar event/email or WhatsApp message/reaction/media was delivered.

## Final external certification procedure

After the operator connects one Calendar account and pairs WhatsApp, rerun a
reversible event create/read/update/delete lifecycle, send one authorized test
email, and perform message/reaction/file/audio/media-download round trips to a
designated test contact. Until then the implementation score is 9.8, while
external-delivery certification remains conditional rather than falsely passed.
