# Phase 46: MCP trust and facade — Research (REPLAN)

**Supersedes:** `46-RESEARCH.md` as dated 2026-08-17, in full. Trigger:
`.planning/phases/46-mcp-trust-and-facade/46-HALT-2026-08-22.md`, whose §1-§7 are authoritative
where anything below would disagree. This file does not restate the HALT's live measurements — it
cites them — and adds what the HALT itself left for a replan to settle (§6).

**Researched:** 2026-08-22 (re-research after halt). **Domain:** unchanged — MCP host policy
(`internal/agent/mcptools` + `internal/gateway`) and two forked MCP sidecars.

**Confidence:** HIGH on everything measured live this session and quoted with its source below.
Two claims are flagged `UNVERIFIED — planner must confirm` rather than re-checked, per this
session's explicit instruction not to re-verify after the second interruption; both are named where
they occur.

## Session note — the tree moved twice under this research

1. At the start of this session the WhatsApp/calendar pin edits (HALT §7) were **uncommitted**.
   They are now **committed**: `73764ea11` *"Pin both MCP sidecars to the commit they were built
   from"* (2026-08-22 14:27), touching `compose.yaml`, `.github/workflows/ci.yml`,
   `cmd/aura/container_artifacts_test.go`, `docker/aura/PROVENANCE.md`,
   `.planning/codebase/INTEGRATIONS.md`, `.planning/STATE.md`, and the HALT doc itself.
2. That commit's subject says **"both sidecars"** — it does not stop at WhatsApp. The HALT (§7,
   "Left open, deliberately") explicitly left the PIM pin floating because only one workflow wrote
   its `:sidecar` tag and it was "currently identical" to the commit-sha tag beside it. That
   equality broke the same afternoon: `aura/pim-sidecar` merged upstream through `3c0ae72d7`, the
   fork's publish reran, GHCR's `:sidecar` moved to a new digest, and the running host — pinned
   only by an unpinned tag name, `pull_policy: missing` — kept serving the old bytes under an
   unchanged name with nothing to report the divergence. `73764ea11`'s own commit message records
   this measured, not inferred, and closes it: `compose.yaml`'s `AURA_PIM_MCP_IMAGE` default now
   reads `ghcr.io/chetto1983/aura-pim-mcp:10383276961828bc19f34a9372ba2c64a14e2b62` (the fork's
   `aura-publish-image.yml` tags the raw 40-hex `github.sha` alongside `:sidecar`, so the format
   46-06 already assumes was available and used). WhatsApp's pin is unchanged from HALT §7:
   `ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345`.
3. Live, at time of writing: `aura-pim-mcp` container running the new 40-hex tag, healthy;
   `aura-whatsapp` running `sha-e0b8345`, healthy, session paired (`jid 393248682022:31@s.whatsapp.net`).
4. **Consequence for planning:** both mounts are now settled by commit, not one. The replan targets
   `aura-pim-mcp:10383276961828bc19f34a9372ba2c64a14e2b62` and `whatsapp-mcp:sha-e0b8345` as the
   pre-curation baseline for BOTH forks — 46-05/46-06 are not curating against a stale pin any more
   than 46-08 is, they were simply never wrong about the *branch* the way WhatsApp was.
5. A second session is committing into this checkout concurrently. Everything below not explicitly
   re-measured after point 1 above reflects this session's own earlier reads; where the working
   tree could plausibly have moved again since, it is flagged.

## User Constraints (from CONTEXT.md) — carried forward, one correction

