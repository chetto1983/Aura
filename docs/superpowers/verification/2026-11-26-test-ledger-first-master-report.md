# The test ledger's first report on master (2026-11-26)

Plan task B4, second item (`docs/superpowers/plans/2026-11-25-test-ledger.md`). It is the first `master`
CI run of the report-only gate that #282 merged, and the list the allowlist of plan task C1 (#284) was
written from. Measured from CI, not from a container: run
[45866028711](https://github.com/chetto1983/Aura/actions/runs/45866028711) on `231143339`, push of
2026-11-26 17:14 UTC, concluded `success`, the `Test ledger (report-only)` job exiting 0 with the
verdict below. The one `master` run before it, on `f3a6a9a5a` (the merge of #282), was cancelled by
that push and judged nothing.

## Counts

| suite | declared | executed | skipped | excused |
|---|---|---|---|---|
| go | 10169 | 10118 | 3 | 0 |
| playwright | 68 | 238 | 100 | 0 |
| pytest | 273 | 273 | 0 | 0 |
| python | 18 | 242 | 0 | 0 |
| script | 43 | 42 | 0 | 0 |
| vitest | 438 | 4385 | 0 | 0 |

Go and pytest count tests. Playwright, Vitest, Python and shell self-tests are declared by file, so
`declared` counts files and the other columns count the tests their reports list.

## Violations: 153, none of them a job

| Kind | Count |
|---|---|
| A declared test no job executed | 48 Go |
| A unit skipped in every job that ran it | 3 Go, 100 Playwright |
| A declared file in no report | 1 Playwright, 1 shell self-test |
| A needed job that failed or uploaded nothing | 0 |

The list is the same, line for line, as the report-only run on #282's head (`b448b0ae5`, run
45850621478), so nothing that reached `master` between the two added a violation. The first run
of all, on `dd700dbe6` (PR #282), had 168. The 15 extra were that run's own failures: four job lines
from three jobs (`ingest-reconcile-e2e`, failed and without an artifact; `ingest-sidecar-test`, on the
stale cocoindex pin; `web-e2e`), and the 11 chrome units of the live document, paperless and
native-media specs, whose steps never ran once Web E2E's skill-catalogue step had failed. #282 fixed
the first two; the third was the install-count format, fixed in #283.

## What became of each

Recorded in #284 (plan task C1), judged locally against run 45850621478's 21 artifacts before it was
pushed, then by that PR's own CI:

- **Deleted, 5:** `scripts/musr_live_run_assert_test.go`, a `//go:build ignore` file no runner can
  compile (operator, 2026-11-26).
- **Fixed, 2:** `internal/mcp`'s helper processes `TestSDKHelperProcess` and `TestSDKNoisyHelperProcess`
  return outside the child, as the repository's other three do.
- **Wired, 2:** `scripts/cloudflared_image_contract_test.sh` in `build-and-lint`, and
  `mcp-cockpit-live`'s TLS mailbox test in Web E2E. The second needed `AURA_PIM_MCP_URL` set in its
  step, a defect of the default that #285 fixes.
- **Excused, 144,** by 77 entries of `.github/test_ledger_allow.txt`: 26 `paid`, 4 `outward`, 17
  `measure`, 30 `platform`.

The table below gives each violation its outcome.

## Each violation and what closes it

Every one of the 153 has exactly one outcome, checked by matching it against #284's allowlist with the
gate's own selector rules (a unit's id, its file, `<file> [<project>]`, `<package dir> <TestName>`).
The 144 excused units fall in the four classes as 80 `paid`, 9 `outward`, 21 `measure` and 34
`platform`; a file entry covers all of a file's units, hence fewer entries than units.

| suite | unit | closed by |
|---|---|---|
| go | `cmd/aura` `TestDocumentCorpusAgentEval` | excused `paid`, entry `cmd/aura/document_agent_live_test.go` |
| go | `cmd/aura` `TestDocumentProductionAgentE2E` | excused `paid`, entry `cmd/aura/document_agent_live_test.go` |
| go | `cmd/aura` `TestMeasureSkillToolBreakdown` | excused `measure`, entry `cmd/aura/tool_weight_measure_test.go` |
| go | `cmd/aura` `TestMeasureToolWeight` | excused `measure`, entry `cmd/aura/tool_weight_measure_test.go` |
| go | `cmd/aura` `TestMediaDocumentProductionAgentE2E` | excused `paid`, entry `cmd/aura/document_agent_live_test.go` |
| go | `internal/agent` `TestLiveMeteoCaraglio` | excused `paid`, entry `internal/agent/live_finalize_test.go` |
| go | `internal/agent` `TestLiveToolChoiceNone` | excused `paid`, entry `internal/agent/live_finalize_test.go` |
| go | `internal/agent` `TestLiveTrustedMemoryUsesFactWithoutGrantingInstructionAuthority` | excused `paid`, entry `internal/agent/live_memory_test.go` |
| go | `internal/agent` `TestTurnRecallFrozenEval` | excused `paid`, entry `internal/agent/turn_recall_eval_test.go` |
| go | `internal/agent/tools` `TestMeasureBM25Variants` | excused `measure`, entry `internal/agent/tools/bm25_variants_measure_test.go` |
| go | `internal/agent/tools` `TestMeasureRetrievalKeywords` | excused `measure`, entry `internal/agent/tools/search_keywords_measure_test.go` |
| go | `internal/agenteval` `TestBehaviourGate` | excused `paid`, entry `internal/agenteval/live_test.go` |
| go | `internal/arcadedb` `TestCardCosineBand` | excused `measure`, entry `internal/arcadedb/document_card_floor_bench_test.go` |
| go | `internal/arcadedb` `TestCardTextVariants` | excused `measure`, entry `internal/arcadedb/document_card_floor_bench_test.go` |
| go | `internal/arcadedb` `TestDenseFilterCost` | excused `measure`, entry `internal/arcadedb/document_filter_bench_test.go` |
| go | `internal/arcadedb` `TestFusedQueryVariants` | excused `measure`, entry `internal/arcadedb/document_query_variants_bench_test.go` |
| go | `internal/channels/telegram` `TestArtifactLiveE2E_DocumentReachesAnAuthorizedChat` | excused `outward`, entry `internal/channels/telegram/artifact_live_e2e_test.go` |
| go | `internal/channels/telegram` `TestLiveSendDocumentResponse` | excused `outward`, entry `internal/channels/telegram/integration_test.go` |
| go | `internal/channels/telegram` `TestLiveSendPhotoResponse` | excused `outward`, entry `internal/channels/telegram/integration_test.go` |
| go | `internal/channels/telegram` `TestLiveSendVoiceResponse` | excused `outward`, entry `internal/channels/telegram/integration_test.go` |
| go | `internal/conversations` `TestMeasureL1` | excused `measure`, entry `internal/conversations/l1_measure_test.go` |
| go | `internal/conversations` `TestMeasureOptionalFieldWeight` | excused `measure`, entry `internal/conversations/toon_measure_test.go` |
| go | `internal/conversations` `TestMeasureTOON` | excused `measure`, entry `internal/conversations/toon_measure_test.go` |
| go | `internal/cron` `TestSchedulerNorthStarE2E` | excused `paid`, entry `internal/cron/e2e_test.go` |
| go | `internal/documents` `TestBenchmarkMetricsSupportMultipleGoldDocsAndExposeRegression` | excused `measure`, entry `internal/documents/retrieval_fusion_bench_test.go` |
| go | `internal/documents` `TestCodingAgentQuestions` | excused `measure`, entry `internal/documents/retrieval_coding_questions_bench_test.go` |
| go | `internal/documents` `TestFusionBenchmark` | excused `measure`, entry `internal/documents/retrieval_fusion_bench_test.go` |
| go | `internal/documents` `TestProductionRetrievalRecall` | excused `measure`, entry `internal/documents/retrieval_recall_bench_test.go` |
| go | `internal/llm/openai_compat` `TestLiveReasoning_HighTierNoStarvation` | excused `paid`, entry `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go` |
| go | `internal/llm/openai_compat` `TestLiveReasoning_NoneTierOffSwitch` | excused `paid`, entry `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go` |
| go | `internal/llm/openai_compat` `TestLiveReasoning_RouterEnabledFalseOffSwitch` | excused `paid`, entry `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go` |
| go | `internal/llm/openai_compat` `TestOllamaLiveProfileDiscovery` | excused `paid`, entry `internal/llm/openai_compat/ollama_live_e2e_test.go` |
| go | `internal/llm/openai_compat` `TestOllamaLiveStreamingCompletion` | excused `paid`, entry `internal/llm/openai_compat/ollama_live_e2e_test.go` |
| go | `internal/llm/openai_compat` `TestOllamaLiveStreamingReasoning` | excused `paid`, entry `internal/llm/openai_compat/ollama_live_e2e_test.go` |
| go | `internal/llm/openai_compat` `TestOllamaLiveStreamingToolCall` | excused `paid`, entry `internal/llm/openai_compat/ollama_live_e2e_test.go` |
| go | `internal/mcp` `TestSDKHelperProcess` | fixed: returns outside the child, no skip (c7be041ac) |
| go | `internal/mcp` `TestSDKNoisyHelperProcess` | fixed: returns outside the child, no skip (c7be041ac) |
| go | `internal/procgroup` `TestSetProcessGroupWindowsSetsCreateNewProcessGroup` | excused `platform`, entry `internal/procgroup/procgroup_windows_test.go` |
| go | `internal/procgroup` `TestTaskkillProcessMissingTolerates` | excused `platform`, entry `internal/procgroup/procgroup_windows_test.go` |
| go | `internal/runner` `TestLiveE2E_AutoTitleAndCost` | excused `paid`, entry `internal/runner/live_e2e_scenarios_test.go` |
| go | `internal/runner` `TestLiveE2E_ExpiredApprovalVisibleToFreshAgent` | excused `paid`, entry `internal/runner/live_e2e_expiry_test.go` |
| go | `internal/runner` `TestLiveE2E_PauseResume` | excused `paid`, entry `internal/runner/live_e2e_scenarios_test.go` |
| go | `internal/runner` `TestLiveE2E_RestrictedDecisionPolicy` | excused `paid`, entry `internal/runner/live_e2e_policy_test.go` |
| go | `internal/runner` `TestLiveE2E_SearchSimilarity` | excused `paid`, entry `internal/runner/live_e2e_scenarios_test.go` |
| go | `internal/runner` `TestLiveE2E_ThreeSimultaneous` | excused `paid`, entry `internal/runner/live_e2e_scenarios_test.go` |
| go | `internal/sandbox/usersandbox` `TestSoak_ConcurrentIdentities` | excused `platform`, entry `internal/sandbox/usersandbox TestSoak_ConcurrentIdentities` |
| go | `scripts` `TestCleanFixturePasses` | deleted with its `//go:build ignore` file (41d3df57a) |
| go | `scripts` `TestCleanSwappedFixturePasses` | deleted with its `//go:build ignore` file (41d3df57a) |
| go | `scripts` `TestEmptyTranscriptFixtureFailsNamingCompletion` | deleted with its `//go:build ignore` file (41d3df57a) |
| go | `scripts` `TestLeakingFixtureFailsNamingCrossRead` | deleted with its `//go:build ignore` file (41d3df57a) |
| go | `scripts` `TestNoOverlapFixtureFailsNamingOverlap` | deleted with its `//go:build ignore` file (41d3df57a) |
| playwright | `artifact-position-live.spec.ts › live artifact position › a file delivered in a later turn stays on that turn after a reload [chrome]` | excused `paid`, entry `web/e2e/artifact-position-live.spec.ts` |
| playwright | `artifact-position-live.spec.ts › live artifact position › a file delivered in a later turn stays on that turn after a reload [mobile-chrome]` | excused `paid`, entry `web/e2e/artifact-position-live.spec.ts` |
| playwright | `artifact-render-live.spec.ts › live artifact render lane › composer stays anchored while messages scroll and the workspace resizes [chrome]` | excused `paid`, entry `web/e2e/artifact-render-live.spec.ts` |
| playwright | `artifact-render-live.spec.ts › live artifact render lane › composer stays anchored while messages scroll and the workspace resizes [mobile-chrome]` | excused `paid`, entry `web/e2e/artifact-render-live.spec.ts` |
| playwright | `artifact-render-live.spec.ts › live artifact render lane › inline artifact expands, splits code and preview, downloads and returns to chat [chrome]` | excused `paid`, entry `web/e2e/artifact-render-live.spec.ts` |
| playwright | `artifact-render-live.spec.ts › live artifact render lane › inline artifact expands, splits code and preview, downloads and returns to chat [mobile-chrome]` | excused `paid`, entry `web/e2e/artifact-render-live.spec.ts` |
| playwright | `artifact-render-live.spec.ts › live artifact render lane › the cockpit frames the sealed render route and the document runs [chrome]` | excused `paid`, entry `web/e2e/artifact-render-live.spec.ts` |
| playwright | `artifact-render-live.spec.ts › live artifact render lane › the cockpit frames the sealed render route and the document runs [mobile-chrome]` | excused `paid`, entry `web/e2e/artifact-render-live.spec.ts` |
| playwright | `attachment-filename.spec.ts › a chat attachment carries its real name to the object store [mobile-chrome]` | excused `platform`, entry `web/e2e/attachment-filename.spec.ts › a chat attachment carries its...` |
| playwright | `background-shell-completion-real.spec.ts › real background-shell completion hook › reactivates the owning conversation once and lets the agent collect final output [chrome]` | excused `paid`, entry `web/e2e/background-shell-completion-real.spec.ts` |
| playwright | `background-shell-completion-real.spec.ts › real background-shell completion hook › reactivates the owning conversation once and lets the agent collect final output [mobile-chrome]` | excused `paid`, entry `web/e2e/background-shell-completion-real.spec.ts` |
| playwright | `board-layout.spec.ts › the desktop board fills its width and keeps its controls compact [mobile-chrome]` | excused `platform`, entry `web/e2e/board-layout.spec.ts › the desktop board fills its width an...` |
| playwright | `board.spec.ts › a column over its limit is marked, and the cards stay [mobile-chrome]` | excused `platform`, entry `web/e2e/board.spec.ts › a column over its limit is marked, and the ...` |
| playwright | `browser-sidecar-live.spec.ts › browser sidecar live acceptance › eval is withheld behind a destructive approval without reading any credentials [chrome]` | excused `paid`, entry `web/e2e/browser-sidecar-live.spec.ts` |
| playwright | `browser-sidecar-live.spec.ts › browser sidecar live acceptance › eval is withheld behind a destructive approval without reading any credentials [mobile-chrome]` | excused `paid`, entry `web/e2e/browser-sidecar-live.spec.ts` |
| playwright | `browser-sidecar-live.spec.ts › browser sidecar live acceptance › real agent opens a live view, downloads a complete file, and cannot see browser credentials from shell [chrome]` | excused `paid`, entry `web/e2e/browser-sidecar-live.spec.ts` |
| playwright | `browser-sidecar-live.spec.ts › browser sidecar live acceptance › real agent opens a live view, downloads a complete file, and cannot see browser credentials from shell [mobile-chrome]` | excused `paid`, entry `web/e2e/browser-sidecar-live.spec.ts` |
| playwright | `browser-sidecar-live.spec.ts › browser sidecar live acceptance › real browser tool calls reject file URLs and screenshot filesystem destinations [chrome]` | excused `paid`, entry `web/e2e/browser-sidecar-live.spec.ts` |
| playwright | `browser-sidecar-live.spec.ts › browser sidecar live acceptance › real browser tool calls reject file URLs and screenshot filesystem destinations [mobile-chrome]` | excused `paid`, entry `web/e2e/browser-sidecar-live.spec.ts` |
| playwright | `chat-branches-live.spec.ts › conversation branches against a live model › navigates, continues and reloads the conversation tree [chrome]` | excused `paid`, entry `web/e2e/chat-branches-live.spec.ts` |
| playwright | `chat-branches-live.spec.ts › conversation branches against a live model › navigates, continues and reloads the conversation tree [mobile-chrome]` | excused `paid`, entry `web/e2e/chat-branches-live.spec.ts` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism Chrome contracts › Calm Prism desktop-dark [mobile-chrome]` | excused `platform`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism Chrome contracts › Cal...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism Chrome contracts › Calm Prism desktop-light [mobile-chrome]` | excused `platform`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism Chrome contracts › Cal...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism Chrome contracts › Calm Prism mobile-dark [chrome]` | excused `platform`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism Chrome contracts › Cal...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism Chrome contracts › Calm Prism mobile-light [chrome]` | excused `platform`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism Chrome contracts › Cal...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism Chrome contracts › reflows without overflow at 320px [mobile-chrome]` | excused `platform`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism Chrome contracts › ref...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism Chrome contracts › reflows without overflow at 768px [mobile-chrome]` | excused `platform`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism Chrome contracts › ref...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism Chrome contracts › zoom, forced-colors, keyboard, and accessibility-tree smoke [mobile-chrome]` | excused `platform`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism Chrome contracts › zoo...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism combined comparisons › builds labelled same-input comparison PNGs [chrome]` | excused `measure`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism combined comparisons ›...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism combined comparisons › builds labelled same-input comparison PNGs [mobile-chrome]` | excused `measure`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism combined comparisons ›...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism evidence capture › capture desktop-dark [chrome]` | excused `measure`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism evidence capture › cap...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism evidence capture › capture desktop-dark [mobile-chrome]` | excused `measure`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism evidence capture › cap...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism evidence capture › capture mobile-dark [chrome]` | excused `measure`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism evidence capture › cap...` |
| playwright | `chat-calm-prism.spec.ts › Calm Prism evidence capture › capture mobile-dark [mobile-chrome]` | excused `measure`, entry `web/e2e/chat-calm-prism.spec.ts › Calm Prism evidence capture › cap...` |
| playwright | `chat-real-agent.spec.ts › real-agent Calm Prism acceptance › scores the real composer, DOCX RAG, persistence, and responsive health at 10/10 [chrome]` | excused `paid`, entry `web/e2e/chat-real-agent.spec.ts` |
| playwright | `chat-real-agent.spec.ts › real-agent Calm Prism acceptance › scores the real composer, DOCX RAG, persistence, and responsive health at 10/10 [mobile-chrome]` | excused `paid`, entry `web/e2e/chat-real-agent.spec.ts` |
| playwright | `chat-thread-switch-real.spec.ts › real multi-conversation ownership › keeps A and B isolated across a live run, canonical URL, and second tab [chrome]` | excused `paid`, entry `web/e2e/chat-thread-switch-real.spec.ts` |
| playwright | `chat-thread-switch-real.spec.ts › real multi-conversation ownership › keeps A and B isolated across a live run, canonical URL, and second tab [mobile-chrome]` | excused `paid`, entry `web/e2e/chat-thread-switch-real.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › deckcraft: HWC20_PPT_IT.pptx [chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › deckcraft: HWC20_PPT_IT.pptx [mobile-chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › filmcraft: sintel-trailer-1080p.mp4 [chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › filmcraft: sintel-trailer-1080p.mp4 [mobile-chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › gridcraft: fcffginzu.xls [chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › gridcraft: fcffginzu.xls [mobile-chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › pdfcraft: anac-nota-illustrativa.pdf [chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › pdfcraft: anac-nota-illustrativa.pdf [mobile-chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › photocraft: ponte-vecchio-dusk.jpg [chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › photocraft: ponte-vecchio-dusk.jpg [mobile-chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › wordcraft: bando-tipo-anac-2.docx [chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `craft-live.spec.ts › Craft MCP servers, live, with the real agent › wordcraft: bando-tipo-anac-2.docx [mobile-chrome]` | excused `paid`, entry `web/e2e/craft-live.spec.ts` |
| playwright | `documents-filter-eval-live.spec.ts › the which-documents set, live › filters raise set recall over today search and lose no gold [mobile-chrome]` | excused `platform`, entry `web/e2e/documents-filter-eval-live.spec.ts [mobile-chrome]` |
| playwright | `documents-live.spec.ts › the Documenti view, live › desktop › list, filter, bulk edit and review, as the operator [mobile-chrome]` | excused `platform`, entry `web/e2e/documents-live.spec.ts [mobile-chrome]` |
| playwright | `documents-live.spec.ts › the Documenti view, live › mobile › list, filter, bulk edit and review, as the operator [mobile-chrome]` | excused `platform`, entry `web/e2e/documents-live.spec.ts [mobile-chrome]` |
| playwright | `documents-survival-live.spec.ts › document fields survive what the cockpit does to a file, live › rename, move and new bytes keep the values [mobile-chrome]` | excused `platform`, entry `web/e2e/documents-survival-live.spec.ts [mobile-chrome]` |
| playwright | `file-manager-write.spec.ts › a folder created through the widget reaches the bucket, labelled as JSON [mobile-chrome]` | excused `platform`, entry `web/e2e/file-manager-write.spec.ts › a folder created through the w...` |
| playwright | `file-manager-write.spec.ts › the deployed server refuses a write that is not labelled JSON [mobile-chrome]` | excused `platform`, entry `web/e2e/file-manager-write.spec.ts › the deployed server refuses a ...` |
| playwright | `governance.spec.ts › Phase 28 — Governance boards (desktop + mobile) › the rail and the board list resize independently [mobile-chrome]` | excused `platform`, entry `web/e2e/governance.spec.ts › Phase 28 — Governance boards (desktop ...` |
| playwright | `graph-expand.spec.ts › Graph expansion grows the graph in place › a double click on a node expands it [mobile-chrome]` | excused `platform`, entry `web/e2e/graph-expand.spec.ts › Graph expansion grows the graph in p...` |
| playwright | `graph-live-arcadedb.spec.ts › live ArcadeDB memory graph › draws the authenticated identity graph and expands a RID cumulatively [mobile-chrome]` | excused `platform`, entry `web/e2e/graph-live-arcadedb.spec.ts [mobile-chrome]` |
| playwright | `live-rag-replay.spec.ts › live RAG replay smoke › uploads a real DOCX, uses document_search, and replays cleanly on mobile [chrome]` | excused `paid`, entry `web/e2e/live-rag-replay.spec.ts` |
| playwright | `live-rag-replay.spec.ts › live RAG replay smoke › uploads a real DOCX, uses document_search, and replays cleanly on mobile [mobile-chrome]` | excused `paid`, entry `web/e2e/live-rag-replay.spec.ts` |
| playwright | `management-key-onboarding-live.spec.ts › live management-key onboarding › follows the services cap an admin changes after the first run [chrome]` | excused `outward`, entry `web/e2e/management-key-onboarding-live.spec.ts` |
| playwright | `management-key-onboarding-live.spec.ts › live management-key onboarding › follows the services cap an admin changes after the first run [mobile-chrome]` | excused `outward`, entry `web/e2e/management-key-onboarding-live.spec.ts` |
| playwright | `management-key-onboarding-live.spec.ts › live management-key onboarding › mints, bills and caps OpenRouter keys by role [chrome]` | excused `outward`, entry `web/e2e/management-key-onboarding-live.spec.ts` |
| playwright | `management-key-onboarding-live.spec.ts › live management-key onboarding › mints, bills and caps OpenRouter keys by role [mobile-chrome]` | excused `outward`, entry `web/e2e/management-key-onboarding-live.spec.ts` |
| playwright | `mcp-cockpit-live.spec.ts › connects a mailbox over TLS from Settings, and removes it [chrome]` | wired: Web E2E's TLS mailbox step (b83df6fb7, d8bdd5715) |
| playwright | `mcp-cockpit-live.spec.ts › connects a mailbox over TLS from Settings, and removes it [mobile-chrome]` | excused `platform`, entry `web/e2e/mcp-cockpit-live.spec.ts › connects a mailbox over TLS from...` |
| playwright | `mcp-cockpit-live.spec.ts › sends through the real agent from a mailbox connected in Settings [chrome]` | excused `paid`, entry `web/e2e/mcp-cockpit-live.spec.ts › sends through the real agent fro...` |
| playwright | `mcp-cockpit-live.spec.ts › sends through the real agent from a mailbox connected in Settings [mobile-chrome]` | excused `paid`, entry `web/e2e/mcp-cockpit-live.spec.ts › sends through the real agent fro...` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › a detached clip replays a static frame and arrives on the wake [chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › a detached clip replays a static frame and arrives on the wake [mobile-chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › a first frame animation reuses the generated image [chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › a first frame animation reuses the generated image [mobile-chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › the cockpit streams the delivered clip over HTTP Range [chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › the cockpit streams the delivered clip over HTTP Range [mobile-chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › the real agent delivers a clip in the same turn [chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › the real agent delivers a clip in the same turn [mobile-chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › the real agent generates a visible image [chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `media-generation-live.spec.ts › media generation (paid, real agent) › the real agent generates a visible image [mobile-chrome]` | excused `paid`, entry `web/e2e/media-generation-live.spec.ts` |
| playwright | `model-settings-hot-reload-live.spec.ts › live primary-model hot reload › switches Ollama and llama.cpp without an Aura restart [chrome]` | excused `paid`, entry `web/e2e/model-settings-hot-reload-live.spec.ts` |
| playwright | `model-settings-hot-reload-live.spec.ts › live primary-model hot reload › switches Ollama and llama.cpp without an Aura restart [mobile-chrome]` | excused `paid`, entry `web/e2e/model-settings-hot-reload-live.spec.ts` |
| playwright | `native-media-live.spec.ts › selected-model native media live acceptance › projects a current-turn Garage image into the selected primary [mobile-chrome]` | excused `platform`, entry `web/e2e/native-media-live.spec.ts [mobile-chrome]` |
| playwright | `origin-isolation-live.spec.ts › artifact origin isolation (live) › the daemon serves the data proxy and the sandboxed artifact policy [mobile-chrome]` | excused `platform`, entry `web/e2e/origin-isolation-live.spec.ts [mobile-chrome]` |
| playwright | `paperless-first-import-live.spec.ts › a first Files import refreshes an already mounted empty Library [mobile-chrome]` | excused `platform`, entry `web/e2e/paperless-first-import-live.spec.ts [mobile-chrome]` |
| playwright | `paperless-isolation-live.spec.ts › documents, indexed text, thumbnails and views remain private between real identities [mobile-chrome]` | excused `platform`, entry `web/e2e/paperless-isolation-live.spec.ts [mobile-chrome]` |
| playwright | `paperless-saved-view-delete-live.spec.ts › pinned saved views can be cancelled or deleted from the 1440px navigation [mobile-chrome]` | excused `platform`, entry `web/e2e/paperless-saved-view-delete-live.spec.ts [mobile-chrome]` |
| playwright | `paperless-saved-view-delete-live.spec.ts › pinned saved views can be cancelled or deleted from the 390px navigation [mobile-chrome]` | excused `platform`, entry `web/e2e/paperless-saved-view-delete-live.spec.ts [mobile-chrome]` |
| playwright | `paperless-workflow-live.spec.ts › Paperless document workflow on the real backend › upload, content search, review and durable saved view at 1440px [mobile-chrome]` | excused `platform`, entry `web/e2e/paperless-workflow-live.spec.ts [mobile-chrome]` |
| playwright | `paperless-workflow-live.spec.ts › Paperless document workflow on the real backend › upload, content search, review and durable saved view at 390px [mobile-chrome]` | excused `platform`, entry `web/e2e/paperless-workflow-live.spec.ts [mobile-chrome]` |
| playwright | `phase32-uat.spec.ts › Phase 32 UAT — SkeletonBlock CSS-wave loading visuals (3 migrated consumers) › ConversationSidebar renders the rich skeleton while the conversation list loads [mobile-chrome]` | excused `platform`, entry `web/e2e/phase32-uat.spec.ts › Phase 32 UAT — SkeletonBlock CSS-wave...` |
| playwright | `phase32-uat.spec.ts › Phase 32 UAT — SkeletonBlock CSS-wave loading visuals (3 migrated consumers) › SearchPanel renders the rich skeleton while a search is in flight [mobile-chrome]` | excused `platform`, entry `web/e2e/phase32-uat.spec.ts › Phase 32 UAT — SkeletonBlock CSS-wave...` |
| playwright | `remote-access-live.spec.ts` (file) | excused `outward`, entry `web/e2e/remote-access-live.spec.ts` |
| playwright | `skill-catalog-search-live.spec.ts › live skill catalog performance score › passes the approved rubric at 10/10 [mobile-chrome]` | excused `platform`, entry `web/e2e/skill-catalog-search-live.spec.ts [mobile-chrome]` |
| playwright | `studio-live.spec.ts › Studio (paid, real generations) › both survive a reload in History, and no conversation was touched [chrome]` | excused `paid`, entry `web/e2e/studio-live.spec.ts` |
| playwright | `studio-live.spec.ts › Studio (paid, real generations) › both survive a reload in History, and no conversation was touched [mobile-chrome]` | excused `paid`, entry `web/e2e/studio-live.spec.ts` |
| playwright | `studio-live.spec.ts › Studio (paid, real generations) › generates one clip on google/veo-3.1-lite at ≈ $0.12 [chrome]` | excused `paid`, entry `web/e2e/studio-live.spec.ts` |
| playwright | `studio-live.spec.ts › Studio (paid, real generations) › generates one clip on google/veo-3.1-lite at ≈ $0.12 [mobile-chrome]` | excused `paid`, entry `web/e2e/studio-live.spec.ts` |
| playwright | `studio-live.spec.ts › Studio (paid, real generations) › generates one image on the cheapest declared model [chrome]` | excused `paid`, entry `web/e2e/studio-live.spec.ts` |
| playwright | `studio-live.spec.ts › Studio (paid, real generations) › generates one image on the cheapest declared model [mobile-chrome]` | excused `paid`, entry `web/e2e/studio-live.spec.ts` |
| playwright | `two-role-live.spec.ts › live two-role capabilities › a member works, is refused the admin pair, and reads its own refusals [chrome]` | excused `paid`, entry `web/e2e/two-role-live.spec.ts` |
| playwright | `two-role-live.spec.ts › live two-role capabilities › a member works, is refused the admin pair, and reads its own refusals [mobile-chrome]` | excused `paid`, entry `web/e2e/two-role-live.spec.ts` |
| script | `scripts/cloudflared_image_contract_test.sh` | wired: build-and-lint builds the image and runs it (225821603) |

## What this does not prove

- That the allowlist is right. This run had none; #284's CI is where the entries meet the artifacts,
  and an entry that matches an executed test or nothing is itself a violation there.
- Anything about `skills.yml`, `live-outward.yml`, `create-aura-appliance.yml` or the publish
  workflows: the gate reads `ci.yml`'s artifacts only. The Telegram live tier, for one, is green or red
  in the nightly workflow and only excused here.
- That an executed test asserts anything, or that an excused one still works: the ledger keeps them
  visible, not green (spec §9).
- Coverage of a Playwright test on both projects: a unit is one test on one project, so a spec that
  passes on chrome and is skipped on mobile-chrome is one executed and one skipped unit.
- Pushes that run no CI (documentation-only changes under the workflow's `paths` filter).
