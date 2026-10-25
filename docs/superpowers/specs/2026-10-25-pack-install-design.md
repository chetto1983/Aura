# Pack install from the cockpit and from chat — design

Date: 2026-10-25
Status: draft, for the operator's review

## 1. Why

On 2026-10-23 the operator asked for `chetto1983/broker-analysis-mcp/broker-analysis` on the lab
VM. It was installed by hand through `aura pack install` and `aura pack trust`. The test showed
two gaps:
- **The CLI wrote a correct registry row.** It had runtime box, `initTimeoutSec` 120 and trust
  `sandboxed_local`.
- **The running daemon never mounted it.** The daemon mounts registry servers at boot
  (`cmd/aura/main.go`), and while it runs only through the cockpit's MCP write path
  (`mcpWriteAdapter` → `liveMCPMount.MountNow`). The agent saw the skills and none of the tools.

The operator's correction was: "lo deve poter fare l'operatore o l'agente non te a mano". A pack
that only a shell outside the operator's surfaces can install is not a feature.

The operator decided on 2026-10-23 (memory `project_pack_install_cockpit_and_agent`):
- One install backend with two entries, the cockpit and the chat.
- In chat, the agent proposes and the operator confirms. The agent never installs without that
  confirmation.
- Aura's pack reader also reads the portable Agent Plugins 1.0 format, beside the Claude format.

On 2026-10-25 the operator added three answers:
- **Conformant, not read-only.** Aura gives a plugin's stdio servers their files, `PLUGIN_ROOT` and
  `PLUGIN_DATA`.
- **Install and remove.** Update is out of this slice.
- **The CLI loses `install` and `trust`.** It keeps `list` and `show`.

## 2. Goals, non-goals, anti-goals

**Goals**
1. From the cockpit, the operator installs a plugin named `owner/repo/plugin`:
   - Aura shows a review first;
   - one confirmation installs the skills and the connectors and mounts the connectors live.
2. From chat, the agent proposes the same install, and the operator confirms it on an approval
   card, in the cockpit or on Telegram. The proposal and the card carry the same review.
3. Aura reads an Agent Plugins 1.0 plugin as the specification requires:
   - its validation and failure boundaries;
   - `PLUGIN_ROOT`, `PLUGIN_DATA` and placeholder expansion for stdio servers;
   - the plugin root as the default working directory.
4. From both surfaces, the operator removes an installed plugin in one step: its skills, its
   connectors (unmounted live) and its files.
5. On the lab VM, the operator installs the broker plugin from the cockpit and the agent answers a
   broker question with the server running in `aura-box-<identity>`. The agent then proposes an
   install, and the operator confirms it from the card.

**Non-goals**
- **Update** (re-fetch and diff). For a new version, remove and reinstall.
- **Placeholder expansion for the Claude format** (`${CLAUDE_PLUGIN_ROOT}`). Claude-format packs keep
  today's reading.
- **Hooks, agents, LSP, client extension directories.** They are outside Agent Plugins v1 (§7), and
  Aura ignores them as §11.3 requires.
- **Marketplaces and catalog search for plugins.** The input stays `owner/repo/plugin` on GitHub
  (`packs.gitHubHTTPS`).
- **Pinning syntax in the reference** (`owner/repo/plugin@sha`). The review pins the commit
  instead (§4.2).

**Anti-goals**
- **The agent installs or removes without the operator's confirmation.** This must hold on every
  runtime profile, including `local_trusted` and `dev`, where the gateway is a no-op
  (`gateway.Decide`).
- **What gets installed differs from what the operator reviewed** (another commit, or another
  connector set).
- **A second install path that behaves differently.** That is the 2026-10-23 failure.
- **A third-party stdio server runs as a child of the Aura process.** Aura's environment holds the
  database credentials (`stagePackServer`).
- **A secret travels inside a pack.** Agent Plugins §9.2 forbids it, and a box server takes no
  secrets (`mcp.ValidateManagedServer`).

## 3. What exists, and is reused

