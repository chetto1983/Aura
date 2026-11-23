# The PIM's tools, one per operation again

- **Date:** 2026-11-23
- **Status:** Draft, for the operator's approval
- **Repositories:** `chetto1983/aura-pim-mcp` (the fork) and `chetto1983/Aura`
- **Supersedes:** in `2026-08-17-mcp-curated-surface-design.md`, D-18 for the PIM (one multiplexed tool),
  D-19 (flat-union `action` schema), D-26 as applied to the PIM's raw handlers, and the calendar half of
  D-35's mixed-key risk table. D-17, D-20, D-23 and D-32 stand and are reused below.
- **Inventory this rests on:** the sweep of 2026-11-23 (this session), summarised in §9.

## 1. Why

The fork collapsed upstream's per-operation tools into one `calendar` tool on 2026-08-22 (`38c94fd`), so the
PIM would fit MCP-04's always-loaded slot (at most 3 tools per server). That reason is gone:

- since 2026-10-03 the tool is never loaded (`bridgePolicy.neverLoaded`, `bridge_deferral.go`). It was 13,136
  of a real turn's 43,403 manifest characters. The model fetches it through `tool_search` on every use;
- what is left of the collapse is its cost: one schema of 48 flat, optional, nullable parameters for 29
  operations. Measured on the lab VM on 2026-11-23, the model put `toDate` (a `search_emails` field) into
  `send_email`, put an account id under `emailId`, and followed the shared `accountId` description, which
  told it to omit the account on a send. `63365df` fixed that one sentence, not the shape that invites the
  rest.

Upstream (MarimerLLC/Adjutant 2.0.0) still ships one tool per operation, each with only its own parameters:
`send_email` has nine. Aura already mounts a trusted recipe as plain tools: WhatsApp's tools are deferred and
action-free.

## 2. Goals, non-goals, anti-goals

**Goals**
- The PIM advertises one tool per operation, the 29 operations the curated tool covers today, under their
  current action names (`send_email`, `get_emails`, `create_event`, …). The model sees `calendar__send_email`
  and so on.
- Each tool's schema holds only its own parameters, with its own required list.
- Every behaviour the fork added to the curated tool survives on the tool it belongs to (§4.2).
- Aura keys everything on the tool name. The `action` discriminator disappears from the bridge, the gateway,
  message drafts, display and host-built calls.
- The upgrade keeps what operators stored: standing grants, ask/deny policies, pending and scheduled email
  drafts, and the tables of past conversations.
- Aura and the PIM image ship as a pair. Neither runs against the other's old shape.

**Non-goals**
- Changing what any operation does with a provider. The upstream classes stay the implementation.
- New operations. MailCraft's rows (drafts, threading, series) come after, on the new shape.
- The WhatsApp sidecar's remaining multiplexed tool.
- Renaming the `calendar` namespace or the `recipe:calendar` source.
- Prompts and skills an operator or the agent has already stored on an appliance. Only the texts shipped
  in this repository are rewritten (§5.6). *(My call.)*

**Anti-goals: these would be failures even if the tools work**
- A window in which an email send skips the message-draft review, or runs under the generic Destructive
  prompt.
- A past conversation that loses its PIM tables on replay.
- An operator's deny policy on sending mail that silently stops applying.
- Any manual step on an appliance. The updater must converge it (operator rule, 2026-09-14).

## 3. Approaches considered

| | Approach | Trade-off | Verdict |
|---|---|---|---|
| 1 | Full split in one release: the fork publishes the per-operation tools, then one Aura release carries the image pin, the re-keying and the data migration | Several days across two repositories; the cleanest end state, with no dual key space | **Chosen by the operator, 2026-11-23** |
| 2 | Split mail first, calendar and contacts later | Smaller first delivery, but the multiplex machinery and the tool-keyed path coexist until the end, and the draft migration is needed in the first step anyway | Rejected: more total work, two shapes to reason about |
| 3 | Keep one tool, publish a `oneOf` schema per action | No Aura change at all | Rejected: D-19 already found provider support for `oneOf` uneven. It would need a spike across every route, and it keeps the gateway's action parsing |

An Aura-side facade that splits the tool on the host is ruled out by D-17: curation lives in the fork.

## 4. The fork

### 4.1 Surface

