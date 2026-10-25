# Pack install on the lab VM, 2026-10-25

Plan `docs/superpowers/plans/2026-10-25-pack-install.md`, Task 13, Steps 4 and 5. Spec
`docs/superpowers/specs/2026-10-25-pack-install-design.md`, §5 acceptance A–D.

**Verdict: A, B and D pass, and so does C's install; C's closing broker question was not asked.**
- **A.** The operator installed the broker plugin from the cockpit. Its server runs in the box from
  `/plugins/broker-analysis` with both variables set. The agent answered a broker question through it
  a minute later, with no restart.
- **B.** The removal took out the connector, the five skills, the tree and the server, live.
- **C.** From chat, after the removal, the agent proposed the plugin. The operator accepted the Italian
  card, and all five skills landed. Before `997a6878c`, the review after a removal skipped every one of
  them.
- **D.** Aura's agent gave five frictions. None is a defect, and four stay open as improvements (below).

## Protocol

- **Build.** `3ed81a0b2`, the aura image's revision label. The operator approved the update in the
  cockpit, since the updater held it for an admin, and aura restarted at 17:51:00Z. It carries the
  whole plan, `d81343b`…`e416293`, and the final review's fixes, `997a6878c`…`3ed81a0b2`.
- **Who did what.** The operator drove every cockpit and chat step, as the plan says. Claude read the
  results back over ssh, read-only:
  - the plugin record under `/var/lib/aura/plugins`;
  - `aura.mcp_server`, `aura.mcp_audit`, `aura.skill_audit`, `aura.tool_invocations` and
    `aura.conversation_turns`;
  - the skills directory;
  - `docker top` and `/proc/<pid>/{cwd,environ}` in the operator's box, `aura-box-448ddbe1…`.
- **The starting state,** read at 17:52Z:
  - no plugin record;
  - the registry held `browser`, `calendar`, `linear` and `whatsapp`, none from a `pack:` source;
  - none of the broker's five skills was in the library;
  - the box had no `/plugins`.

## A. Install from the cockpit

- **The review.** The operator read it in Governance → Plugins before installing. It showed:
  - `agent-plugins/1.0.0`;
  - five skills;
  - `broker-analysis` in the box, with its command line `uvx --from
    git+https://github.com/chetto1983/broker-analysis-mcp@v0.1.1 broker-analysis-mcp`.
- **The commit is `172b348`, not the tag's `17890a0`.** The review reads the default branch, and the
  broker repository's `main` gained a commit after the v0.1.1 tag. The kept tree, and so the skills,
  come from `172b348`. The server runs from the `@v0.1.1` tag its `mcp.json` names.
- **The install:**

  | Time (Z) | What |
  |---|---|
  | 17:54:23 | Five skill-install audit rows; the record is installed, with the five skills and their hashes. |
  | 17:54:50 | One `plugin-install` registry audit row, after the stdio connector was verified in the box. |

- **The agent, with no restart** (aura still running since 17:51:00):

  | Time (Z) | Tool | Result |
  |---|---|---|
  | 17:55:10 | `skill` | loaded `risk-analysis` |
  | 17:55:14 | `tool_search` | found `broker-analysis__risk_metrics` |
  | 17:55:20 | `broker-analysis__risk_metrics` | ok in 1317 ms, AAPL over 753 days |

- **The box:**
  - `/plugins/broker-analysis` is present, with its commit marker in `/plugins/.aura`.
  - `docker top` shows the wrapper shell, then `uv tool uvx --from …@v0.1.1 broker-analysis-mcp`,
    then its python. The wrapper runs `mkdir -p
    '/workspace/.plugin-data/broker-analysis-7011f7f24e82' && cd '/plugins/broker-analysis' && 'uvx' …`.
  - All three processes have cwd `/plugins/broker-analysis`, and in their environment
    `PLUGIN_ROOT=/plugins/broker-analysis` and
    `PLUGIN_DATA=/workspace/.plugin-data/broker-analysis-7011f7f24e82`. The data directory exists.

## B. Removal from the cockpit

The operator ticked "also delete its data". The plan asked for the data to be kept: this run
exercised the other branch.