| Piece | Where | Reused for |
|---|---|---|
| Resolver, `ReadTree`, `Pack`, `Server`, `SourceMarker` | `internal/packs` | the reading, extended with the Agent Plugins format |
| Shallow `git clone` of github.com only | `packs.GitFetcher` | the fetch, extended to a pinned commit |
| `InstallableServers` (placeholder `url: ""` skipped) | `internal/packs/install.go` | kept |
| `stagePackServer`: stdio → box runtime, `initTimeoutSec` 120, name collision refused | `cmd/aura/pack_install.go` | moved into the daemon-side backend |
| `prepareAndVerify`: validate, then an MCP handshake in the box before the save | `cmd/aura/mcp_prepare.go` | every connector before it is saved |
| Registry write with its audit row in one transaction | `saveManagedMCPConfig`, `mcpWriteAdapter.save` | the single write of an install or a removal |
| `MountNow` / `Unmount` | `cmd/aura/mcp_live_mount.go` | the live mount and unmount |
| Skill install: validation, blocklist, `Writer.WriteInstall`, audit | `skills.Installer.Install` | split, so a skill installs from a directory already fetched |
| Box mirror of host trees (`MaterializeIn`, the `/skills` mirror) | `internal/sandbox/usersandbox/materialize.go` | `/plugins` in the box |
| Tool-owned approval challenge bound to the question text, plus a resume hook | `tools.ShellApprovals`, `newShellResumeHook`, `approvaltext` | the chat confirmation |
| Read-only `plugin_pack` tool (`list`, `show`) | `internal/agent/tools/pack.go` | gains `install` and `remove` |
| Cockpit Skill and MCP install panels | `web/src/governance/SkillInstallPanel.tsx`, `McpInstallPanel.tsx` | the pattern for the plugin panel |

Measured facts this design rests on:
- **Broker cold start in a box.** It takes 28.7 s with an empty uv cache and 1.5 s warm
  (2026-10-23), against the 30 s default box budget. Hence `initTimeoutSec` 120.
- **The verification warms the box.** `prepareAndVerify` gives a box server its first-start budget
  (`mcp.BoxInitTimeout`). After it, `MountNow`'s 15 s single attempt meets a warm cache.
- **The lab VM runs `AURA_PROFILE=single_user_hardened`** (read 2026-10-25). That is a strict
  profile, so the gateway is active there. Goal 2 must not depend on it.

## 4. Design

### 4.1 Reading the two formats

A plugin directory is read in **Agent Plugins** form when two things hold:
- it has a root `plugin.json`;
- that file's `$schema` begins with `https://agent-plugins.org/schemas/`.

Otherwise it is read in **Claude** form (`.claude-plugin/plugin.json` + `.mcp.json`, unchanged). When
both forms are present, Agent Plugins wins: §5.1 says no other file overrides root `plugin.json`.
The broker plugin will ship both forms (§6).

Discovery (`packDirs`) recognises either manifest, at the root or one level down, as it does today.

**Agent Plugins rules**, from `spec/1.0.0.md` of `agentplugins/agent-plugins-spec` (read
2026-10-25; 1.1.0 is a working draft that changes only version strings):

Manifest:
- **Supported version.** `$schema` must be exactly
  `https://agent-plugins.org/schemas/1.0.0/plugin.schema.json`. Another `agent-plugins.org`
  version rejects the plugin and names the unsupported version (§5.2). The schema is never
  fetched.
- **Closed schema.** The allowed fields are `$schema`, `name`, `version`, `description`, `author`
  (an object with only `name`, `email` and `url`), `homepage`, `repository`, `license`, `keywords`
  and `extensions`.
  - An unknown top-level field is reported and ignored.
  - A non-object `extensions` is reported and ignored.
  - Any other violation rejects the plugin (§5.2, §5.3).
- **Name rule.** `name` has 1–64 characters from `a-z 0-9 - .`, starts and ends with an
  alphanumeric, and has no `--` or `..` (§5.5).

Skills:
- Each immediate child of `skills/` that holds a regular file named `SKILL.md` is one skill
  (§7.1). An invalid skill is skipped with its reason.

MCP servers:
- **The file.** `mcp.json` holds exactly `$schema` and `mcpServers`. Its `$schema` must name the
  same version as `plugin.json`. Otherwise MCP is disabled for the plugin, the skills still load,
  and the review says why (§7.2.2, §10.1).