CONTEXT.md's D-00..D-38 are NOT re-litigated by this research except where named below. Load-bearing
for the replan, unchanged: **D-17** (curation lives in the fork, zero Aura-side facade/hide-list/
config — the organizing constraint of the whole phase), **D-18** (one multiplexed tool per sidecar,
two always-loaded slots) **— AMENDED for WhatsApp only, see "The views question" below: WhatsApp's
slot now holds three model-facing tools, not one; D-18 stands as written for calendar**, **D-19**
(flat-union `action` schema, typed IDs, never a bare `id`), **D-20** (MCP-05's `accountId` fix lives
in the fork's schema, never host-injected), **D-21/D-34/D-35** (re-key `trustedRecipeActions` to
action names, gate `Multiplexed` on classifier existence not schema shape, document the mixed key
space), **D-23** (immutable pin over floating tag — reinforced twice now, first by the WhatsApp
branch-ownership hazard, second by the PIM `:sidecar` drift this session's commit closed), **D-26**
(DELETE the raw handlers, never merely unadvertise), **D-27** (≤3 model-facing tools per server earns
a slot, global cap 2, arithmetic not a list), **D-32/D-33** (fork publishes first, one atomic commit
per source; WARN-not-panic at mount on drift). **Deferred, unchanged:** D-22 (tool_search scope),
memory promotion (Phase 48), `@sha256` digest pinning, a `_meta`-declared always-load hint.

**Operator directives, unchanged:** no ceremony; native MCP client; all Aura's MCP servers are forks
Aura controls.

<phase_requirements>
## Phase Requirements

| ID | Description | Research support |
|----|-------------|-------------------|
| MCP-01 | Descriptions reach the model as ordinary text | Shipped (`34b892512`), unaffected by the halt; ratify by amendment (46-01, unchanged) |
| MCP-02 | Fail-closed classification survives, fencing dropped | Unaffected; 46-01/46-03 unchanged |
| MCP-03 | Trust unconditional across every mounted server | Unaffected; 46-01/46-09 unchanged |
| MCP-04 | Calendar+WhatsApp collapse to curated, always-loaded surfaces | **Directly affected** — see "curated action list" and "the views question" below |
| MCP-05 | `accountId` fixed in the fork, not host-injected | Unaffected by the halt (calendar-only); 46-05 unchanged |
| TOOL-14 | Tiering axis = frequency + hard count budget | Unaffected in mechanism (46-04); its *worked example* in prose changes — WhatsApp is no longer "1 tool," see below |
</phase_requirements>

## 1. The curated action list, re-derived from the served 14

The WhatsApp sidecar (`sha-e0b8345`, `main`) serves exactly the 14 tools HALT §1 tabulates, all
`readOnlyHint`-unset. This research adds nothing to that count — it was measured live this session
via a direct `tools/list` against `127.0.0.1:8092/mcp` and matched the HALT's table tool-for-tool,
including the two view bindings (`list_messages` → `ui://whatsapp/thread.html`,
`list_chats` → `ui://whatsapp/chats.html`) and the absence of `_meta` on the other 12.

Given the views decision below (§2), the split is:

- **Merge into one curated `messages` tool (12 actions):** `search_contacts`, `get_contact`,
  `get_chat`, `get_direct_chat_by_contact`, `get_contact_chats`, `get_last_interaction`,
  `get_message_context`, `download_media`, `send_message`, `send_reaction`, `send_file`,
  `send_audio_message`. Risk classes carry over unchanged from `bridge_risk.go`'s existing
  `trustedRecipeActions[whatsAppRecipeSource]` (verified this session still lists all 14 with classes
  matching the live server exactly — the Go table was never stale, per HALT §2): 7 read, 1 mutate
  (`download_media`), 4 destructive.
- **Keep as their own raw, un-curated, always-loaded tools (2 tools):** `list_chats`, `list_messages`
  — see §2 for why.
- **Nothing is dropped.** This is a different "12" than the one the 2026-08-17 research measured —
  that 12 was a branch missing two actions entirely; this 12 is a deliberate exemption of two actions
  that exist and are kept, just not folded into the multiplexed tool.

Calendar's 14 (all read/mutate/destructive, unchanged, no `_meta`, no `resources` capability — see
§3) collapse into ONE curated tool exactly as originally planned; nothing here changes calendar's
action list.

## 2. The views question — recommendation and cost

**Recommendation: (b) — exempt `list_chats` and `list_messages` from the merge.** Curate the
remaining 12 WhatsApp actions into one `messages` tool; leave the two view-bound reads as their own
plain tools. This is not a menu — the other two candidates are rejected below with reasons, not left
open.

**Why (a) "curate all 14 and drop the views" is rejected.** The views are not aspirational — HALT §4
proves them live and in active use (`GET /api/mcp/view/...thread.html` → 200, 27,733 bytes of armed
HTML; `POST /api/mcp/view/call {tool:list_chats}` → 200 with real chat rows). Dropping them destroys
a working, in-use operator capability to buy nothing the exemption doesn't also buy — the tool-count
saving is 2 tools, well inside the ≤3 ceiling either way.

