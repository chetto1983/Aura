# Curated MCP surface design: `aura-pim-mcp` + `whatsapp-mcp`

- **Original date:** 2026-08-17
- **Revised 2026-08-22** — after `.planning/phases/46-mcp-trust-and-facade/46-HALT-2026-08-22.md`
  halted execution before any plan commit landed. The 2026-08-17 edition of this document was never
  written (the plan that would have written it was replanned first); this is the first and only
  edition, but it carries the revision banner because plans 46-05, 46-06 and 46-08 already cite this
  exact filename and date, and a reader who finds that filename must know the content answers the
  *replanned* question, not the original one. See `.planning/phases/46-mcp-trust-and-facade/
  46-RESEARCH.md` (replan edition, 2026-08-22) for the research this document is written against.
- **Status:** Approved — operator decision recorded in §10, pending fork implementation (plans 46-05
  through 46-08).
- **Author:** Davide + Claude
- **Supersedes / relates to:** D-17..D-36 (`46-CONTEXT.md`); `docs/superpowers/specs/
  2026-06-16-calendar-pim-mcp-fork-design.md`, the fork-design precedent this document mirrors in
  shape; commit `73764ea11` ("Pin both MCP sidecars to the commit they were built from"), which
  settled the mount target this document builds against.

## 1. Background & motivation

Aura mounts two forked, trusted-recipe MCP sidecars: `aura-pim-mcp` (mail + calendar + contacts,
14 tools) and `whatsapp-mcp` (14 tools). MCP-04 requires each sidecar's surface to collapse into a
curated, **always-loaded** manifest slot. `bridgePolicy.defaultDeferred` returns `true`
unconditionally for every bridged tool today; D-27 replaces that with a count rule — a mounted
server exposing `<= 3` model-facing tools earns an always-loaded slot, capped globally at 2 slots.
Both sidecars, mounted raw at 14 tools apiece, are far over that threshold and stay deferred forever.
Curation therefore has to reduce each sidecar's *advertised* tool count into the budget, and — per
D-17, the organizing constraint of the whole phase — curation happens **in the fork**, never as an
Aura-side facade: every MCP server Aura ships is a fork Aura controls, so a problem solvable in the
server must not become a wrapper in the host.

This document is the in-repo contract the two fork commits implement (D-24): action names, risk
classes, the flat-argument-union schema shape, the ID-typing discipline, and MCP-05's `accountId`
fix. It mirrors the 2026-06-16 `aura-pim-mcp` fork-design precedent's ten-section shape, extended to
cover both forks in one place because they are curated by the same rule at the same time (D-25:
design doc first, then one fork-repo plan each, joined by the immutable image pin).

**What changed since the phase started (2026-08-17 -> 2026-08-22).** The question this document was
originally going to answer — "12 actions as served on `aura/cockpit-connect`, or 14 after merging
forward from `main`" — is moot. `aura/cockpit-connect` is **retired**: its one unique commit was
fused into `main` on 2026-07-01 (`4a8306206`), it sits 143 commits behind `main` today, and nothing
mounts it. A live re-measurement on 2026-08-22 (46-HALT) found the *running* WhatsApp sidecar already
serves 14 tools from `main`, and that two of those fourteen — `list_chats` and `list_messages` —
carry live MCP Apps views the operator uses today (`ui://whatsapp/chats.html`,
`ui://whatsapp/thread.html`). Both sidecars are now pinned by commit: `compose.yaml`'s
`AURA_PIM_MCP_IMAGE` default is `ghcr.io/chetto1983/aura-pim-mcp:
10383276961828bc19f34a9372ba2c64a14e2b62` (40-hex) and `AURA_WHATSAPP_MCP_IMAGE` is
`ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345` (`sha-<7hex>`) — both landed in commit `73764ea11`.
This revision re-derives the WhatsApp curated surface from the **served 14**, not a branch, and
records the operator's resolution of the views-vs-merge conflict the halt surfaced (§10).

## 2. Goals / Non-goals

### Goals

- Collapse calendar's 14 raw tools into **one** curated, always-loaded, multiplexed tool covering
  all 14 actions (D-18, unchanged by the halt).
