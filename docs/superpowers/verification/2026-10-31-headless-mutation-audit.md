# Ordinary writes in a run with no operator — audit, 2026-10-31

Asked by the operator on 2026-10-31: the next step named by
`docs/superpowers/specs/2026-10-30-headless-memory-erase-design.md` §7, "the same read applied to
every `MCPActionMutate` entry".
- **Where it was read.** Aura at `8af1b9f`, in a cloud container. The verdicts are measured
  in-process. **Not the lab VM.**
- **The question.** In a scheduled `agent_job` that reads untrusted text (items 4 and 5 read the
  operator's mail), which ordinary writes run with no approval, and which of those cannot be taken
  back or reach outside the appliance?

## How a tier is decided, briefly

- **A trusted recipe's action table sets the floor.** `trustedRecipeActions`,
  `browserRecipeActions` and `craftRecipeActions` in `internal/agent/mcptools`.
- **The server can raise it.** A server's `DestructiveHint: true` raises the tool to Destructive
  (`explicitDestructive`, `bridge_risk.go`).
- **Only Destructive stops.** It stops a turn (`gated`, `internal/gateway/decide.go`), and with no
  responder on a strict profile it is denied. Everything below runs, headless included.
- **One exception.** `calendar` `send_email` and WhatsApp `send_message` are withheld as drafts
  for the operator before the gateway (`internal/messagedrafts/validation.go`).

## Measured verdicts

A throwaway in-process probe, not committed. `Decide` ran on `single_user_hardened` with no
responder and an acquired operation:

| Call | Verdict |
|---|---|
| `calendar` `create_event` with `attendees: ["outside@example.test"]` and a `body` | **allow**, tier normal, no approval |
| `calendar` `update_event` adding `attendees` | **allow**, tier normal, no approval |
| `calendar` `bulk_mark_emails_read` | **allow**, tier normal, no approval |
| `browser__agent_browser_click` | **allow**, tier normal, no approval |
| `calendar` `send_email` (reference) | deny, destructive |
| `browser__agent_browser_eval` (reference) | deny, destructive |

## Every ordinary write, judged

| Tool | Effect | Can it be undone or does it leave the appliance? | Finding |
|---|---|---|---|
| `memory_upsert_fact` | writes a fact; `supersedes` closes a window | the past stays answerable | safe |
| `memory_batch` | upserts, supersessions, merges, forgets | the server declares `DestructiveHint: true`, so it is gated | safe; the table says Mutate, the server raises it |
| `memory_merge_entities` | folds one entity into another and deletes it | not cleanly; the server declares `DestructiveHint: true`, so it is gated | safe by annotation only; the table should say Destructive too |
| `memory_reembed` | rewrites vectors | recomputable | safe |
| `memory_forget` | removes facts | no | Destructive since 2026-10-30 |
| `whatsapp` `download_media` | a local file | local | safe |
| Craft writes | edit a document | the Craft table calls them reversible (`bridge_risk_craft.go`) | not re-read here |
| **`calendar` `create_event`, `update_event` with `attendees`** | a calendar invitation with a subject and a body to any address | **leaves the appliance**, if the PIM sends invitations; not verified here | **A1.** An outbound message that bypasses both the gateway and the draft review that `send_email` gets |
| **`browser` click, fill, type, press, select, check, uncheck** | acts on a page, logged in as the operator | **depends on the site**: a purchase, a deletion, a submitted form | **A2.** Ungated by design in a turn, and ungated headless too |
| `calendar` `mark_email_read`, `bulk_mark_emails_read` | changes the unread state | the state can be reset, but not which mails were unread | **A3.** An injected "mark all read" hides mail from the operator and from the next briefing |
| `calendar` `update_contact` | overwrites a contact's fields | no history | **A4.** Data loss, small |
| `calendar` `create_contact` | adds a contact | yes | safe |

## What this audit does not establish

- **That the PIM sends invitations.** The PIM fork is a separate repository, outside this
  session's scope, and was not read. A1's severity depends on it. Microsoft Graph sends
  invitations on create by default; whether the fork does is a lab-VM probe with a test address.
- **That a model follows an injected instruction.** These are the gateway's verdicts, not a run.
- **Built-in tools.** `shell_exec`, the fs tools, `skill_manage` and `task` are outside the MCP
  tables and were not re-read. `shell_exec` has its own destructive-pattern approval.
- **Unmanaged MCP servers.** They follow their own annotations; an unannotated tool fails closed to
  Destructive.
- **The fixes.** A1–A4 are findings. Each fix is a behaviour change, and the decision is the
  operator's.