- Register the 29 upstream tool classes the curated tool forwards to today, under the names the action
  enum uses now. Two differ from the name the SDK derives from upstream's method: `mark_email_read` and
  `bulk_mark_emails_read` (upstream `…_as_read`). The tools take the action names, so Aura's stored rows
  and texts map one to one, and each name is set explicitly on its `[McpServerTool]`. Delete
  `CalendarActionTool` and its dispatch. No handler stays registered-but-hidden: this is D-26's rule,
  applied now to the dispatcher.
- `WithCalendarMcpSurface()` stays the one registration chain for both hosts and the tests, so a
  `Program.cs` merge cannot drop part of it.
- The tenant binding moves out of the dispatcher. Today `CalendarActionTool` binds the tenant from the
  caller's principal on every call (`CalendarActionTool.cs:225`). It becomes a call-tool filter registered
  in the same chain, so it covers every tool and no tool can be added without it. A test calls each of the
  29 tools as one tenant and sees none of another tenant's accounts.
- MCP annotations stay as upstream declares them. They do not decide risk in Aura: a `recipe:calendar`
  tool's class comes from Aura's own table (§5.1).
- The string-scalar coercion filter (`StringArgumentCoercion`) is per tool already and is unchanged.

### 4.2 Fork behaviours that move onto single tools

| Behaviour, today in the curated tool | Lands on |
|---|---|
| Opaque event references: `get_calendar_events` and `create_event` return `eventId = EventRef(account, id)`; the detail, update, delete and respond operations take that reference and no `accountId` (D-20) | those six tools' own schemas and results |
| `send_email` requires `accountId` (`63365df`) | `send_email`'s schema: `accountId` required, no domain routing |
| `get_email_attachment` answers with an `attachment://` resource link plus an id | `get_email_attachment` |
| The MCP Apps view `ui://calendar/view.html` | `_meta.ui` on each read tool whose result the view renders. The view calls the per-tool names |
| The tool-level description (one 1.5–2 KB text, D-36) | one short description per tool. Per-field detail stays in argument descriptions |

Modifying upstream classes (for example `SendEmailTool`'s `accountId`) is now the fork's way. `AURA-FORK.md`
records each one, so a sync knows to keep it.

### 4.3 Contract and publishing

- A contract test lists exactly the 29 names and, per tool, its required fields.
- A live check against the built image does the same.
- Images are tagged by immutable `:<sha>` (D-23). The floating tags may stay for humans, but Aura pins the
  sha.

## 5. Aura

### 5.1 Bridge and risk

- Delete the multiplex registry for the PIM: `CalendarMultiplexedToolName`, `MCPActionClassFor`,
  `reconcileCuratedActions`, and `Multiplexed` for `calendar`.
- `trustedRecipeActions["recipe:calendar"]` becomes tool-keyed. The keys already equal the upstream names.
- A `recipe:calendar` tool missing from the table stays fail-closed: Destructive, never the annotation path.
  This keeps today's floor for an unknown action. Drift detection moves from the action enum to the tool
  name set.
- Read tools become non-mutating. That is the point: they leave the operation-reservation funnel that once
  denied briefing reads. They become callable from the MCP Apps view's read-only callbacks.
- `neverLoaded` for `recipe:calendar` stays: about 29 tools is above the slot rule anyway.

### 5.2 Gateway

- Remove `classifyCalendarAction` and its entry in `multiplexedClassifiers`. Tiers come from the spec bits,
  as for every other tool.
- Grant and policy subjects become tool-only (`calendar__send_email`).
- The same holds under an alias (`<alias>__send_email`). The inventory found that today an alias loses the
  per-action classifier and subject altogether; the split closes that gap.

### 5.3 Message drafts and host-built calls

- The email target becomes `{recipe: recipe:calendar, tool: send_email, action: ""}`. `TrustedOutboundTarget`
  no longer parses the arguments to find it.
- `action` leaves the email field allow-list and `validateEmail`. `InlineReview().Complete` keys on the tool.
- Host-built calls drop `action` and name the tool:
  - `task_message.recipeArgs` and `cron/notify_email` → `send_email`;
  - `serve_pim_senders` and the notifier's account lookup → `list_accounts` with no arguments;
  - the scheduled-message and notify route tables, and `ByTrustedRecipe` callers.
- A scheduler notification keeps the account rule `c0fe0528c` shipped before the split (decided by the
  operator, 2026-11-23): it leaves from the account whose address is the recipient's when that account may
  send, else from the first account that may send, else it fails. Only the tool it rides changes.
- Aura does not require `accountId` before review in this release. The fork's `send_email` requires it,
  and an omitted account comes back from the PIM as a correctable error. Whether Aura should also refuse
  before review is decided after §7's measurement, not before (operator, 2026-11-23; `581c115b8` refused
  it before the model could comply and was reverted).