- Collapse WhatsApp's 14 raw tools into **one** curated, always-loaded, multiplexed tool covering
  **12** actions, while keeping **`list_chats`** and **`list_messages`** registered as their own raw,
  advertised, always-loaded, non-Mutating tools. WhatsApp ends the phase at **3 model-facing tools**,
  not 1 — the D-18 rationale text ("the two curated forks expose 1 tool each") is amended for
  WhatsApp only, and only in its worked example; the underlying `<=3`-per-server / cap-2 arithmetic
  (D-27) needs no code change because 3 <= 3.
- Fix MCP-05 concretely: the calendar detail action accepts the same single opaque reference its
  listing action returns, and requires **no `accountId`**.
- Preserve every action's **existing** risk classification from `bridge_risk.go`'s
  `trustedRecipeActions` table exactly — merging registrations must never re-tier a single action.
- Land both curated descriptions inside a tight, explicitly stated byte budget (D-36), and state the
  WhatsApp exemption's added always-loaded cost as a number, not an inference.
- Preserve the view-callback grant `list_chats`/`list_messages` already carry today — the exemption
  protects a working, in-use operator capability, it does not create a new one.

### Non-goals

- **No Aura-side facade tool.** No new Go tool (`comms`, `pim`, or otherwise) wrapping either
  sidecar's raw surface. The curated tool the model sees is a schema **the fork itself registers**.
- **no curation config.** Nothing in Aura's tree — no file, table, flag, or env var — lists which raw
  tools a fork should keep, merge, or drop. That decision lives in the fork's own source.
- **no hide-list.** No Aura-side allow/deny list of raw tool names, and no per-integration
  namespace-keyed policy table (superseded, D-17; this also supersedes research §2.3's original
  Go-table recommendation). To restate both together plainly: this design specifies no Aura-side
  facade tool, no hide-list, no curation config, and no per-integration namespace table.
- **No `bridgePolicy` generalization into a namespace-keyed configuration surface.** The bridge stays
  generic: the same mount/risk/deferral/view machinery that handles these two forks handles any
  future self-minted or ad hoc server, with zero Aura code added per server.
- The admin REST surface (`internal/agui/connect_pim_api.go`, the cockpit's PIM "Connect account"
  proxy) is **untouched** — it is not MCP, and this curation does not reach it.
- The provider-call implementation layer inside both forks (the actual Graph/Gmail/IMAP calls in
  `aura-pim-mcp`, the actual `whatsmeow` calls in `whatsapp-mcp`) is **untouched** — only the
  registration/dispatch layer that exposes those calls as MCP tools collapses.
- **The views exemption is a decision about what the FORK registers, not an Aura-side allow-list.**
  D-17 still holds in full: Aura's host applies the exact same generic risk-classification,
  deferral-counting, and view-rendering machinery to all three WhatsApp tools that it would apply to
  any other mounted server's tools. Nothing Aura-side names `list_chats` or `list_messages`
  specifically; the fork's own `tools/list` response is what makes them exist.

## 3. Architecture

Both sidecars keep their existing shape: loopback-bound streamable-HTTP services (like the existing
`agent-memory` `:8091` and embed `:8081` sidecars), mounted trusted-recipe via
`mcptools.MountManagedServer`. Curation changes what each sidecar *advertises*, not how Aura reaches
it.

```
Aura agent loop
   |  mcptools.MountManagedServer (streamable-HTTP, TrustTrustedRecipe)
   v
aura-pim-mcp     127.0.0.1:8093/      -> calendar__calendar(action=...)    [1 model-facing tool, 14 actions]
whatsapp-mcp     127.0.0.1:8092/mcp   -> whatsapp__messages(action=...)    [1 model-facing tool, 12 actions]
                                      -> whatsapp__list_chats              [raw, read, ui://whatsapp/chats.html]
                                      -> whatsapp__list_messages           [raw, read, ui://whatsapp/thread.html]
```

Once mounted, a curated tool is handled by Aura's *existing* Multiplexed-tool machinery — the same
path `task` and `skill_manage` already use: `trustedRecipeActions` re-keyed to action names (D-21),
`Multiplexed: true` set only where a classifier already exists (D-34), and one entry per curated tool
in `multiplexedClassifiers` (`internal/gateway/classify.go`). The two exempted WhatsApp tools stay on
the **existing, unmodified** raw-tool-name path — `classifyToolRisk`'s current `t.Name` lookup keeps
working for them unchanged, because they are not multiplexed at all.

