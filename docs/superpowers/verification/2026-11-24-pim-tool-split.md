# PIM tool split: verification ledger

Plan: `docs/superpowers/plans/2026-11-23-pim-tool-split.md`. Spec:
`docs/superpowers/specs/2026-11-23-pim-tool-split-design.md`. Lab VM `192.168.101.158`. Each part of
the plan appends its section here.

## Part A: the PIM pinned by sha (2026-11-24)

Release `713e5e169`, carrying `45bdb11ff`..`713e5e169`. Applied through the updater on the operator's
"Aggiorna subito" at 08:20 UTC.

| Check | Result |
|---|---|
| Aura revision after the apply | `713e5e169` |
| updater log | `env: retired AURA_PIM_MCP_IMAGE removed from .env.` |
| `AURA_PIM_MCP_IMAGE=` lines in the appliance `.env` | 0 (was 1) |
| PIM container image reference | `ghcr.io/chetto1983/aura-pim-mcp:43c49b31c11eebfc041cdd5031cdc3cd45021f3a` (was `:sidecar`) |
| PIM image digest, before and after | unchanged (`sha256:4bc5…9054`): the pin is the revision that already ran |
| PIM recreated by the updater's sidecar step | yes, 08:21–08:22 UTC, with no manual step |
| `list_accounts` through the mounted PIM after the recreate | 3 accounts, same as before |

What this does not show:
- That a push to the fork's `main` no longer reaches the VM. Task B5 records that, after the fork's
  first push.
- A fresh install. `install_env.sh` no longer writes the key; that is unit-tested
  (`TestRetiredEnvKeysStayRetired`), not run on a clean host.

## Part B: the split fork published, the VM unmoved (2026-11-24)

Fork `main` pushed `43c49b3..8fbd799`, carrying the 29 per-operation tools.

| Check | Result |
|---|---|
| Fork CI Build on `8fbd799` | green |
| `aura-publish-image` on `8fbd799` | green, finished 10:40 UTC; `:8fbd79960eee2fbb2970a5a725d5759fb44ccfdb` published |
| `TestCalendarServerLive` against the published image | fails as expected: the sidecar advertises 29 tools and the test still wants the single `calendar` tool (Task C8 updates it) |
| VM updater ticks after the publish | yes, 10:42 UTC |
| VM PIM after that tick | still `ghcr.io/chetto1983/aura-pim-mcp:43c49b31c11eebfc041cdd5031cdc3cd45021f3a`, digest `sha256:4bc5…9054`, started 08:22 UTC |

A push to the fork's `main` no longer reaches the VM: the pin from Part A holds it.

What this does not show:
- A `tools/call` on each of the 29 tools against the published image. Task C8 runs the live tier
  against it.
- That Aura reads the new schemas (nullable optional types, per-tool required lists). That is Part C.

## Part C: search gate (C7) (2026-11-24)

**Corpus.** `testdata/deferred_manifest.json` comes from `aura toolpipe --manifest-json`, run on master
`c3b41f382` (C1–C6, local, not pushed). It ran against a disposable database with the PIM image
`:8fbd79960eee2fbb2970a5a725d5759fb44ccfdb` (digest `sha256:f595731d…`) mounted
(`mcp live mounted … tools=29`).

From that dump, verbatim:
- the 29 `calendar__*` tools, in place of `calendar__calendar`;
- the 13 deferred built-ins:
  - `board`, `image_generate`, `mail_watch`, `swarm_status` and `video_generate` are added;
  - `shell_kill` and `shell_poll` are dropped, because they have been always-active since `ebcb94f9a`;
  - the text of `plugin_pack`, `skill_manage`, `swarm_spawn`, `task` and `web_fetch` is refreshed.

The Linear (55), memory (4) and WhatsApp (15) entries are the old corpus's: the stack did not mount
them. Total: 85 entries before, 116 after.

**Keywords.** The 29 PIM tools have `retrievalKeywords`, in English and Italian. They were written
after the blind-eval PIM misses below were seen, so that subset is spent. The held-out set is the
evidence that the keywords generalise. It was committed in `eb393da5c` before the keywords existed,
and read only after `search_keywords.go` was frozen (`sha256 60e304f6…`). After the set was read,
the only change to that file was a `gofmt` re-alignment, whitespace only (`diff -w` empty).

### Blind eval, PIM queries (5)

Rank is the gold tool's place in the full BM25 ranking. "Unranked" means the gold tool scored zero.

| Corpus | In the top 5 | Worst rank |
|---|---|---|
| before the split, gold `calendar__calendar` | 2/5 | unranked (3) |
| after the split, no PIM keywords | 2/5 | unranked (3) |
| after the split, with PIM keywords | **5/5** | 4 |

### Held-out set (`pim_heldout_2026-11-24.json`, 52 cases)