### 5.4 Display and cockpit

- The display marker becomes `{recipe, tool}`, as WhatsApp's is. `sidecars.go` keys its read tables by tool,
  with the same keys and titles.
- `TrustedMCPFromMeta` keeps accepting the old `{recipe, tool: calendar, action}` marker.
  `tool_invocations` is append-only, so past conversations replay with the old shape forever.
- The policy picker lists plain tools; nothing has actions any more. The collapsed-row summary
  (`toolSummary.ts`) loses its action word, a cosmetic change.

### 5.5 Data migration (next free slot at landing; 0157 today)

- **`gateway_approval_grants` and `gateway_tool_policies`** (decided by the operator, 2026-11-23).
  - Only a per-action row `(<ns>__calendar, <action>)` is rewritten, to `(<ns>__<action>, '')`, for any
    namespace, so aliases are covered.
  - A tool-wide row `(<ns>__calendar, '')` is not fanned out to the 29 tools, and a hand-typed row already
    named `<ns>__<action>` (inert today) is not switched on. Both are listed in the upgrade log, by tool,
    action and kind, and then deleted. Neither becomes a live rule that nobody set on the new tools.
  - Because those rows go first, the rewrite cannot collide.
  - The new names go through the bridge's `namespacedName`, which truncates and hashes a long name. The
    rewrite therefore runs in Go with that function, once, with the upgrade; plain SQL string concatenation
    would produce names the bridge never registers.
- **`message_drafts`.**
  - Replace the CHECK so that `(recipe:calendar, send_email, '')` is the email shape. The old shape
    `(recipe:calendar, calendar, send_email)` stays allowed for resolved rows, which keep their old values
    as history.
  - A pending or scheduled email draft that names an `accountId` survives and still sends after the upgrade:
    `tool_name`, `action` and `registered_tool_name` are rewritten, `action` leaves the stored args, and
    fingerprints are recomputed with `argsFingerprint` itself, in the same Go step.
  - A pending or scheduled email draft without an `accountId` is expired, with a reason that says the
    sending account must be chosen again. The new `send_email` would refuse it (operator, 2026-11-23).
- **Left alone.**
  - `tool_invocations`: append-only, so the old shape is read through §5.4's legacy reader.
  - `conversations.tool_calls`: replay only.
  - `paused_states` and `idempotency_operations`: transient. A prompt pending at the upgrade resolves as
    "once".

### 5.6 Prompts, skills, search

- Rewrite the texts that name `calendar__calendar` or an `action`:
  - the `briefing-aura` skill;
  - the model-in-the-loop skill;
  - `briefing_baseline.py`;
  - the live e2e prompt;
  - the task and mail-watch descriptions.
- Regenerate the deferred-manifest corpus.
- Update the blind-eval gold labels from one tool to the specific tools.
- Gate for the new corpus: on every PIM query of the eval, the right tool is in `tool_search`'s top 5.
  Today the one `calendar` tool is trivially right; after the split the model must find one of 29, so
  "does not regress" alone would compare unlike things. *(My call.)*

### 5.7 Pin and rollout (D-23, D-32)

Today the sidecar can change under an Aura that does not match it. `compose.yaml` has drifted from D-23 to
`:latest` with `pull_policy: always`; `install_env.sh` writes `AURA_PIM_MCP_IMAGE=…:sidecar` into each
appliance's `.env`; the fork's every push to `main` moves `:latest` and `:sidecar`; and the updater pulls
sidecars on its own. A split merged to the fork's `main` would reach appliances still running the old Aura.
So the rollout has a step before the split (decided by the operator, 2026-11-23):