| Time (Z) | What |
|---|---|
| 17:57:11 | One `plugin-remove` registry audit row; `mcp live unmounted server=broker-analysis tools=11`; five skill-delete audit rows. |

At the read-back:
- the record and the registry row are gone, and so are the five skills, deleted rather than archived;
- in the box, `/plugins/broker-analysis` and its marker are gone, and no broker process runs;
- `/workspace/.plugin-data` is empty: the data was deleted in the operator's box, as asked.

## C. Install from chat

The conversation `01a17438…`:

| Time (Z) | What |
|---|---|
| 18:00:13 | The operator writes "installa il plugin chetto1983/broker-analysis-mcp/broker-analysis". |
| 18:00:18 | `tool_search` finds `plugin_pack`. |
| 18:00:24 | `plugin_pack` answers `plugin_approval_required`, in 1806 ms. |
| — | The operator reads the card in Italian before accepting: "competenze (5)" and the `uvx …@v0.1.1` command (operator's confirmation). |
| 18:00:57 | `plugin_pack` installs it, in 9184 ms: all five skills `ok`, the connector `ok`. |
| 18:01:04 | The agent reports the plugin and its five skills. |

The record was rewritten at 18:00:50, and the server runs again in the box with the same cwd and
environment. The broker question that closes C was not asked; the next message was D's.

## D. The agent's account

Asked what the proposal and the card told it and what was missing, Aura's agent named what it read:
- the version;
- the repository and the exact commit;
- the five skills;
- the one connector, run in the box through `uvx`, with the full command and the `v0.1.1` tag;
- MCP enabled, nothing skipped, no notes;
- the digest that bound its consent to this proposal.

Its frictions, with what was found and their status:

| # | The agent's friction | Finding | Status |
|---|---|---|---|
| 1 | No delta against what exists: the five skills "were already in my list before the install". | Not so: the removal deleted them at 17:57:11, the skills directory was empty at 17:58, and the loader's snapshot lasts one second and is also invalidated by the removal. The conversation began at 18:00:13. The agent read its list as it stood at 18:03. A name that exists is shown under "skipped"; nothing says "new". | Open: say "new" explicitly on the card. |
| 2 | No practical effects: what credentials, network access or data the server needs; "notes: none" does not say whether anything was checked. | The review cannot know what a server reaches. It knows the plugin asks for no env, and which egress policy the box applies. | Open. |
| 3 | The approval text it relays is technical and in English. | By design: the canonical question is English, and `approvaltext` renders it in the operator's language, which the operator confirmed. The challenge message does not tell the agent so. | Open: one sentence in the challenge message. |
| 4 | A second interaction after "installa". | The operator's 2026-10-23 decision: the agent proposes, the operator confirms. | Stands. |
| 5 | The report says `ok` and is no functional proof. | A stdio connector answers an MCP handshake in the box before it is saved (spec §4.3), and the report does not say so. | Open: name the handshake and the tool count in the report. |

## What this does not show

- **The Telegram card.** Not exercised on the VM. Its rendering is unit-tested in `internal/approvaltext`
  (`TestDerivePluginInstallAndRemove`, `TestRenderItalianPluginInstall`).
- **A removal that keeps the data.** The operator deleted it. That branch is covered by
  `TestRemoveKeepsDataUnlessAsked` and the PluginsBoard tests.
- **A member.** The VM has no second identity, so the 403 and the refused approval are covered only by
  unit tests (`TestPluginPackRefusesAMember`, the route tests).
- **A `./` command and `PLUGIN_ROOT` in use.** The broker needs neither. That path is
  `TestPluginServerAnswersFromItsRootInTheBox` (`docker_integration`). The data surviving a box
  re-creation is `TestPluginProcessSeesItsRootAndKeepsItsData`.
- **A remote connector, or OAuth.** The broker has none.
- **An install pinned to a tag.** A ref names a repository and a plugin, and the review takes the
  default branch. Here the skills came from `main` past the tag and the server from the tag. Whether a
  ref should name a tag is not decided.
- **Latency.** Not measured.

## End state

At the operator's choice the plugin stays installed. Its record is `installed` at `172b348`, its
connector and five skills are live, and its server runs in the operator's box. Nothing else was
written on the VM.