## 4. Thin-fork changes

| Fork | Edit site | Branch | What collapses | Tag format |
|---|---|---|---|---|
| `chetto1983/aura-pim-mcp` | `src/CalendarMcp.HttpServer/Program.cs` (the `.WithTools<...>()` registration block; `app.MapMcp()` serves whatever is registered there) | **`aura/pim-sidecar`** | 14 `.WithTools<X>()` registrations -> 1 curated tool's registration, `action`-dispatched | Full 40-hex `github.sha`. `aura-publish-image.yml` (triggers on push to `aura/pim-sidecar`) tags `ghcr.io/${{ github.repository }}:${{ github.sha }}` verbatim. |
| `chetto1983/whatsapp-mcp` | `whatsapp-mcp-server/main.py` (the `@mcp.tool()` decorators; `whatsapp.py`'s provider calls are unchanged) | **`main`** | 14 `@mcp.tool()` decorators -> 1 curated `messages` decorator (12 actions) + 2 unmerged decorators (`list_chats`, `list_messages`, left exactly as they are today) | `sha-<7hex>`. `publish-image.yml` (triggers on push to `main`) uses `docker/metadata-action`'s `type=sha` with no length override, which mints the short `sha-` form, never a 40-hex tag. |

**`aura/cockpit-connect` is retired.** It was fused into `main` on 2026-07-01 (`4a8306206`) and is
143 commits behind, 1 commit ahead (that one commit already merged forward). Nothing mounts it, no
CI job builds it, and this document gives no instruction to push curation commits there — every
WhatsApp curation commit lands on `main` directly.

**The 28 raw handlers this document curates away are DELETED, not merely unadvertised (D-26).** An
unadvertised-but-still-callable handler is dark code (forbidden by CLAUDE.md) and stays reachable by
anything holding the sidecar's HTTP port — Aura's namespacing protects the *model's* surface, not the
sidecar's own listener. Once the curated tool's action branches are the only path to a given provider
call, the standalone handler is removed from the fork's source, not left registered-but-hidden.

**The two exempted WhatsApp tools are the opposite case.** `list_chats` and `list_messages` stay
**advertised** — visible to the operator, to `tools/list`, and to Aura's risk table — precisely
because an unadvertised reachable tool is what D-26 forbids, and these two are meant to be reachable.
Curation here means "left alone," not "hidden."

## 5. The two curated surfaces

### 5a. Calendar — one curated tool, 14 actions, no exemptions

Calendar declares no `resources` capability at all (confirmed live 2026-08-22 against
`aura-pim-mcp:10383276961828bc19f34a9372ba2c64a14e2b62`'s `initialize` handshake) and has zero MCP
Apps views, so there is no views question on this side — all 14 actions merge into one tool.

| action | risk class | required arguments (curated) | underlying provider call |
|---|---|---|---|
| `list_accounts` | read | — | list connected mail/calendar/contact accounts |
| `get_emails` | read | (`accountId` optional) | list emails, optionally scoped to one account |
| `get_email_details` | read | `accountId`, `emailId` | full email body/headers for one message |
| `search_emails` | read | `query` (`accountId` optional) | search emails by query |
| `list_calendars` | read | (`accountId` optional) | list a user's calendars |
| `get_calendar_events` | read | `timeZone` (`accountId` optional) | list events in a window; returns each event's `eventId` |
| `get_calendar_event_details` | read | `timeZone`, `calendarId`, `eventId` | full event detail (attendees, free/busy, recurrence, meeting link) — **MCP-05 fix, see below** |
| `get_contacts` | read | — | list contacts |
| `search_contacts` | read | `query` | search contacts |
| `get_contact_details` | read | `accountId`, `contactId` | full contact detail |
| `create_event` | mutate | `subject`, `start`, `end` (`accountId` optional/defaultable) | create a calendar event |
| `update_event` | mutate | `accountId`, `calendarId`, `eventId` | update an existing event |
| `respond_to_event` | destructive | `eventId`, `response` | accept/decline/tentatively-accept an invite |
| `send_email` | destructive | `to`, `subject` | send an email |

Classes: 10 read, 2 mutate (`create_event`, `update_event`), 2 destructive (`respond_to_event`,
`send_email`) — read directly off `bridge_risk.go`'s live `trustedRecipeActions[calendarRecipeSource]`
table on 2026-08-22 and reproduced here **unchanged**; the merge re-registers these 14 actions, it
does not re-tier a single one of them.

**MCP-05's fix, stated concretely.** Today `accountId` is two different things sharing one name:
a defaultable **routing hint** in `create_event`/`get_calendar_events`, and a required **opaque
handle** in `get_calendar_event_details`, documented upstream as "Account ID from
`get_calendar_events`" — a host that injects a configured default into the handle case passes the
*wrong* account, because it is not actually an account identifier at all, it is a reference. The
curated `get_calendar_event_details` action drops `accountId` entirely and instead accepts the same
`eventId` its own listing action (`get_calendar_events`) already returns per event, together with
`calendarId` (unchanged) and `timeZone` — three fields, none of them `accountId`. `eventId`'s value is
**opaque to Aura**: Aura passes back byte-for-byte whatever the fork issued in the listing response
and never re-cases, normalizes, or re-encodes it, the same discipline `prd.md`'s document citation
token (`document:<search_document_id>@<version_number>#<locator>`, `prd.md:4579`) already uses for an
opaque reference minted by one call and consumed by another.

### 5b. WhatsApp — one curated tool (12 actions) + a 2-tool exemption

| action | risk class | required arguments (curated) | underlying provider call |
|---|---|---|---|
| `search_contacts` | read | `query` | search WhatsApp contacts |
| `get_contact` | read | — | look up a contact |
| `get_chat` | read | `chatId` | get one chat's metadata |
| `get_direct_chat_by_contact` | read | `phoneNumber` | find the 1:1 chat for a phone number |
| `get_contact_chats` | read | `contactId` | list chats a contact appears in |
| `get_last_interaction` | read | `contactId` | most recent message with a contact |
| `get_message_context` | read | `messageId` | messages around a given message |
| `download_media` | mutate | `chatId`, `messageId` | fetch a message's attached media to local disk |
| `send_message` | destructive | `recipient`, `message` | send a text message |
| `send_reaction` | destructive | `recipient`, `messageId`, `emoji` | react to a message |
| `send_file` | destructive | `recipient`, `mediaPath` | send a file attachment |
| `send_audio_message` | destructive | `recipient`, `mediaPath` | send a voice/audio message |

Classes: 7 read, 1 mutate (`download_media`), 4 destructive (`send_message`, `send_reaction`,
`send_file`, `send_audio_message`) — matching `bridge_risk.go`'s live table exactly (the table was
never stale; the served 14 match it tool-for-tool, per 46-HALT §2).

**Exemption table** — these two are **not** in the curated tool's `action` enum. They stay their own
registered, advertised, always-loaded, non-Mutating tools:

| tool (raw name, unchanged) | risk class | `ui://` binding | why exempted |
|---|---|---|---|
| `list_chats` | read | `ui://whatsapp/chats.html` | live MCP Apps view, rendered and used today |
| `list_messages` | read | `ui://whatsapp/thread.html` | live MCP Apps view, rendered and used today |

**Why these two are exempted rather than merged (the operator's Task 1 decision — see §10 for the
verbatim record).** Both view documents are catalogued and render today (`GET /api/mcp/view?...
thread.html` -> 200, 27,733 bytes of armed HTML). A rendered view's read-back calls
(`POST /api/mcp/view/call {tool:list_chats}`) return 200 with real rows. A merged
`messages(action=...)` tool must be `Mutating` (it has to cover `send_message`), and
`CallReadOnlyTool`'s gate (`bridge_supervisor.go:337-344`, `toolIsReadOnly`) refuses any Mutating
tool's view callback — measured live: `POST /api/mcp/view/call {tool:send_message}` -> **403 `tool
call refused`**. Worse, `toolIsReadOnly` looks the call up by the **raw mounted-server tool name**
(`s.bridged[name]`), and the view's own embedded JS calls `list_chats`/`list_messages` **by that raw
name** — so after D-26 deletes the raw handlers, the view's callback would not merely be refused, it
would find no tool by that name at **all**. Exempting the two tools avoids both failures: they stay
raw, stay advertised, stay non-Mutating, and their view callbacks keep passing `toolIsReadOnly`
exactly as they do today.

**A third candidate — a per-result view binding, so one curated tool could still point at two
documents — was investigated and does not exist.** `bridgedTool` (`internal/agent/mcptools/
bridge.go`) carries exactly one `view mcp.ViewRef` field, set once at construction from the raw
mounted tool and deliberately never refreshed (`bridge_views.go:35-60`, `viewRefFor`). `ViewRef`
itself is documented as what a **tool** carries, read once from that tool's own `_meta.ui` in
`tools/list` — there is no field, map, or parse path anywhere in the bridge that reads a view
reference off a *call result* rather than the static tool definition. The protocol agrees: SEP-1865
(status **Final**) binds the view on the **tool** (`Tool._meta.ui`), and its own
Extensibility -> "Future Considerations" section lists, verbatim, *"Support multiple UI resources in
a tool response"* as **not** part of the finalized spec. Building a per-result mechanism would mean
inventing a protocol extension Aura's forks and Aura's host would both have to speak alone — exactly
the bespoke-protocol trap CLAUDE.md's "stop before bespoke" rule exists for. This candidate is
recorded as **investigated-and-absent**, not offered as an implementable option.

### 5c. Flat-union argument shape (both curated tools, D-19)

Both curated tools use the same shape Aura's own multiplexed tools already use (`task`,
`skill_manage`): **one flat object**, `action` plus every field any action needs, all optional at the
schema root except `action` itself, with each field's per-action requirement stated in that field's
own `description` rather than enforced by a root-level union. There is **no root `oneOf`/`anyOf`** —
SEP-2106 makes a discriminated-union `oneOf` newly possible, but provider `oneOf` support is uneven
and Aura's own `task`/`skill_manage` tools already prove the flat shape works; D-19 stands.

Illustrative shape for the calendar tool (WhatsApp's `messages` tool mirrors this exactly, with its
own 12-member `action` enum):

```json
{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["list_accounts", "get_emails", "..."],
               "description": "The calendar operation."},
    "accountId": {"type": "string", "description": "Optional routing hint for get_emails, list_calendars, get_calendar_events, create_event. NOT used by get_calendar_event_details (see eventId)."},
    "calendarId": {"type": "string", "description": "Required for get_calendar_event_details and update_event."},
    "eventId": {"type": "string", "description": "Required for get_calendar_event_details, update_event, respond_to_event. Opaque -- pass back byte-for-byte what get_calendar_events returned."},
    "emailId": {"type": "string", "description": "Required for get_email_details."},
    "contactId": {"type": "string", "description": "Required for get_contact_details."}
  },
  "required": ["action"]
}
```

The schema's only required field, root-wide, is `action` — `required: ["action"]` — never a
per-action-conditional root schema and never a root `oneOf`/`anyOf` discriminated union.

**ID discipline: never a bare `id`.** Both curated schemas name their identifiers by what they
identify — `eventId` and `emailId` on the calendar side, `chatId` and `messageId` on the WhatsApp
side (plus `contactId` and `phoneNumber` where the raw fork used an untyped `jid`/phone string) — so
an ambiguous bare `id` argument never appears in either schema.

### 5d. The description budget — numbers, not an inference

`frameMCPDescription` caps **any single description at 4,096 bytes** (`maxMCPDescriptionBytes = 4096`,
the hard cap); `capSchemaDescriptions`
empties a schema entirely above 16KB total or 128 properties. **Target ~1.5-2KB** for each curated
tool's own top-level description: a short preamble plus one line per action naming its required
fields; per-field detail lives in the schema's own argument descriptions (capped at 512 bytes each,
read by the model only when it inspects arguments, not paid every turn).

**Under the WhatsApp exemption, the curated `messages` tool is no longer the only always-loaded
WhatsApp description** — `list_chats` and `list_messages` are now also always-loaded, and their
descriptions are paid every turn too. **Measured 2026-08-22** directly via `tools/list` against the
pinned `whatsapp-mcp:sha-e0b8345` on `127.0.0.1:8092/mcp` (protocolVersion `2025-06-18`, 14 tools,
`annotations` empty on all 14):

- `list_messages` description = **997 bytes** (input schema 1,018 bytes)
- `list_chats` description = **399 bytes** (input schema 456 bytes)
- **997 + 399 = 1,396 bytes** — the exemption's added always-loaded cost, on top of the curated
  `messages` tool's own ~1.5-2KB.

So WhatsApp's total standing always-loaded description cost becomes roughly
**~1.5-2KB + 1,396 bytes ~= 2.9-3.4KB**, against calendar's ~1.5-2KB (calendar has no exempted
extras) — both curated slots together land near 5KB, comfortably inside D-36's "roughly 2k
tokens/turn for both slots" target and far under the 6,346 bytes the full raw 14-tool WhatsApp
surface would cost if it were ever all loaded at once. **Re-measure both numbers if the WhatsApp pin
moves past `sha-e0b8345`**, and state whichever pin the new numbers were taken from.

## 6. Aura-side integration

**Model-facing names — the operator's Task 1(B) decision (verbatim record in §10).** Both mount
namespaces come from `internal/mcp/manager/catalog.go`'s `BuiltInCatalog` entries: `Name: "calendar"`
(`Source: "recipe:calendar"`) and `Name: "whatsapp"` (`Source: "recipe:whatsapp"`).
`internal/agent/mcptools/name.go`'s `namespacedName(namespace, tool)` builds `<namespace>__<tool>`.
D-18 names the curated tools `calendar` and `messages`; taken literally through `namespacedName` that
yields:

- **`calendar__calendar`** — the calendar fork's curated tool.
- **`whatsapp__messages`** — the WhatsApp fork's curated tool.

These are the exact `multiplexedClassifiers` map keys and must appear byte-for-byte in every later
plan's classifier registration, name constant, and integration-test expectation. `whatsapp__list_chats`
and `whatsapp__list_messages` keep their existing namespaced names unchanged (they are not
multiplexed, so they need no classifier entry at all).

**`trustedRecipeActions` ends this phase with THREE key spaces, not two** (subtler than D-35's
original two-way framing — this document is what corrects that):

| source | key space | read by |
|---|---|---|
| `recipe:calendar` | action names (all 14) | the gateway classifier, from the multiplexed call's `rawArgs.action` |
| `recipe:whatsapp` | **MIXED, within one source:** 12 action names (the curated `messages` tool) + 2 raw tool names (`list_chats`, `list_messages`) | the gateway classifier for the 12 action names; `classifyToolRisk`'s existing `t.Name` lookup, unchanged, for the 2 raw names |
| `recipe:memory` | raw tool names | `classifyToolRisk`'s `t.Name` lookup, unchanged |

The two WhatsApp key spaces are **disjoint** — none of the 12 curated action names equals either
exempted raw tool name (verified against the live table above) — and a unit test must assert that
disjointness, because a collision would silently make one table entry answer two different
questions depending on which lookup path reached it first.

**The mount-time reconciliation predicate.** D-33 WARNs at mount in both directions when the fork's
served surface and the Go table disagree. Under the exemption, naively checking "every table entry
the server currently advertises as a tool name" would fire on `list_chats` and `list_messages` at
**every single mount**, because they are genuine table entries that are *not* members of the curated
tool's `action` enum. The correct predicate, stated once so 46-06/46-08 implement it identically: a
`trustedRecipeActions` entry is **accounted-for** when it is **EITHER** a member of the curated
tool's `action` enum **OR** the name of a tool the mounted server currently advertises. This
predicate is derived entirely from what the server serves at mount time, so it introduces no
Aura-side list of names and D-17 still holds.

**This document's action lists are what the fork commits and the Go table are both written
against; a divergence between them is caught by the mount-time reconciliation above, not by this
document.** Neither this document nor its authors can prove the fork's eventual source code matches
these tables — that proof is D-33's WARN-at-mount, exercised live by 46-06's and 46-08's own
validation.

## 7. Security & policy

**The view-callback grant, stated explicitly because the exemption is what keeps it alive.** A
rendered `ui://` document may call back **only** into the server that served it
(`mcp.ViewCallers.CallForView`), **only** into a tool that is currently advertised **and**
non-Mutating (`bridge_supervisor.go:337-344`, `CallReadOnlyTool` -> `toolIsReadOnly`, fail-closed on
an unknown name), with **no agent turn and no human in the loop** for that callback. Under the
exemption this grant covers exactly `list_chats` and `list_messages`, both classed read, **unchanged
from today** — the exemption *preserves* an existing grant, it does not widen one. `viewRefFor`
(`bridge_views.go:35-60`) is gated on the mount's trust class (`policy.views`), never on a
per-tool allow-list, so nothing here is WhatsApp-specific Aura code.