- **Each server** is a closed variant (§7.2.1), and a bad entry is skipped with its reason:
  - **`stdio`.** It has `command`, `args`, `env` and `cwd`.
    - `command` is one token: a bare name or `./path`.
    - `cwd` is `./…`, `${PLUGIN_ROOT}…` or `${PLUGIN_DATA}…`.
    - `env` must not contain `PLUGIN_ROOT` or `PLUGIN_DATA`.
  - **`streamable-http` / `sse`.** It has `url` and `headers`.
    - The URL is absolute, has no userinfo and no fragment, and uses HTTPS unless the host is
      loopback.
    - Header names are not repeated under different casing.
- **Containment (§4.1).** Every package path resolves inside the plugin root. A symlink that leaves
  it is a failure for that path's component, at the narrowest boundary §4.1 lists.

Everything skipped or rejected reaches the review (§4.2) with its reason. Nothing is dropped
silently. The reading must also preserve what §11.3 requires: a failure in one component never
stops an independent valid one.

### 4.2 The review

`Review(ref)` fetches the default branch, resolves its commit, reads the plugin and returns:
- **identity:** name, version, author, format, and `owner/repo/plugin@<40-char commit>`;
- **skills:** their names, plus each skipped skill and why;
- **connectors**, each with:
  - its transport and where it runs: stdio in the calling identity's box, or remote over HTTPS;
  - whether it will ask for an OAuth authorization (the Claude format's `oauth` block, or a remote
    server, since Agent Plugins leaves authorization to the client, §7.2.1);
  - the trust class it will receive;
- **skipped connectors and why:** placeholder, invalid variant, unsupported transport, or a name
  already taken and by which source;
- **a digest** over the reviewed content: commit plus the normalized list of skills and connectors.

The review installs nothing and writes nothing.

`Install(ref, commit, digest)` refuses when the content at that commit no longer gives the same
digest. This is how anti-goal 2 holds. It is also why the confirmation in chat can bind to exact
arguments (§4.6).

### 4.3 The install backend

There is one implementation, daemon-side, behind both entries. In order:

1. **Fetch** the repository at `commit`, shallow, and re-read the plugin. A different digest refuses
   the install.
2. **Keep the plugin tree** under `<AURA_CONFIG_DIR>/plugins/<name>/`. That is the durable
   `aura-home` volume on compose, so no new variable is needed.
   - The kept tree is the filesystem-resolved plugin root. Symlinks are refused, as `MaterializeIn`
     already refuses them.
   - An existing directory for that name refuses the install ("already installed"). There is no
     overwrite.
   - Beside the tree, outside the plugin root, an **install record** keeps the ref, the commit,
     the digest, who installed it and when, and the skill and connector names that landed. It is
     the only record of which skills belong to the plugin. No skill source is persisted today:
     `Writer.WriteInstall`, the skill audit row and the catalog upsert carry none. Being outside
     the root, the record is never mirrored into the box.
3. **Install each skill from the kept tree.** Skills go into the **deployment's** library (the
   global `Writer`), like the registry and `/plugins`. Each skill passes the same validation and
   blocklist as a single skill install and goes through `Writer.WriteInstall`. Skills are not
   re-fetched one by one, because a re-fetch could land another commit.
   - A plugin skill whose name already exists in the library is **skipped** and listed in the
     review, as a taken connector name is. `WriteInstall` would otherwise replace the existing
     skill (`promoteDir`), and a later removal would archive it.
4. **Stage each connector:**
   - **stdio:**
     - it runs in the box (`runtime.kind=box`, `initTimeoutSec` 120);
     - the working directory is the plugin root in the box unless `cwd` says otherwise;
     - `${PLUGIN_ROOT}` and `${PLUGIN_DATA}` are expanded in `args`, `env` values and `cwd` only,
       once, non-recursively (§9.2), into the in-box paths of §4.4;
     - `PLUGIN_ROOT` and `PLUGIN_DATA` are set in the process environment after the configured
       `env`;
     - a `./` command resolves against the plugin root in the box.
   - **Remote:** its URL and headers as declared.
   - **Both:**
     - `Source: pack:owner/repo/plugin`;
     - trust approved by this confirmation: `ApprovedBy` the operator, `Reason` `plugin install
       <ref>@<commit>`, class from `mcp.Classify`, so `sandboxed_local` for a box stdio server;
     - membership in the active profile (`joinActiveProfile`).
