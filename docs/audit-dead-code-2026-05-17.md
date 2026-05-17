# Dead Code Audit: Aura - 2026-05-17

**Scope:** Read-only audit of codebase for orphan exported symbols, defunct debug harnesses, stale test fixtures, stale planning artifacts, deprecated patterns, and TODO/FIXME comments.

**Methodology:**
- Exported symbol grep: [A-Z][a-zA-Z0-9]* function/type/const/var at package scope
- Out-of-package caller search: PackageName.Symbol across internal/ and cmd/
- Test fixture search: scan testdata/ and fixture/ directories for unused JSON files
- Debug harness age: git log -1 on each cmd/debug_*/main.go and cmd/probe_*/main.go
- Planning dir age: stat on .planning/ subdirectories for 30+ day staleness
- Code comments: grep for deprecated, legacy, TODO: remove, TODO/FIXME with context

**Conservatism rule:** Only flag orphan symbols when ALL are true:
1. No out-of-package caller found
2. No test reference found
3. Not part of a public API or interface implementation
4. Not a JSON-tagged struct field used in encode/decode

---

## 1. CONFIRMED DELETABLE

None found. All reviewed items are false positives or live fixtures.

---

## 2. PROBABLE DELETABLE

### 2.1 Mistral OCR Configuration Fields

**Path:** internal/config/config.go lines 133-141, 284-288  
**Related:** cmd/aura/app.go lines 280-284

**Fields:**
- MistralOCRBaseURL
- MistralOCRModel  
- MistralOCRTableFormat
- MistralOCRExtractHeader
- MistralOCRExtractFooter

**Reason:** Per CLAUDE.md and prd.md, Mistral Document AI OCR was replaced by on-device EmbeddingGemma + llama.cpp. Config fields are still loaded from environment but may be dead code if the Mistral client is never instantiated.

**Risk:** Code is wired but may not be called at runtime.

**Action:** Verify if cmd/aura/app.go line 280 OCR initialization is reached. If not, delete config fields + initialization code together.

**Last touched:** 2026-05-16 (1 day ago)

---

## 3. STALE TODOS CLUSTER

**Finding:** Zero actual TODO/FIXME code comments in non-test .go files.

One hardcoded "TODO:" string exists in tool_definitions.go line 96 as example data, not a code comment.

**Action:** None required.

---

## 4. STALE PLANNING DIRECTORIES

All .planning/ subdirectories are active:
- .planning/audits/* - updated 2026-05-15 (2 days ago)
- .planning/deep-refactor/* - updated 2026-05-16 (1 day ago, INDEX.md current)

**Action:** Keep all as-is.

---

## 5. DEFUNCT DEBUG HARNESSES

All debug_* and probe_* harnesses are recent (last 13 days):
- debug_backup, debug_ingest, debug_qdrant, debug_tools: 2026-05-16 (20 hours)
- debug_telegram, debug_pdf, debug_docx, debug_xlsx: 2026-05-15 (2 days)
- debug_llm: 2026-05-03 (13 days - oldest but acceptable)

**Action:** Keep all. None are >60 days old. All are documented entry points per CLAUDE.md.

---

## 6. REPLACED PATTERNS

Mistral OCR backend was replaced (see section 2.1). No other deprecated patterns found.

---

## 7. ORPHANED EXPORTED SYMBOLS

Spot check of packages:
- testutil.OpenTestDB: used in internal tests
- channels/cron.New(): used in swarm tests
- channels/telegram/fixture.Capture(): snapshots are live fixtures
- channels/silent.New(): used in swarm E2E tests

**Finding:** No orphan exports detected.

---

## 8. TEST FIXTURES

Telegram fixture testdata/ JSON files (fallback_entity_edit_to_plain_text.json, with_cot.json, with_tool_call_and_entity_table.json) are WRITTEN to by fixture.Capture() and READ back by byte_parity_test.go for snapshot verification (Phase02_Protect_Telegram gate).

**Action:** Keep all. They are live Phase02 verification artifacts.

---

## 9. SUMMARY

| Category | Count | Recommendation |
|----------|-------|-----------------|
| Confirmed deletable | 0 | - |
| Probable deletable | 1 | Mistral config (verify runtime usage first) |
| Stale TODOs | 0 | - |
| Orphan exports | 0 | - |

---

## 10. ACTION ITEMS

**For programmer review:**

1. **Optional:** Trace cmd/aura/app.go line 280 to confirm Mistral OCR client is/isn't used at runtime. If dead, delete:
   - config.go MistralOCR* fields (5 lines)
   - config.go env loads (lines 284-288)
   - app.go OCR init (lines 280-284)

2. **Repeat audit** after Phase08 (Cron/Swarm) and Phase09 (Memory) close, as dead code accumulates after large rewrites.

---

**Audit date:** 2026-05-17  
**Risk:** LOW (1 conditional item, 0 confirmed orphans)