**Every action in both curated tables, and both exempted raw tools, keeps its existing risk class.**
No re-tiering happens as part of this merge — the classes in §5a/§5b are copied from
`bridge_risk.go`'s live table, not invented. `mcpToolRisk`'s fail-closed default (nil
`Annotations` -> `true, true`) and the 45.1 escalate-only `unsafeToRepeatBeyondAura` branch remain the
backstop for anything this document's tables do not name.

**The opaque calendar reference (MCP-05) is issued by the fork and passed back byte-for-byte** — Aura
never synthesizes, defaults, or re-encodes it. This is exactly why the earlier host-side-injection
idea was rejected: a host default would pass the wrong account, since the value was never really an
account identifier to begin with.

## 8. Validation plan

1. **Each fork's own CI green before push** — this document specifies the target surface; it cannot
   prove the fork's actual source matches it. That proof is the fork repository's own CI, which no
   Aura pipeline can reach across the repo boundary. Fork-CI-green is a **precondition recorded in the
   phase SUMMARY**, not something Aura's pipeline asserts.
2. **The published immutable tag is the artifact.** `aura-pim-mcp` publishes a 40-hex `github.sha`
   tag (`aura-publish-image.yml`); `whatsapp-mcp` publishes a `sha-<7hex>` tag (`publish-image.yml`,
   `type=sha`). `compose.yaml`'s two `AURA_*_MCP_IMAGE` defaults move to the new post-curation tags in
   the single atomic commit D-32 describes (pin + re-keyed table + `Multiplexed` + classifier entry,
   all together — no intermediate state where the table expects actions the mounted image does not
   yet serve, or vice versa).