5. **Verify each stdio connector** with `prepareAndVerify` before anything is saved. A stdio
   connector whose handshake fails is not saved and is reported. The rest of the install continues
   (§11.3).
   - **Remote connectors are not verified before the save.** `prepareAndVerify` returns at once for
     them (`cmd/aura/mcp_prepare.go`), and one that needs OAuth cannot complete a handshake until
     the operator authorizes it.
   - They are saved and then probed, as a cockpit MCP install does today. A remote connector
     waiting for authorization is reported as such, with its link to the authorization panel.
6. **One registry write** with one audit row for the pack. Then `MountNow` for each saved
   connector.
7. **Return a report:** every skill and connector with ok, skipped or failed, and why.

The cockpit request waits through the verifications: up to 120 s per cold box server, and the broker
has one.

### 4.4 In the box

- **The kept tree** is mirrored into every box at `/plugins/<name>/`, by the `MaterializeIn`
  mechanism that mirrors `/skills`.
  - That mirror runs on **every** box lookup: each `Route` does a full `Resolve`
    (`router_provision.go`), and each `Resolve` clears and re-copies its destinations, even into a
    running box. A newly installed plugin therefore reaches a running box before its connectors'
    verification, with no extra work.
  - **The `/plugins` mirror must not clear and rewrite a plugin tree that has not changed.** A
    plugin server runs with that tree as its working directory, and today's clear-then-copy would
    delete it under the server on the next `shell_exec` or other boxed call.
  - A tree is rewritten only when it is absent or its kept copy differs, for example by digest. A
    tree whose plugin was removed is deleted.
- **`PLUGIN_DATA`** is `/workspace/.plugin-data/<name>/`, on the identity's workspace volume:
  - it persists across box re-creation and Aura updates (§9.1);
  - it is created before the first start of a server.
- **The box exec of a server** runs with the server's working directory. Today the exec always
  runs in `/workspace` (`sandboxMCPBox.startStdio`).

### 4.5 The cockpit

- **Install.** A "Plugin" entry in Governance has a field for `owner/repo/plugin` and a Review
  button. The review (§4.2) shows each connector with where it runs, its OAuth need and the reason
  for anything skipped. Then a single "Installa" confirmation, and the report (§4.3 step 7).
  - A connector that needs OAuth links to the existing MCP authorization panel.
  - Strings in EN and IT (react-i18next, as the rest of Governance).
- **Installed plugins.** A list shows each plugin's name, version, commit, skills and connectors,
  and its "Rimuovi" button.
  - Removal asks one confirmation.
  - It offers to also delete `PLUGIN_DATA`, off by default (§9.1: "MAY delete").
- **Route permissions.** The review, install and remove routes require `identity.create`, as the
  MCP write routes and the skill install route do (`cmd/aura/serve_webui.go`, prd.md §3
  2026-10-10). The registry, the library and `/plugins` belong to the deployment, and a plugin's
  servers run in every identity's box. A member keeps their own OAuth authorization on a server.

### 4.6 The chat

`plugin_pack` gains `action=install` and `action=remove`. It stays deferred.

- **First call.** `install {ref}` (or `remove {name}`) returns a tool-owned approval challenge. It
  carries:
  - the review text;
  - the exact arguments to repeat: `ref`, `commit` and `digest`;
  - a `resume_context` of a new type, `plugin_pack_approval`.

  The model relays it with `ask_user kind=approval`, as `shell_exec_approval` is relayed today.
- **Binding.** The challenge store binds the operator-visible question to the arguments, keyed on
  the authenticated conversation. A model that relays a different question records nothing, as
  `ShellApprovals.ApproveChallenge` does. The challenge needs a store; reuse the shell's store
  rather than duplicate it.
- **Accept.** The resume hook records the accept. The agent repeats the call, which installs once
  through §4.3 and consumes the approval.