This is the controller's measurement: raw BM25 through `rankedNames`, counting a case when a gold
tool is in the top 5.

| Corpus | All (52) | Italian (27) | English (25) | Unranked |
|---|---|---|---|---|
| before the split, gold `calendar__calendar` (any PIM hit) | 9 | 6 | 3 | 41 |
| split, no PIM keywords | 5 | 0 | 5 | 41 |
| split + PIM keywords | 32 | 21 | 11 | 15 |

**Operator decision, 2026-11-24.** The held-out gate is a no-regression floor:
`TestHeldOutPIMQueriesKeepTheirFloor` requires 32 in total, 21 Italian and 11 English, through
`gateSearch`. The floor is a ratchet and is never lowered. Retrieval of paraphrases is a separate
job, after the push. The blind-eval gate (`TestEveryPIMQueryFindsItsToolInTopFive`) stays strict at
5/5.

C7's own count through the gate path agrees: 32/52, 21 Italian, 11 English. Raw BM25 leaves 15 of
the cases unranked. An earlier draft of this section said 12; that was a miscount of the table
below, which lists 15 unranked rows.

The 20 misses after the keywords, by the case's position in the file:

| # | Lang | Gold | Rank |
|---|---|---|---|
| 3 | it | search_emails / get_emails | unranked |
| 4 | en | search_emails / get_emails | unranked |
| 12 | en | get_contextual_email_summary / search_emails | 16 |
| 14 | en | send_email / get_email_details | 9 |
| 15 | it | send_email | unranked |
| 16 | en | send_email | unranked |
| 18 | en | send_email | 7 |
| 19 | it | delete_email | unranked |
| 26 | en | get_unsubscribe_info / unsubscribe_from_email | unranked |
| 28 | en | get_email_attachment / search_emails | unranked |
| 29 | it | get_email_attachment / get_email_details | 12 |
| 30 | it | get_calendar_events | unranked |
| 31 | en | get_calendar_events | unranked |
| 32 | it | get_calendar_events | unranked |
| 35 | en | list_calendars | unranked |
| 37 | en | create_event | unranked |
| 39 | en | update_event | unranked |
| 45 | en | search_contacts / get_contact_details | 35 |
| 47 | en | create_contact | unranked |
| 49 | en | delete_contact | unranked |

The keywords were not changed after the set was read.

**Out of scope (8 queries, no PIM request among them).** The untagged gates assert nothing for this
list, so these figures are measurement only:
- A PIM tool reaches the top 5 for 3 of the 8: the weather, the web-search flights and the museum
  hours queries.
- None ranks a PIM tool first.
- 1 of the 8 ranks nothing at all.

### Other effects

- **The regression in `TestRetrievalKeywordsRecoverLiveStackMisses`** ("look up the current price of
  something online" ranked `current_time` over `web_search`) appeared with the PIM-only corpus. It
  cleared with the production realignment, before any PIM keyword existed. Its queries and
  expectations are unchanged.
- **`TestToolSearchGate_RealCorpus`** went from 26 cases to 24 (the two shell cases left with their
  tools), and scores top-1 22/24, recall@3 24/24. Two cases lose top-1:
  - "periodic check" ranks `calendar__get_unsubscribe_info` over `task`;
  - "create or update a Linear issue" ranks `linear__create_issue_label` over `linear__save_issue`.
- **Blind-eval negatives (measure harness).** 21 of 58 rank nothing, with or without the PIM keywords.
  Before the keywords were frozen, two passes narrowed them so that no non-PIM query moved and no
  negative gained a ranking. The words removed were:
  - `open`, `leggi`, `arrivo`, `manda` and `mandare`;
  - `aggiungi` and `add`;
  - `telefono`.

### Deferred roster, from this corpus

The roster is `Registry.DeferredRoster`, built from the fixture.

| | Before (85 entries) | After (116 entries) |
|---|---|---|
| whole roster | 2,052 bytes | 2,874 bytes |
| `calendar` line | 30 bytes | 811 bytes |

The appliance's figure is C8's measurement: its roster has no Linear, and it may mount families this
corpus lacks.

### What this does not show

- **More than one sample.** The held-out evidence is one set of 52 queries.
- **What the model chooses.** The ranking is raw BM25, not the model's own choice after `tool_search`.
  The queries are written sets, and a model phrases its own.
- **A like-for-like baseline.** "Before the split" scores the easier criterion: any PIM hit counted,
  because there was one PIM tool.
- **That the corpus equals an appliance's registry.** The dump came from a native-shaped daemon
  (`AURA_IN_CONTAINER` unset). An in-container dump also tried to mount `browser`, `photocraft` and
  `wordcraft`, which this corpus lacks. Linear, memory and WhatsApp were not measured.
- **The keywords on a PIM mounted under another server name.** They are keyed on the `calendar__`
  names, so such a mount gets none of them.