3. **From Aura's side:** the `calendar_integration` and `whatsapp_integration` build-tagged tests
   assert the expected model-facing tool **count** per sidecar — **1 for calendar, 3 for WhatsApp**
   under this exemption — and the expected `action` enum for each curated tool.
4. **Live E2E (D-37):** one driven conversation through the real agent that reads a calendar, sends
   something that trips the approval gate, and follows a calendar event from listing through to
   detail using the new opaque reference — quoted against `aura.tool_invocations` rows in the
   phase's `VALIDATION.md`. A live smoke test alone is not the green signal (`e2e-real-not-smoke`).

## 9. Risks & open items

- **COMPAT-01/02/03 (assigned to Phases 47/48, not this phase).** D-26 deletes 28 registered raw tool
  names. Persisted `aura.tool_invocations` rows, any paused approval, and any scheduled `agent_job`
  that resolves a tool by name at fire time have nothing to resolve against once the image is pinned
  post-curation. This phase does not own COMPAT-01 (rehydrated history), COMPAT-02 (paused
  approvals), or COMPAT-03 (scheduled jobs) — it must not silently absorb them, and it must not blow
  them up unnoticed either. The blast radius is recorded here for Phases 47/48 to pick up.
- **Zero headroom under WhatsApp's `<=3` ceiling.** With the exemption, WhatsApp sits at **exactly 3**
  model-facing tools against D-27's `<=3` cap. One further standalone (non-merged) tool added to the
  fork in the future silently flips the *whole server* to deferred, with no Aura code change and no
  warning today. A mount-time WARN when a trusted-recipe server's model-facing count crosses back
  over its own prior value is a reasonable mitigation plan 46-08 may add; it is not required by this
  document.