1. **Pin release (before any split code).** One Aura release:
   - pins `AURA_PIM_MCP_IMAGE`'s default in `compose.yaml` to the current fork revision's `:<sha>`;
   - drops `pull_policy: always` for the PIM;
   - adds `AURA_PIM_MCP_IMAGE` to the updater's `RETIRED_ENV_KEYS` (`deploy/aura-image-update.sh`) and stops
     `install_env.sh` writing it, so no appliance `.env` overrides the pin;
   - keeps an explicit override for development, under a name the updater does not retire and appliances
     never set.

   This is the shape the updater already expects: its header says an MCP left at its SHA-pinned compose
   default is a no-op for the sidecar refresh. The PIM then changes on an appliance only when an Aura
   release changes the pin. The updater applies the release. From then on, a push to the fork's `main`
   reaches no appliance.
2. **Fork split.** The fork merges §4 and publishes `:<sha>`.
3. **Split release.** One Aura release carries the new pin, the code and the migration. The updater applies
   the pair.

The CI references to `:latest` move to the pin in step 1.

## 6. Documents

- PRD amendments for the normative sites that describe the multiplexed tool. Each cites date and evidence:
  §5's policy key, §11/§13's never-loaded rationale, the reservation note, the mail-behind-`calendar` line,
  and the `send_email` draft line.
- A superseded banner on the curated-surface spec, naming this document.
- `PROVENANCE.md` and the fork's `AURA-FORK.md` change 4 rewritten.
- The MailCraft plan (`plans/2026-11-23-pim-mail-calendar-rows.md`) is reconciled after this lands. Its new
  operations become new tools, not new actions.

## 7. Verification

**Units and integration:** every touched package at its coverage floor, mutation on the risk table and the
draft target, and the fork's suite against Dovecot and GreenMail.

**On the lab VM, through the updater:**
1. `tools/list` shows the 29 tools.
2. A cockpit turn that sends mail produces a draft with `accountId` and sender. Approved, it sends, and the
   Sent copy lands.
3. A briefing turn reads mail and events with no reservation error.
4. An event is created, read and deleted through its reference.
5. A `deny` on `calendar` / `send_email` set before the upgrade refuses `calendar__send_email` after it.
6. A pending draft and a scheduled email draft made before the upgrade, each with an account, send after
   it. One made without an account is expired.
7. A conversation from before the upgrade replays with its tables.
8. `tool_search "manda una mail"` returns `calendar__send_email` first.
9. A scheduled task with an email notification delivers it, from the recipient's own account.
10. Over several cockpit sends on the route of the day, count how many carry `accountId` on the first call.
    That count decides whether Aura adds its own pre-review requirement (§5.3).

Before the split, the pin release is checked on its own: the PIM container runs the pinned digest, and a
push to the fork's `main` changes nothing on the VM.

The run is recorded in one dated file in `docs/superpowers/verification/`.

## 8. Left to the builder

- Task order inside each repository, under the constraint of §5.7.
- How the 29 tools are grouped in files within the 600-line limit.
- How the Go upgrade step of §5.5 is hooked to the migration (a boot-time repair keyed on the migration
  version, or a step of the migration runner), as long as it runs once and before the first turn.
- Wording of tool descriptions, within D-36's budget per tool.
- The name of the development-only image override (§5.7).

## 9. Inventory summary (2026-11-23)

Production sites in Aura keyed on `(calendar, action)`:
- 24 rename-only;
- 10 that touch persisted data;
- 20 behaviour changes;
- about 14 namespace-level sites left alone.

To update as well: about 47 Go test files, 6 web test files, 4 search and eval fixtures, and 13 normative
document sites.

The tables that store `calendar` + action pairs are `gateway_approval_grants`, `gateway_tool_policies`,
`message_drafts` (columns, CHECK, args, fingerprints), `tool_invocations` (append-only) and
`conversations.tool_calls`.

## 10. What this design does not establish

- That every route in use fills the per-tool schemas correctly. That is measured in §7. One cockpit turn
  on `inkling-2-mini` after `63365df` is the only data point so far.
- The cost of about 29 deferred entries in `tool_search`'s ranking until the eval of §5.6 runs.
- How many operators hold a tool-wide `calendar` grant or policy. The upgrade drops them and logs them
  (§5.5); a tool-wide `deny` set on the old tool stops applying, and the log is how the operator learns it.