**Why (c) "a per-result view mechanism" was investigated and does not exist — inventory, not
assumption.** Two independent sources checked this session:
- **Aura's own code.** `bridgedTool` (`internal/agent/mcptools/bridge.go:35-49`) carries exactly one
  `view mcp.ViewRef` field, set once at construction from the RAW mounted tool (`bridge.go:164`,
  `view: viewRefFor(policy, t)`) and explicitly never refreshed (comment: *"a server that repoints a
  tool at a different document mid-run would move the operator's rendering surface underneath
  them"*). `ViewRef` itself (`internal/mcp/apps.go:33-43`) is documented as *"what a TOOL carries"* —
  one `ResourceURI`, read from that tool's own `_meta.ui` in `tools/list`. There is no field, map, or
  parse path anywhere in `bridge_views.go`/`apps.go`/`appviews.go` that reads a view reference off a
  CALL RESULT rather than the static tool definition. The view-callback path is a second, separate
  finding worth carrying: `CallReadOnlyTool` → `toolIsReadOnly` → `s.bridged[name]`
  (`bridge_supervisor.go:337-343`) is keyed by the RAW mounted-server tool name, not the model-facing
  namespaced/multiplexed name — so a curated `messages` tool would not merely fail the Mutating gate
  for a view callback, the view's own embedded JS (written against `list_chats`/`list_messages` by
  name) would find no tool by that name at all the moment those names are curated away. This is a
  THIRD, independent way the 14→1 merge breaks the views, beyond the two HALT §4 already names.