- **Decline or expiry** records nothing.
- **Profiles.** The challenge is the tool's own, so it holds on every profile. It does not ride on
  the gateway, which only gates `Destructive` calls on strict profiles.
- **Who may approve.** The tool issues an install or remove challenge only in a conversation whose
  identity holds `identity.create`, and the resume hook counts an accept only from it. A member
  asking for a plugin gets a refusal naming the operator, the same line the cockpit routes draw.
- **The gateway.** `plugin_pack` becomes `Mutating` and `Multiplexed`, so it needs a per-action
  classifier at boot (`internal/gateway/guard.go`):
  - `list` and `show` are `Safe`;
  - `install` and `remove` are `Normal`.

  The tool's card is the only card. A `Destructive` grade would make the gateway ask a second time
  on a strict profile for the same decision, which is the prompt fatigue `gateway.gated` was
  narrowed to stop.
- **Presentation.** `approvaltext` gains the `plugin_pack_approval` type, so the cockpit and
  Telegram cards render the review rather than raw JSON.

### 4.7 Removal

Removal finds a plugin's connectors by their `pack:owner/repo/plugin` source and its skills by the
install record (§4.3 step 2). It then:
- removes the connectors from the registry, in one write and one audit row, and calls `Unmount` on
  each;
- archives the skills through the existing archive path, so they leave the box mirror;
- deletes the kept tree, which leaves `/plugins/<name>` at the next mirror;
- deletes `PLUGIN_DATA` only when asked. Every identity's box holds its own copy; the request
  deletes the copies of the boxes Aura can reach.

A new table is not needed for this slice. Measured on the lab VM on 2026-10-25, nothing is left
over from the 2026-10-23 hand install:
- the registry holds `browser`, `calendar`, `linear` and `whatsapp`, with no `pack:` source;
- the library has none of the broker's five skills.

So removal does not have to handle a plugin without a kept tree.

### 4.8 The CLI

`aura pack install` and `aura pack trust` are removed, with their code, tests and usage text.
`list` and `show` stay. The usage text points to the cockpit and to the agent.

## 5. Acceptance

Machine-checkable, before the VM:
1. **Reader conformance:** a fixture table over Agent Plugins manifests and `mcp.json` files.
   - Each fatal case of §5 rejects the plugin.
   - Each per-component case of §6–§7 skips only its component, with a reason.
   - An unknown field is reported and the plugin is kept.
   - A `$schema` mismatch between the two files disables MCP only.
   - Containment escapes, through `..` and through a symlink, fail at the narrowest boundary.
2. **Digest:** an install at a commit whose content changed since the review is refused, and nothing
   is written.
3. **Expansion:** `${PLUGIN_ROOT}` and `${PLUGIN_DATA}` expand once, non-recursively, in `args`,
   `env` values and `cwd`, and never in `command` or `env` keys. A server whose `env` names either
   variable is skipped.
4. **Install:**
   - skills come from the kept tree, with no network, into the deployment's library;
   - a skill whose name already exists is skipped and reported, and the existing skill is
     untouched;
   - connectors are trusted by the confirmation and written in one transaction with one audit row;
   - stdio connectors are verified before the save, and a failed verification saves nothing for
     that connector and reports it;
   - `MountNow` is called once per saved connector;
   - the install record names every skill and connector that landed.
5. **Chat:**
   - without an accepted challenge, `install` writes nothing on every profile, `local_trusted`
     included;
   - a relayed question that differs from the challenge records nothing;
   - after an accept, exactly one install runs;
   - an identity without `identity.create` gets no challenge, and its accept counts for nothing.
   - The gateway classifies `list` and `show` as `Safe` and `install` and `remove` as `Normal`.
   - The cockpit routes answer 403 to a member.
6. **Removal:** connectors leave the registry and the live registry, skills are archived, and the
   tree is deleted. `PLUGIN_DATA` stays unless asked.
7. **Box:** a `docker_integration` test with a fixture plugin whose stdio server is a `./` script.
   - The script reads `PLUGIN_ROOT`, `PLUGIN_DATA` and its working directory, writes into
     `PLUGIN_DATA`, and answers an MCP handshake.
   - The file it wrote survives a box re-creation.
   - A second `Resolve` while the server runs leaves its working directory in place.