- **`io.modelcontextprotocol/ui` is never declared by Aura's client** (`internal/mcp/sdkclient.go:71`
  sends empty `ClientCapabilities`; `mcp.AppsClientSettings()` has no production caller). The views
  render anyway because the server advertises `_meta.ui` unconditionally regardless of what the
  client declared, so this is a missed negotiation promise (a server that saw the capability could
  trim its text output for a client that can't render it), not a broken feature. **Explicitly out of
  Phase 46's scope** — recorded here so it is not mistaken for something this phase forgot.

## 10. Decisions log

**Task 1(A) — WhatsApp's two view-bound tools: exempt or curate away. SELECTED: `views-exempt`.**

> (A) RECOMMENDED — Exempt `list_chats` and `list_messages`: curate 12 actions, keep 2 raw tools.
> WhatsApp ends at 3 model-facing tools.

Rejected alternative, `views-drop` ("curate all 14 actions into one tool and let the views die"), and
its stated cost, quoted verbatim from the plan's own option text: *"Destroys a working, in-use
capability, measured live on an authenticated session this week — and buys nothing the exemption does
not also buy, since 3 <= 3 either way. The view documents ship inside the fork image and keep being
served by `resources/list`, so the operator sees a UI that renders and then refuses every
interaction: a 403 on the callback path, and a not-found once the raw names are deleted. Reversal
costs a second fork commit + publish + pin."*

The per-result-view candidate was investigated and is recorded as **INVESTIGATED-AND-ABSENT** (§5b):
`bridgedTool` carries exactly one `mcp.ViewRef`, and SEP-1865 (status Final) lists "Support multiple
UI resources in a tool response" under its own Future Considerations, i.e. explicitly not part of the
finalized spec. It was not offered as a selectable option.

**Task 1(B) — the model-facing curated tool names. SELECTED: `name-keep-d18`.**

> (B) Keep D-18's names: model-facing `calendar__calendar` and `whatsapp__messages`.

Rejected alternative, `name-pim` ("rename the calendar fork's curated tool to `pim`: model-facing
`calendar__pim` and `whatsapp__messages`"), and its stated cost, quoted verbatim: *"Diverges from
D-18's literal wording; `pim` inside a `calendar` namespace is its own small confusion."*

**The 2026-08-17 branch question is SETTLED, not silently dropped.** The original "12 actions on
`aura/cockpit-connect` vs. 14 after merging from `main`" decision this document was originally
supposed to record is moot: `aura/cockpit-connect` is retired (fused into `main` 2026-07-01), nothing
mounts it, and the mount target for both sidecars was settled by measurement and commit `73764ea11`
before this document was written. It is recorded here as settled-by-measurement so a later reader
does not mistake its disappearance from this document for an oversight.

**Carried-forward decisions (D-17..D-36), one line each with the rejected alternative:**

| Decision | Choice | Rejected alternative |
|---|---|---|
| D-17 | Curation lives in the fork; zero Aura-side facade/hide-list/config | An Aura-side `comms`/namespace-policy table (superseded research §2.3) |
| D-18 | One multiplexed tool per sidecar (amended for WhatsApp: +2 exempted raw tools) | ~8-10 discrete verbs per sidecar; one tool spanning both sidecars |
| D-19 | Flat-union `action` schema, typed IDs, never bare `id` | A `oneOf` discriminated union (newly possible under SEP-2106, still rejected: uneven provider support) |
| D-20 | MCP-05 fixed in the fork's own schema (opaque `eventId`, no `accountId`) | Host-side injection of a configured default account (passes the wrong account) |
| D-23 | Pin both sidecars by immutable `:<sha>` tag | Floating `:sidecar` tag (proven unsafe: it moved under the PIM fork the same afternoon); `@sha256` digest (more ceremony than needed) |
| D-26 | Delete the curated raw handlers in the fork, never merely unadvertise | Leaving them registered-but-hidden (dark code, still reachable on the sidecar's own port) |
| D-27 | `<=3` model-facing tools per server earns an always-loaded slot, global cap 2 | N=1 (brittle: a fork splitting one verb into two falls off the cliff) |
| D-32 | Fork publishes first; one atomic Aura commit (pin + re-key + `Multiplexed` + classifier) | A dual-key transition table with a forgettable cleanup commit |
| D-35 | `trustedRecipeActions` stays one table with mixed keys, documented — amended here to a THREE-key-space description (§6), not the original two-way framing | A second risk-source table per key shape |
| D-36 | Merged description target ~1.5-2KB; per-field detail in 512B argument descriptions | Using the full 4,096B allowance (standing cost, paid every turn) |