- **The protocol itself.** Fetched directly from
  `github.com/modelcontextprotocol/ext-apps/specification/draft/apps.mdx` (SEP-1865, status Final)
  this session. The tool-to-view binding is defined once, on the TOOL (`interface McpUiToolMeta {
  resourceUri?: string; visibility?: ... }`, attached to `Tool._meta.ui`), read at `tools/list`, and
  the spec states the behavior plainly: *"If `ui.resourceUri` is present ... host renders tool
  results using the specified UI resource"* — the tool's declared one, not a per-call one. Under
  **§Extensibility → "Other Advanced Features (see Future Considerations)"** the spec lists, verbatim:
  *"Support multiple UI resources in a tool response"* — i.e. a genuinely per-result mechanism is
  explicitly named as NOT part of the finalized MVP, deferred to unscheduled future work. (A
  different, easily-confused mechanism DOES exist per-response in the spec: `resources/read`'s
  content item MAY carry its own `_meta.ui` overriding the listing-level one — but that is the
  resource's FRAMING policy, CSP/permissions, already modeled in Aura as `ViewPolicy`, not a
  selection of WHICH resource a tool's result renders in.)

Conclusion, with both sources agreeing: candidate (c) is not a corner Aura's code failed to build —
it is a capability the finalized spec does not yet define. Building it would mean inventing a
protocol extension Aura's forks and Aura's host would both have to speak, alone, which is exactly the
bespoke-protocol trap CLAUDE.md's "stop before bespoke" rule exists for.

**Cost of the recommendation (b), stated plainly:**
- WhatsApp's model-facing tool count becomes **3** (1 curated + 2 exempted), not 1. D-18's own
  rationale text ("the two curated forks expose 1 tool each") is now wrong for WhatsApp and must be
  corrected wherever it is repeated (46-01's TOOL-14 amendment prose, ROADMAP §46, D-27's own
  commentary) — the CODE (D-27's `≤3` ceiling) needs no change, only the worked example.
- `trustedRecipeActions[whatsAppRecipeSource]` becomes a MIXED key space **within one source**, not
  just across sources as D-35 anticipated: 2 entries stay raw-tool-name-keyed (`list_chats`,
  `list_messages`, read by `classifyToolRisk`'s existing `t.Name` lookup, unchanged mechanism) and 12
  become action-name-keyed (read by the new gateway classifier from `rawArgs`). No name collision
  exists between the two key spaces (verified: none of the 12 curated action names equals either
  exempted raw name), but D-35's comment ("calendar and whatsapp are ACTION-keyed... memory stays
  RAW-TOOL-NAME-keyed") is now imprecise for WhatsApp specifically and needs a third clause.
  46-08 is the plan that must write this correctly.
- Zero headroom: WhatsApp sits exactly AT the `≤3` ceiling. A fork change that adds one more
  standalone (non-merged) tool flips it to deferred with no code change — worth a mount-time WARN if
  46-08's author wants one, not required by this research.
- The merged tool's own description budget (D-36, target ~1.5-2KB) is computed over 12 actions
  instead of 14 — smaller, not larger, so the existing ~1.5-2KB target still holds; but it is no
  longer the ONLY always-loaded WhatsApp description paid every turn — `list_chats`'s and
  `list_messages`'s own descriptions (already individually capped at 4,096B by `frameMCPDescription`,
  observed live this session at roughly 300-800B each) are ALSO paid every turn now, since both stay
  always-loaded. Total is still well under budget; 46-02/46-08 should say so explicitly rather than
  leave the reader to infer it.
- **UNVERIFIED — planner must confirm:** the exact current byte length of `list_chats`'s and
  `list_messages`' JSON descriptions as served (this session read their text but did not compute
  byte counts before the write-now instruction arrived).

**What does NOT change:** calendar (§3), D-19's flat-union shape, D-20's `accountId` fix, D-23's pin
mechanics, D-26's delete-not-unadvertise rule, D-27's code (only its worked example), D-32's atomic
commit-per-source rule.

## 3. Calendar side — stands, confirmed live this session

`tools/list` against `127.0.0.1:8093/` (the pre-curation-pin PIM sidecar, this session, before the
tag moved under `73764ea11`) returned exactly the 14 tools `keptCalendarTools` in
`calendar_integration_test.go` already names, **zero `_meta` fields on any of them**, and the
`initialize` handshake's `capabilities` block carried **no `resources` key at all** — unlike
WhatsApp's, which explicitly declares `resources: {listChanged:false, subscribe:false}`. A server
that never declares the resources capability has no `ui://` documents to read; calendar has zero MCP
Apps views. The views question above is exclusively a WhatsApp problem.

**What this means for 46-05 → 46-07:** unchanged in shape. They still curate 14 calendar actions into
one multiplexed tool, still fix `accountId` in the fork's schema (D-20), still land the tracer's one
atomic commit. The only adjustment is the STARTING pin: per the session note above, the pre-curation
baseline is now `ghcr.io/chetto1983/aura-pim-mcp:10383276961828bc19f34a9372ba2c64a14e2b62`
(40-hex, committed in `73764ea11`), not the floating `:sidecar` 46-06 originally expected to read
and pin itself — 46-06's OWN job (curate, publish, re-pin to the NEW post-curation commit) is
unaffected; it simply starts one step further along than originally planned, and its task should say
so rather than re-discover the pin already exists.

## 4. `sha-<7hex>` vs 40-hex — the must_have rewording

Confirmed this session directly from both forks' workflow YAML (not inferred from HALT prose):

- **`chetto1983/whatsapp-mcp`'s `publish-image.yml`** (triggers on push to `main`, not a dedicated
  Aura branch — the file's own header comment says so): `docker/metadata-action` tags include
  `type=sha` with no length override, which mints **`sha-<7-hex-char>`** (docker/metadata-action's
  documented short form) — never a 40-char SHA. This is architecturally different from calendar, not
  just differently formatted: WhatsApp curation commits land on `main` directly (there is no
  surviving Aura-only branch — `aura/cockpit-connect` is retired, fused into `main` 2026-07-01), and
  `main` pushes are what `publish-image.yml` reacts to.
- **`chetto1983/aura-pim-mcp`'s `aura-publish-image.yml`** (triggers on push to `aura/pim-sidecar`
  specifically): tags literally `ghcr.io/${{ github.repository }}:${{ github.sha }}` — the full
  40-hex commit SHA, verbatim. This is what `73764ea11` just used
  (`10383276961828bc19f34a9372ba2c64a14e2b62`, 40 chars).