8. **CLI:** `aura pack install` and `aura pack trust` are gone; `list` and `show` behave as before.
9. Gate 2 and Gate 3 as CLAUDE.md states them.

On the lab VM, recorded in `docs/superpowers/verification/`:
- **A. Cockpit install.** The operator installs `chetto1983/broker-analysis-mcp/broker-analysis` at
  v0.1.1 from the cockpit, and reads the review first. The Agent Plugins form is the one read.
  - The agent answers a broker question with no restart.
  - `docker top aura-box-<identity>` shows the server.
- **B. Removal.** The operator removes the plugin from the cockpit. The tools leave the agent's
  registry with no restart.
- **C. Chat install.** In a conversation the operator asks for the plugin. The agent proposes it,
  the card shows the review, and the operator confirms. The agent then answers a broker question.
- **D. The agent's account.** Aura's agent says what the review and the card told it, and what was
  missing. The operator's direction: "se collaborate te ed Aura sistemate tutto".

## 6. The broker plugin

`chetto1983/broker-analysis-mcp` gains, in `broker-analysis/`:
- a root `plugin.json` with
  `"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"` and the same metadata as
  `.claude-plugin/plugin.json`;
- an `mcp.json` with `"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"` and the
  same server as `.mcp.json`.

`tests/test_plugin_manifest.py` extends to them. The release is v0.1.1: pyproject, the manifests,
`mcp.json`'s `uvx --from …@v0.1.1` and the tag must agree. Today the repository ships only the
Claude and Codex forms.

## 7. Calls made while writing, for the operator to check

The operator decided the shape (§1). These were decided by the author, from the code and the
builder check of 2026-10-25:
- **Profile-independent confirmation.** The chat confirmation is the tool's own challenge, not the
  gateway's, so it holds on `local_trusted` too (§4.6).
- **One card.** `install` and `remove` are `Normal` in the gateway; the tool's card is the only
  card (§4.6).
- **Who may install and approve.** Only `identity.create`, on both surfaces, as for the MCP
  registry today (§4.5, §4.6).
- **Library.** Plugin skills go into the deployment's library, not the installer's own (§4.3).
- **Skill name clash.** A clashing skill is skipped, never replaced (§4.3).
- **Trust by confirmation.** The confirmation is the trust approval, recorded with the operator,
  the ref and the commit (§4.3).
- **Commit pinning.** The review pins the default branch's commit, and the install refuses a
  changed digest (§4.2).
- **Storage.** The kept tree lives under `<AURA_CONFIG_DIR>/plugins/`, with no new variable.
  `PLUGIN_DATA` is in the workspace volume (§4.3, §4.4).
- **Remote connectors** are saved and then probed, not verified first (§4.3).

## 8. What this design does not settle, left to the builder

- **Approval store.** The shape of the shared challenge store between `shell_exec` and
  `plugin_pack`: a generalised store, or the shell's store with a second key space. Either is fine,
  as long as there is one implementation.
- **Panel placement.** Where the plugin panel sits in Governance: its own tab, or a section beside
  Skills and MCP.
- **Skill-installer split.** How `skills.Installer.Install` splits into fetch and
  install-from-directory.
- **Working-directory transport.** How the working directory reaches the box exec: a field on
  `ManagedRuntime`, or a value derived from the plugin source at launch.
- **Recording the review.** Whether the review is also kept in the audit row.
- **Change detection for the box mirror.** The mechanism that keeps the `/plugins` mirror from
  rewriting an unchanged tree (a digest file, a per-source skip, or another mechanism), under the
  constraint of §4.4.

## 9. What this design does not show

- **Interoperability.** Agent Plugins conformance is checked against fixtures written from the
  specification text. It is not checked against another client's plugins. The broker is the only
  real plugin in the VM run, and it needs neither `PLUGIN_ROOT` nor a bundled executable.
- **Concurrent installs** of the same name from two surfaces. They are expected to be refused by
  the kept-tree check (§4.3 step 2), but no run measures it.
- **Multi-identity removal.** Removal across many identities' boxes is exercised with one identity
  only, as the lab VM has one.