**Rewording required:** 46-06's must_have (*"compose.yaml's AURA_PIM_MCP_IMAGE default is the
immutable ghcr.io/chetto1983/aura-pim-mcp:<40-hex-sha> tag"*) is **already correct as written** — no
change needed, and this session's commit already demonstrates the format working. 46-08's must_have
(*"compose.yaml's AURA_WHATSAPP_MCP_IMAGE default is the immutable ghcr.io/.../whatsapp-mcp:<40-hex-sha>
tag"*, and its verify script's regex `[0-9a-f]{40}`) is **wrong and must be reworded** — that pattern
can never match a WhatsApp tag. Replace with a pattern anchored on the `sha-` prefix and 7 hex chars,
e.g. `ghcr.io/chetto1983/whatsapp-mcp:sha-[0-9a-f]{7}` — matching the value `73764ea11` already
committed (`sha-e0b8345`). The general must_have language ("immutable tag pinned by commit") should
stay source-agnostic; only the concrete regex/example needs the fork-specific form.

## 5. Plan triage (46-01 … 46-09)

| Plan | Verdict | Why |
|---|---|---|
| 46-01 (PRD amendment batch) | **Survives** | Content is trust-posture/mechanism prose, not tool counts; TOOL-14's worked example should say "WhatsApp: 1 curated + 2 exempted = 3" rather than "1," a wording fix, not a blocker. |
| 46-02 (design doc + WhatsApp checkpoint) | **Needs rework** | Task 1's whole decision ("12 vs 14 actions, aura/cockpit-connect vs main") is moot — the mount target is settled (§0 above). Replace the checkpoint with the views-exemption decision (§2) and the naming sub-decision (calendar__calendar vs calendar__pim, still genuinely open, unaffected). Task 2's WhatsApp action table becomes 12 actions + a 2-tool exemption list, not 14. |
| 46-03 (REQUIREMENTS/ROADMAP clean rewrite) | **Survives, minor content care** | Depends only on 46-01's amendment numbers; must not bake "WhatsApp = 1 tool" into the clean prose it writes — say "two curated slots" (still true) rather than "two curated tools" (no longer true). |
| 46-04 (D-27 deferral arithmetic) | **Survives unchanged** | Pure count predicate, ≤3/cap-2, source-agnostic; 3 ≤ 3 still qualifies. No code change needed. |
| 46-05 (calendar fork curation) | **Survives unchanged** | Zero views on calendar (§3, confirmed live); starting pin is now the 40-hex tag from `73764ea11` rather than `:sidecar`, a starting-state note only. |
| 46-06 (calendar tracer: re-key, gate, pin, atomic commit) | **Survives unchanged** | Its 40-hex must_have is already correct (§4); unaffected by the views question entirely. |
| 46-07 (tracer gate: live E2E, calendar) | **Survives unchanged** | Calendar-only, no WhatsApp/views dependency beyond `depends_on: 46-06`. |
| 46-08 (WhatsApp fork curation + table + pin) | **Needs rework** | Fork target moves from `aura/cockpit-connect` to `main` (the branch is retired); action count is 12-merged + 2-exempted, not 14-merged; `trustedRecipeActions[whatsAppRecipeSource]` becomes a THREE-way mix (§2), not purely action-keyed as currently written; "both curated servers... exactly 1 model-facing tool" must_have is wrong for WhatsApp (it's 3); the 40-hex regex must become `sha-[0-9a-f]{7}` (§4); Task 1's "operator resolved this in 46-02's checkpoint" framing points at the now-moot 12-vs-14-branch decision and must point at the views-exemption decision instead. |
| 46-09 (SC#6, tripwires, retrieval-gate repair, phase close) | **Survives** | The retrieval-gate's `55 → ~27` math is unaffected either way — ALL of WhatsApp's tools end up always-loaded (not deferred) whether curated as 1 or split 1+2, so none of them were ever going to appear in the deferred-manifest fixture. SC#6/calculator work is fully independent of WhatsApp's shape. |

## 6. `io.modelcontextprotocol/ui` never declared — recorded, out of scope

Confirmed live this session, exactly as HALT §5 states: `internal/mcp/sdkclient.go:71` sends
`Capabilities: &sdkmcp.ClientCapabilities{}` (empty) with no extensions field. `AppsExtensionID`
(`internal/mcp/apps.go:23`, value `"io.modelcontextprotocol/ui"`) is defined and never referenced
anywhere else in non-test code — grepped this session, zero production call sites. `AppsClientSettings()`
(`apps.go:95`) likewise has no production caller (only `apps_test.go`). The views render anyway
because the server advertises `_meta.ui` unconditionally regardless of what the client declared — so
this is a missed negotiation promise (a server that saw the capability could trim its text output),
not a broken feature. **This is a separate item, not Phase 46 work** — Phase 46 does not touch
`sdkclient.go`, and nothing in the curated-surface design depends on Aura ever declaring the
extension.

## RESEARCH COMPLETE
