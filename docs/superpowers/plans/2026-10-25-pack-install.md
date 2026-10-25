# Pack install from the cockpit and from chat Implementation Plan

**Status (2026-10-25):** DONE. Tasks 1–12 `d81343b`…`e416293`; the final review's fix pass
`997a6878c`…`3ed81a0b2`; the broker plugin's Agent Plugins manifests in `broker-analysis-mcp` `17890a0`
(v0.1.1). On the lab VM at `3ed81a0b2`, acceptance A, B and D passed, and so did C's install
(`docs/superpowers/verification/2026-10-25-pack-install-lab-vm.md`). Four improvements from the agent's
account stay open there.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking. Commits on master (the operator's standing rule).

**Goal:** the operator installs and removes a plugin from the cockpit, and the agent proposes the same
install in chat for the operator to confirm. One daemon-side backend serves both, and it reads the Claude
format and Agent Plugins 1.0.

**Architecture:**
- **`internal/packs` reads.** It gains:
  - the Agent Plugins format (closed schemas, per-component failure boundaries, containment, a reason for
    every skip);
  - safe-name rules;
  - transport mapping;
  - a pinned-commit checkout.
- **`internal/plugins` installs.** It is new and holds:
  - the kept tree and its install record under `AURA_PLUGINS_DIR`;
  - the review, its digest and the escaped card text;
  - connector staging;
  - install, remove and list.

  It reaches the registry, the skill library, the box verification, the probe and the live mount only
  through seams. `cmd/aura` wires them to the existing code, under one process-wide registry lock that the
  MCP adapter also takes.
- **Two surfaces, one backend.** The cockpit uses `/api/governance/plugins` behind `identity.create`. The
  chat uses `plugin_pack` `install`/`remove`. A tool-owned approval challenge comes first (the `shell_exec`
  pattern), and the install runs only after the operator's accept. Both surfaces share one backend
  instance.
- **The box receives the tree.** `usersandbox` mirrors each recorded tree to `/plugins/<name>` and rewrites
  it only when its commit changes or the tree is missing. Box stdio servers start through `cd` into their
  directory, after a `mkdir -p` of their data directory.

**Tech Stack:** Go (`internal/packs`, `internal/plugins`, `internal/mcp`, `internal/sandbox/usersandbox`,
`internal/skills`, `internal/agent/tools`, `internal/gateway`, `internal/approvaltext`, `internal/agui`,
`cmd/aura`), React + TanStack Query + react-i18next (`web/src/governance`, `web/src/approvals`), Python
(the broker repository), Docker (box integration), the lab VM.

**Spec:** `docs/superpowers/specs/2026-10-25-pack-install-design.md` (approved 2026-10-25, with §4.3
step 2 and §6b amended the same day while planning).

## Global Constraints

- **Agent Plugins version.** `$schema` is exactly `https://agent-plugins.org/schemas/1.0.0/plugin.schema.json`
  and `https://agent-plugins.org/schemas/1.0.0/mcp.schema.json`. Another `https://agent-plugins.org/schemas/`
  version is refused and named. No schema is ever fetched.
- **Names:**
  - a **plugin name**, in both formats, has 1–64 characters of `a-z 0-9 - .`, starts and ends alphanumeric,
    and has no `--` or `..`;
  - a **connector name** follows the same rule and also contains no `__`;
  - a connector may not take a catalog recipe's name (`mcpmanager.LookupCatalog`).
- **Paths:**
  - in-box plugin root `/plugins/<name>`;
  - box markers `/plugins/.aura/<name>`;
  - in-box `PLUGIN_DATA` `/workspace/.plugin-data/<name>-<first 12 hex of sha256(ref)>`;
  - host tree `<AURA_PLUGINS_DIR>/<name>/tree`;
  - host record `<AURA_PLUGINS_DIR>/<name>/install.json`;
  - host staging `<AURA_PLUGINS_DIR>/.staging-*`.
- **`AURA_PLUGINS_DIR`.** The code default is `~/.aura/plugins`; compose pins `/var/lib/aura/plugins`.
- **Box stdio connectors.** `runtime.kind=box` with `initTimeoutSec` 120. Trust is approved by the
  confirmation: `ApprovedBy` the operator, `Reason` `plugin install <ref>@<commit>`, class from
  `mcp.Classify`.
- **Transports:**
  - `stdio` and `""` map to stdio;
  - `streamable-http` and `http` map to `streamable_http`;
  - `sse` is skipped as unsupported;
  - a remote server with `headers` is skipped as unsupported (spec §6b).
- **Expansion.** Only `${PLUGIN_ROOT}` and `${PLUGIN_DATA}`, single pass, non-recursive, in `args`, `env`
  values and `cwd`. Never in `command` or `env` keys. An `env` key `PLUGIN_ROOT` or `PLUGIN_DATA` skips
  the server.
- **Skills.** `always` is stripped at install, on every install path. A skill whose name exists active or
  archived is skipped.
- **Permissions.** Review, install and remove need `identity.create`, on the cockpit routes and in chat
  (a member's accept counts for nothing). The installed list needs `governance.read`.
- **The agent never installs or removes** without an accepted challenge, on every profile.
- **Registry writes.** Every registry write the daemon makes goes through
  `updateManagedMCPConfig(ctx, …, fn)`: it takes a process-wide lock, loads, applies `fn`, and saves with
  its audit row.
- **Code rules.** No file over 600 lines, refactor on touch, comments only for a non-obvious why. A new
  Go package enters `scripts/coverage_package_policy.json` at `target`. `deadcode` reports nothing new.
- **Payload.** A change to `compose.yaml` is committed first. Then `make payload-manifest` (WSL)
  regenerates `scripts/payload_manifest.txt`, which is committed right after: the gate refuses while a
  payload file differs from HEAD.

## Review Focus

1. **A crash or a cancel mid-install.**
   - Expected: the record over-states rather than under-states what landed.
   - A name is never locked: a staging directory is renamed into place only with its first record.
   - Removal finds connectors by their `pack:` source as well as by the record.
   - Pinned by Task 7's failure-injection tests.
2. **Concurrent writers.** Two installs of one name, a plugin install during a 120 s MCP install, and a
   removal during an install.
   - Expected: exactly one install wins, and no server is erased from the registry.
   - Pinned by `TestUpdateManagedMCPConfigSerializesWriters` (Task 8) and
     `TestConcurrentInstallsOfOneNameOneWins` (Task 7).
3. **Text the plugin controls reaching the card or the model.**
   - Expected: escaped, capped, one line per field, and no forged `source:` line.
   - Pinned by `TestReviewTextEscapesPluginStrings` (Task 6) and `TestDerivePluginInstallRejectsForgedLines`
     (Task 10).
4. **Box-writable state driving Aura's commands.**
   - Expected: a marker with an invalid name or commit is ignored, and `rm` takes its targets as argv.
   - Pinned by `TestParseMarkersDropsUnsafeEntries` and the `clearDest` argv test (Task 4).
5. **The post-accept call replays the proposal.**
   - Expected: the proposal (`{action, ref}`) and the repeat (`{action, ref, commit, digest}`) have different
     operation fingerprints, so the repeat executes.
   - Pinned by `TestPluginPackRepeatFingerprintDiffersFromTheProposal` (Task 10).

---

## File structure

| File | Responsibility |
|---|---|
| `internal/packs/agentplugins.go`, `agentplugins_mcp.go`, `contain.go`, `names.go` (new) | Agent Plugins manifest and `mcp.json`, containment, name rules, transport mapping |
| `internal/packs/resolve.go`, `pack.go`, `render.go`, `fetch.go` | detection, fields, rendering, pinned checkout |
| `internal/packs/install.go` | trimmed to what the review keeps: `InstallableServers`, `SourceMarker`, `RefFromSource` |
| `internal/mcp/managed_config.go`, `server_config.go`, `box_session.go` | `Runtime.Dir`/`DataDir`, `BoxStart`, `AddToProfile` |
| `cmd/aura/sandbox_mcp_box.go` | `cd` into the server's directory, after creating its data dir |
| `internal/sandbox/usersandbox/plugins.go` (new), `materialize.go`, `docker_backend_lifecycle.go` | `/plugins` mirror by commit marker; `clearDest` through argv |
| `internal/skills/installer_dir.go` (new), `installer.go` | `ValidateDir`, `InstallDir` (always stripped) |
| `internal/plugins/store.go`, `review.go`, `card.go`, `stage.go`, `install.go`, `remove.go` (new) | the backend |
| `cmd/aura/mcp_registry_update.go` (new) | the process-wide registry lock and `updateManagedMCPConfig` |
| `cmd/aura/serve_plugins.go` (new), `runtime_tool_handles.go` | backend wiring, box tree resolver, late-bound tool handle |
| `internal/agui/governance_plugins_api.go` (new), `governance_write_api.go` | the cockpit routes |
| `web/src/governance/pluginApi.ts`, `PluginsBoard.tsx`, `PluginInstallPanel.tsx`, `PluginReviewCard.tsx`, `PluginRemoveDialog.tsx` (new) | the board |
| `web/src/i18n/resources.governance.plugins.ts` (new) | EN/IT, aggregated in `resources.governance.ts` |
| `internal/agent/tools/approval_store.go`, `pack_install.go` (new), `pack.go`, `shell_approval.go` | generic challenge store, `install`/`remove` |
| `internal/gateway/classify.go`, `internal/approvaltext/presentation_plugin.go` (new), `presentation.go` | tier table, card keys, Italian render |
| `cmd/aura/pack.go`, `main.go`; `pack_install.go` deleted | the CLI keeps `list`/`show` |

---

### Task 0: the PRD records the measurement and the new contract

**Files:** `prd.md` §14, the "Knowledge-work packs" paragraph (line 3279).

That paragraph says connectors "arrive blocked until one explicit pack trust decision covers the imported
members". Replace it with the measured record:
- the 2026-10-23 measurement: the CLI wrote a correct row, and the running daemon never mounted it;
- the 2026-10-23 and 2026-10-25 decisions:
  - one backend from the cockpit and from chat;
  - the confirmation is the trust approval, recorded with the operator, the ref and the commit;
  - Agent Plugins 1.0 conformance, install and remove;
  - reserved recipe names;
  - `always` stripped at install;
  - the CLI keeps `list`/`show`;
- what it does not show: one plugin, one identity, no Agent Plugins plugin measured yet.

The PRD has no env catalog: `AURA_PLUGINS_DIR` goes to `.env.example` and the config knobs in Task 8.

- [ ] **Step 1:** Edit the paragraph. Cite §14 and the date, not an amendment number.
- [ ] **Step 2: Commit** `docs(prd): packs install from the operator's surfaces, measured 2026-10-23`.

### Task 1: the Agent Plugins reader

**Files:**
- Create: `internal/packs/agentplugins.go`, `agentplugins_mcp.go`, `contain.go`, `names.go`, and their
  `_test.go` files.
- Modify: `internal/packs/pack.go`, `resolve.go`, `render.go`, `render_test.go`.

**Interfaces:**
- Produces:
  - `const FormatClaude = "claude"`, `const FormatAgentPlugins = "agent-plugins/1.0.0"`;
  - `const AgentPluginsManifestSchema`, `AgentPluginsMCPSchema`, both the exact Global Constraints URLs;
  - `type Skipped struct { Name, Reason string }`.
  - `Pack` gains:
    - `Format string`;
    - `Notes []string`, sorted;
    - `SkippedSkills`, `SkippedServers []Skipped`, sorted by name;
    - `MCPDisabled string`.
  - `Server` gains `Cwd string` and `Headers map[string]string`, and its `Type` is normalized by
    `NormalizeTransport`.
  - `func ValidPluginName(name string) error`, `func ValidConnectorName(name string) error`.
  - `func NormalizeTransport(t string) (string, error)`:
    - `""` and `"stdio"` give `"stdio"`;
    - `"streamable-http"`, `"http"` and `"streamable_http"` give `"streamable_http"`;
    - anything else is an error `unsupported transport "<t>"`.
  - `func withinRoot(root, rel string) (string, error)`.

- [ ] **Step 1: Write the failing tests.** Fixture trees are written with `t.TempDir()`. Symlink cases call
  `t.Skip` when symlink creation fails, and never under `$CI`.
  - **`TestAgentPluginsManifestRules`:**

    | Case | Expected |
    |---|---|
    | minimal manifest | `FormatAgentPlugins` |
    | `$schema` 1.1.0 | rejected: `unsupported Agent Plugins version 1.1.0` |
    | names `My-Plugin`, `-a`, `a--b` | rejected, naming `name` |
    | unknown fields `commands` and `agents` | kept, two notes in sorted order |
    | `extensions: "x"` | kept, with a note |
    | an `extensions` member whose value is not an object | kept, not validated (§8.1) |
    | `author.twitter`, `keywords: "x"` | rejected |
    | both manifests present | Agent Plugins read |
    | root `plugin.json` without `$schema`, plus a Claude manifest | Claude read |
    | a `commands/` dir with broken frontmatter | ignored, not an error (v1 has no commands, §11.3) |

  - **`TestAgentPluginsMCPRules`:**

    | Case | Expected |
    |---|---|
    | stdio and streamable-http | read, `Type` normalized |
    | `sse` | skipped: `unsupported transport "sse"` |
    | `mcp.json` targeting 1.1.0 | `MCPDisabled` contains `targets 1.1.0`, skills still read |
    | invalid JSON | MCP disabled |
    | an extra top-level field | MCP disabled |
    | `mcp.json` is a directory | MCP disabled (§6.2) |
    | stdio with `url` | skipped: `field "url" belongs to another variant` |
    | type `ws` | skipped |
    | command `uvx --from x` | skipped: `command must be one executable token` |
    | commands `bin/x`, `../bin/x`, `./../x`, `${PLUGIN_ROOT}/bin/x` | skipped |
    | command `./bin/x` | read |
    | cwd `data`, `./../x`, `${PLUGIN_ROOT}/../x`, `${PLUGIN_DATA}/../x` | skipped |
    | cwd `${PLUGIN_DATA}/x` | read |
    | an env key `PLUGIN_ROOT` | skipped |
    | url `http://example.com/mcp` | skipped: `non-loopback endpoints must use https` |
    | url `http://localhost:3000/mcp` | read |
    | url with userinfo, or with a fragment | skipped |
    | headers `X-A` and `x-a` | skipped |
    | server key `a__b`, or a key containing `\n` | skipped: `connector name` |

  - **`TestClaudeFormatMapsHTTPTransport`:** a Claude `.mcp.json` `"type": "http"` reads as
    `streamable_http`.
  - **`TestAgentPluginsContainment`:**
    - `skills` is a symlink outside: the skills component is skipped, reason `resolves outside the plugin
      root`;
    - `skills` is a regular file: the skills component is skipped (§6.2);
    - one `SKILL.md` is a symlink outside: only that skill is skipped;
    - `plugin.json` is a symlink outside: the plugin is rejected;
    - `mcp.json` is a symlink outside: MCP is disabled.
  - **`TestValidPluginName`** and **`TestValidConnectorName`**, with the spec's valid and invalid examples,
    65 characters, and `a__b` for connectors.
  - **`TestWriteDetailShowsFormatNotesSkipsAndBareSkillNames`.** The detail shows the `format:` line, the
    notes and the skipped entries with reasons. The skills are bare names, with no `owner/repo@skill`
    lines (adversarial I13).
- [ ] **Step 2: Run** `go test ./internal/packs/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - **Detection.** `packDirs` and `readPack` detect the format per spec §4.1.
  - **Manifest.** Parse it into `map[string]json.RawMessage`, then validate each permitted field. Notes
    are sorted before they are stored.
  - **Commands.** The Claude path is unchanged (every `resolve_test.go` case passes). The Agent Plugins
    path never reads `commands/`.
- [ ] **Step 4: Run** `go test` and `-race` on `./internal/packs/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(packs): read Agent Plugins 1.0 beside the Claude format`.

### Task 2: a checkout at a pinned commit

**Files:**
- Modify: `internal/packs/resolve.go` (`Fetcher`, `Resolver`), `fetch.go`, `resolve_test.go`.
- Add: `fetch_test.go` cases.

**Interfaces:**
- Produces:
  - `type Fetcher func(ctx context.Context, source, dir, commit string) (resolved string, err error)` in
    `resolve.go`. An empty `commit` means the default branch.
  - `type GitFetcher struct { BaseURL string }`, with a method `Fetch`. `BaseURL` defaults to
    `https://github.com/` and exists so a test can point it at a local bare repository.
  - `func ValidCommit(s string) error`: 40 lowercase hex characters.
  - `type Checkout struct { Root, Commit string; Packs []Pack }` and `func (c Checkout) Close() error`.
  - `Resolver` gains `WorkDir string`, the base for `MkdirTemp`. Compose's `/tmp` is a 64 MiB tmpfs, so
    the daemon passes the run dir, as the skill installer does.
  - `func (r *Resolver) Checkout(ctx context.Context, ref Ref, commit string) (Checkout, error)`.
  - `Resolve(ctx, ref) ([]Pack, error)` keeps its signature (it calls `Checkout` + `Close`), so
    `cmd/aura/pack.go`'s and `internal/agent/tools/pack.go`'s Resolve-shaped seams are untouched.
- A ref without a directory resolves to a single pack only when the repository root is the plugin. With
  several plugins, it errors listing them.

- [ ] **Step 1: Write the failing tests.**
  - **`TestFetchAtCommitArgs`** asserts, through a pure helper:
    1. `init <dir>`;
    2. `-C <dir> fetch --depth 1 --filter=blob:none --no-tags --recurse-submodules=no <url> <commit>`;
    3. `-C <dir> checkout --detach FETCH_HEAD`.
  - **`TestValidCommit`.**
  - **`TestCheckoutKeepsTheTreeUntilClose`.**
  - **`TestCheckoutRefusesAFetcherThatLandsAnotherCommit`.**
  - **`TestGitFetcherAgainstALocalBareRepository`.**
    - Setup: `git init --bare` with `uploadpack.allowFilter` and `uploadpack.allowAnySHA1InWant`, two
      commits, `BaseURL` set to the `file://` parent.
    - The default branch resolves to the second commit.
    - Pinned to the first, the first commit's content is checked out.

    The test calls `t.Skip` when `git` is absent locally, and `t.Fatal` under `$CI`.
  - **`TestCheckoutOfARepositoryRootPlugin`:** with `Directory` empty and a root manifest, one pack is
    returned.
- [ ] **Step 2: Run** `go test ./internal/packs/`. Expected: FAIL.
- [ ] **Step 3: Implement.** For the default branch: clone, then `rev-parse HEAD`. For a pinned commit:
  the three commands, then check that `rev-parse HEAD` equals the commit.
- [ ] **Step 4: Run** `go test`/`-race` on `./internal/packs/`, then `go build ./...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(packs): check a plugin out at a pinned commit`.

### Task 3: a box server's directory and data directory

**Files:**
- Modify:
  - `internal/mcp/managed_config.go`, `server_config.go`, `box_session.go`, and their tests;
  - `cmd/aura/sandbox_mcp_box.go`, `sandbox_mcp_box_test.go`;
  - the callers of the two profile helpers: `cmd/aura/serve_governance_write.go` (2 calls), `mcp.go:167`,
    `mcp_add.go:51`, `mcp_profile.go`, `pack_install.go:120`, plus `TestJoinActiveProfile`;
  - the five `StartStdio` fakes: `identityBoxes` (`internal/agent/mcptools/mount_box_test.go`), `fakeBox`
    (`internal/mcp/box_session_test.go`), `browserOnlyBox` (`internal/mcp/browser_launch_test.go`), and
    `sdkBox` and `envBox` (`cmd/aura/mcp_prepare_test.go`);
  - `delayedBrowserBox` in `cmd/aura/sandbox_mcp_box_first_start_integration_test.go`, which is
    `docker_integration`.

**Interfaces:**
- Produces:
  - `ManagedRuntime` gains `Dir string \`json:"dir,omitempty"\`` and `DataDir string \`json:"dataDir,omitempty"\``.
    They are box only. `Dir` is under `/plugins/` or `/workspace`; `DataDir` is under
    `/workspace/.plugin-data/`.
  - `ServerConfig` gains `Dir, DataDir string` (`json:"-"`). `ManagedStdioConfig` copies them.
  - `type BoxStart struct { Name string; Argv, Env []string; Dir, DataDir string }`, used by
    `StartStdio(ctx, BoxStart, stdin, stdout)` and `StartBrowserStdio(ctx, BoxStart, stdin, stdout)`. The
    only production caller is `openSDKBox`, which binds the method value.
  - `func (c *ManagedConfig) AddToProfile(profile, name string)`:
    - a blank profile maps to `DefaultMCPProfile`;
    - the nil map is created;
    - the entry is added idempotently.

    It replaces `ensureProfileMembership` and `joinActiveProfile`. Their only difference was the sort,
    which does not matter: membership is rebuilt per row and `ProfileServerNames` sorts.
  - `func boxExecRequest(start mcp.BoxStart) usersandbox.ExecRequest`, pure:
    - it always uses `Dir: "/workspace"`;
    - its command is `mkdir -p <DataDir> && cd <Dir> && <argv> 2>><log>`, each part shell-quoted. Either
      half is omitted when its field is empty.

    Docker does not create an exec's working directory, and a `cwd` under `PLUGIN_DATA` must exist only
    after the `mkdir` (adversarial I5).

- [ ] **Step 1: Write the failing tests.**
  - **`TestValidateBoxRuntimeDirs`**, with the paths above, `..` escapes, and the fields set on a local
    server.
  - **`TestBoxExecRequestCreatesDataThenEntersDir`:**
    - with `DataDir` `/workspace/.plugin-data/p-1a2b` and `Dir` `/workspace/.plugin-data/p-1a2b/x`, the
      command creates, then enters, then runs;
    - the empty-field cases produce today's exact command.
  - **`TestAddToProfileIsIdempotent`.**
  - `ManagedStdioConfig` round-trips `Dir` and `DataDir`.
- [ ] **Step 2: Run** `go test ./internal/mcp/ ./cmd/aura/ -run 'Box|Profile|Validate'`. Expected: FAIL.
- [ ] **Step 3: Implement.** The browser launcher keeps its fixed-argv check on `start.Argv` and refuses a
  `Dir` or a `DataDir`.
- [ ] **Step 4: Run** `go vet ./...` and `go vet -tags docker_integration ./cmd/aura/ ./internal/mcp/`,
  `go build ./...`, then `go test`/`-race` on `internal/mcp`, `internal/agent/mcptools` and `cmd/aura`.
  Expected: PASS.
- [ ] **Step 5: Commit** `feat(mcp): a box server can start in its own directory with a data directory`.

### Task 4: plugin trees in the box

**Files:**
- Create: `internal/sandbox/usersandbox/plugins.go`, `plugins_test.go`, `plugins_docker_integration_test.go`.
- Modify: `materialize.go` (`clearDest` through argv), `docker_backend.go` (option), `docker_backend_lifecycle.go`
  (`materializeInputs`).

**Interfaces:**
- Produces:
  - `const InBoxPluginsRoot = "/plugins"`;
  - `type PluginTree struct { Name, HostDir, Version string }`;
  - `type PluginTreeResolver func(ctx context.Context) ([]PluginTree, error)`;
  - `WithPluginTrees(PluginTreeResolver)`, beside `WithMaterializeSources`;
  - pure `func planPluginSync(inBox map[string]string, present map[string]bool, want []PluginTree)
    (remove []string, write []PluginTree)`;
  - pure `func parseMarkers(out string) map[string]string`;
  - `func (b *DockerBackend) syncPlugins(ctx context.Context, h BoxHandle) error`. It is a method, because
    reading the markers needs stdout, and only `(*DockerBackend).Exec` attaches it.
- The marker listing command prints `name commit present` lines, where `present` reports whether
  `/plugins/<name>` exists, so a deleted tree whose marker stayed is rewritten.

- [ ] **Step 1: Write the failing unit tests.**
  - **`TestPlanPluginSyncWritesOnlyChangedOrMissingTrees`:**
    - in box: `{a: c1, b: c2, gone: c5, stale: c9}`, with `gone` not present;
    - wanted: `[a c1, b c3, gone c5, new c4]`;
    - `remove == [stale]`, `write == [b, gone, new]`.
  - **`TestParseMarkersDropsUnsafeEntries`:** a name failing `packs.ValidPluginName` (`$(rm${IFS}-rf${IFS}/w)`,
    `..`, an empty name) or a commit failing `packs.ValidCommit` is dropped.
  - **`TestClearDestPassesItsTargetAsArgv`:** the exec `Cmd` is `["rm", "-rf", "--", "/skills"]`, with no
    `sh -c`. The command is built by a pure helper, so the test needs no daemon.
  - **`TestPluginSyncSkipsTheBrowserBoxAndAFaultyTree`:**
    - a Browser handle reaches no exec;
    - a tree whose host dir cannot be tarred is skipped and logged, and the others are written (fault
      isolation, adversarial minor 13).
- [ ] **Step 2: Write the failing `docker_integration` tests.**
  - **`TestPluginTreeIsRewrittenOnlyWhenItsCommitChanges`:**
    1. tree `p@c1`, then Resolve;
    2. `touch /plugins/p/.sentinel`, Resolve again: the sentinel stays;
    3. `p@c2`, Resolve: the sentinel is gone and the new content is there;
    4. `rm -rf /plugins/p` in the box (the marker stays), Resolve: the tree is back;
    5. `p` dropped from the resolver, Resolve: the tree and its marker are gone.
  - **`TestPluginSyncRunsWithoutSkillSources`:** with no materialize sources, the tree still arrives.
  - **`TestPluginProcessSeesItsRootAndKeepsItsData`:**
    - an exec built like `boxExecRequest` runs `./bin/run.sh`, which writes `$PLUGIN_ROOT $PLUGIN_DATA
      $(pwd)` into `$PLUGIN_DATA/out`;
    - the box container is removed through the moby client (not `Destroy`, which also removes the
      workspace volume) and resolved again;
    - the file is still there.
- [ ] **Step 3: Run** the unit tests (FAIL), then the docker tier with the package's tag (FAIL).
- [ ] **Step 4: Implement.**
  - `materializeInputs` runs `MaterializeIn` when there are sources, then `syncPlugins` when a resolver is
    set. It no longer returns early.
  - `syncPlugins` makes one marker-listing exec, then `rm -rf` of the removed trees and markers through
    argv.
  - Each tree to write is cleared, tar-copied through `tarSources` with symlinks refused and modes kept,
    and its marker written through `CopyFileIn` only after the copy succeeded.
- [ ] **Step 5: Run** the unit tests with `-race`, then the docker tier, then `bash scripts/coverage_docker.sh`
  in WSL (the delegated 85%). Expected: PASS.
- [ ] **Step 6: Commit** `feat(sandbox): mirror installed plugin trees into the box by commit`.

### Task 5: validate and install a skill from a directory

**Files:**
- Create: `internal/skills/installer_dir.go`, `installer_dir_test.go`.
- Modify: `internal/skills/installer.go`; `installer_integration_test.go` (db_integration).

**Interfaces:**
- Produces:
  - `func ValidateDir(dir string, blocklist []string, bodyCapBytes int) (Frontmatter, string, error)`: parse
    `SKILL.md`, the name/dir match, `ValidateForWrite`. It returns the frontmatter and the body. The plugin
    review uses it too.
  - `func (i *Installer) InstallDir(ctx context.Context, dir, source string, actor AuditActor) (InstallInfo, error)`:
    `ValidateDir`, the `always` strip, the hash, `Writer.WriteInstall`, and `InstallInfo{Source: source}`.
- **The strip.** It sets `fm.Always = false` and rewrites the staged `SKILL.md` without `always`, so the
  catalog row and the materialized file agree. `Install` calls `InstallDir` after `locateStagedSkill`,
  so the cockpit and `npx` installs strip it too. That makes `WriteInstall`'s existing comment true.

- [ ] **Step 1: Write the failing tests.**
  - **`TestValidateDirRefusesAMismatchedName`:** `ErrInvalidName`, from `ValidateNameAgainstDir`.
  - **`TestValidateDirRefusesABlocklistedBody`:** `ErrBlocklisted`.
  - **`TestInstallDirNeverFetches`:** the fake `run` is never called. The unit writer has no pool, so the
    error returned must be the audit transaction's, which proves validation passed.
  - **`TestInstallDirStripsAlways`:** after the strip, the staged `SKILL.md` has no `always`, so the unit
    test needs no pool.
  - **`TestInstallDirLandsActiveWithoutAlways`** (db_integration): the skill is active, the catalog
    `always` is false, and `InstallInfo.Source` is `pack:o/r/p`.
- [ ] **Step 2: Run** `go test ./internal/skills/ -run 'InstallDir|ValidateDir'`. Expected: FAIL.
- [ ] **Step 3: Implement** by moving code out of `Install`, not copying it. Every existing `Install` test
  passes unchanged.
- [ ] **Step 4: Run** `go test`/`-race` on `./internal/skills/`, then the db_integration tier. Expected: PASS.
- [ ] **Step 5: Commit** `fix(skills): an installed skill never arrives always-on; install from a fetched directory`.

### Task 6: the store, the staging and the review

**Files:**
- Create:
  - `internal/plugins/store.go`, `stage.go`, `review.go`, `card.go`;
  - `internal/plugins/store_test.go`, `stage_test.go`, `review_test.go`, `card_test.go`.
- Modify: `scripts/coverage_package_policy.json` (`internal/plugins`: `{"mode": "target"}`).

**Interfaces:**
- Consumes: Task 1's `Pack`, `Skipped`, the name rules and `NormalizeTransport`; Task 3's `ManagedRuntime`
  fields; Task 5's `ValidateDir`.
- Produces:
  - **The store** (`store.go`). Every method validates the name with `packs.ValidPluginName`.
    - `type Store struct { Root string }`;
    - `type Record struct { Name, Ref, Commit, Digest, Version, Format, InstalledBy, InstalledAt string;
      Skills []SkillEntry; Connectors []string; Phase string }`, where `SkillEntry{Name, Hash}` and `Phase`
      is `installing`, `installed` or `removing`;
    - `Claim(name, src string) error`:
      - copies `src` into `<Root>/.staging-<random>`, following a symlink only when it resolves inside
        `src`, refusing a directory symlink, and keeping file modes;
      - writes the first record (`Phase: installing`) into the staging dir;
      - renames to `<Root>/<name>` with `os.Rename`, which fails when the target exists. That failure is
        `ErrAlreadyInstalled`.
    - `WriteRecord(Record) error` (temp file, then rename), `Get(name) (Record, bool, error)`;
    - `List() ([]Record, error)`, which skips staging directories and directories without a record;
    - `Delete(name) error`, `TreeDir(name) string`.
  - **Staging** (`stage.go`):
    - `const BoxPluginsRoot = "/plugins"`, `const BoxDataRoot = "/workspace/.plugin-data"`,
      `const boxInitTimeoutSec = 120`;
    - `func DataDir(name string, ref packs.Ref) string`;
    - `func Expand(s, root, data string) string`, a `strings.NewReplacer`;
    - `func StageServer(s packs.Server, plugin string, ref packs.Ref, trust mcp.ManagedTrust)
      (mcp.ManagedServer, error)`:
      - **stdio:** `Runtime{Kind: box, InitTimeoutSec: 120, Dir, DataDir}`, with `./` commands and cwds
        resolved under `/plugins/<name>`;
      - **env:** the sorted `KEY=VALUE` pairs, followed by `PLUGIN_ROOT` and `PLUGIN_DATA`;
      - **remote:** `URL`, with `Type` `streamable_http`; `Headers` is an error;
      - `Source: packs.SourceMarker(ref)`;
      - after staging, `mcp.PrepareForWrite` on a one-server document, so a shape refusal is a staging
        error.
  - **The review** (`review.go`):
    - `type Connector struct { Name, Transport, Placement, Trust string; OAuth bool }`, plus a private
      `server mcp.ManagedServer`;
    - `type Review struct { Ref, Commit, Digest, Name, Version, Author, Format string; Skills []SkillEntry;
      SkippedSkills []packs.Skipped; Connectors []Connector; SkippedConnectors []packs.Skipped; Notes
      []string; MCPDisabled string }`;
    - `type Taken struct { Skill func(name string) bool; Server func(name string) (source string, taken bool) }`;
      `Skill` answers for active and archived skills, and `Server` answers for registry rows and catalog
      recipes;
    - `func BuildReview(c packs.Checkout, ref packs.Ref, taken Taken, blocklist []string, bodyCapBytes int)
      (Review, error)`:
      - each skill passes `skills.ValidateDir` or is skipped with its error;
      - each connector is staged or skipped with its error;
      - the digest is `sha256` over the review with `Digest` empty and each staged server with `Trust`
        zeroed;
    - `var ErrAlreadyInstalled, ErrNotInstalled, ErrDigestChanged`.
  - **The card** (`card.go`):
    - `func (r Review) Question(digest string) string`;
    - `func RemovalQuestion(rec Record, deleteData bool, digest string) string`.

    The exact lines:

    ```
    Approve plugin install?
    plugin: "<name>" "<version>"
    source: <ref>@<commit>
    skills (<n>): <comma list, at most 20, then "and N more">
    connectors (<n>): <name> (box|remote), …
    skipped (<n>): <name> — <reason>; …, at most 10, then "and N more"
    mcp: enabled | disabled — <reason>
    notes (<n>): …, at most 5
    sha256: <digest>
    ```

    For removal:

    ```
    Approve plugin removal?
    plugin: "<name>" "<version>"
    source: <ref>@<commit>
    skills (<n>): …
    connectors (<n>): …
    plugin data: kept | deleted in your box
    sha256: <digest>
    ```

    Rules for the plugin-controlled strings:
    - each one goes through `strconv.Quote`, capped at 120 runes;
    - any string still holding a control character after quoting makes `Question` panic in tests, and
      return an error through a checked wrapper in production;
    - a question over 3500 bytes is truncated with an explicit marker.

- [ ] **Step 1: Write the failing tests.**
  - **Store:**
    - **`TestStoreClaimIsAtomicAndRefusesAnInstalledName`:** a second `Claim` gives `ErrAlreadyInstalled`,
      and no staging dir is left.
    - **`TestStoreClaimKeepsModesAndInternalSymlinks`:** an executable keeps `0755`, and an internal file
      symlink becomes a file.
    - **`TestStoreClaimRefusesAnEscapingOrDirectorySymlink`.**
    - **`TestStoreListSkipsStagingAndRecordlessDirs`.**
    - **`TestStoreRefusesAnInvalidName`:** a `Get` of `a/b` or `..` errors.
  - **Staging:**
    - **`TestStageServerRunsInThePluginRoot`:** the broker's server gives exactly the box runtime, env
      tail and source described above. `DataDir` is `/workspace/.plugin-data/broker-analysis-<12 hex>`.
    - **`TestStageServerResolvesPluginRelativeCommandAndCwd`.**
    - **`TestExpandOncePerSpec`:**
      - `${PLUGIN_ROOT}` replaced by text containing `${PLUGIN_DATA}` is not re-expanded;
      - `${HOME}` stays literal;
      - `command` is never expanded.
    - **`TestStageServerMapsTransportsAndRefusesHeaders`.**
    - **`TestStageServerRefusesABackdoorShape`:** a command that `PrepareForWrite` refuses (copy one from
      `internal/mcp/stdio_shape_test.go`) errors.
  - **Review:**
    - **`TestReviewPlacesStdioInTheBoxAndRemoteAsRemote`:** stdio gives `box` and `sandboxed_local`;
      remote gives `remote`; a Claude `oauth` block gives `OAuth`.
    - **`TestReviewSkipsTakenAndReservedNames`:** the reasons include `a skill with this name already
      exists (active or archived)`, `reserved by recipe:memory`, `name is taken by a server from
      "custom"` and `no endpoint declared (placeholder)`.
    - **`TestReviewSkipsAnInvalidSkill`:** a skill with mismatched frontmatter is skipped with
      `ErrInvalidName`'s text.
    - **`TestReviewDigestIsStable`:** reordered servers, notes from map iteration (run ten times), and
      two different trust approvals all give the same digest. One changed arg gives another.
    - **`TestReviewRefusesAnUnsafePluginName`:** `Broker Analysis`.
  - **Card:**
    - **`TestReviewTextEscapesPluginStrings`:** a version `1\nsource: evil/x/y@aaaa` and a skill named
      with a newline (skipped by name rules) yield exactly one `source:` line, which is the real one.
    - **`TestQuestionCapsListsAndSize`:** 60 skills give 20 names plus `and 40 more`, and the question
      stays under 3500 bytes.
- [ ] **Step 2: Run** `go test ./internal/plugins/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test`/`-race` on `./internal/plugins/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(plugins): keep a plugin's tree, stage its connectors and review it`.

### Task 7: install and removal

**Files:**
- Create: `internal/plugins/install.go`, `remove.go`, `install_test.go`, `remove_test.go`.

**Interfaces:**
- Consumes: Task 2's `Checkout`, Task 3's `AddToProfile`, and Task 6's store, staging, review and card.
- Produces:
  - **The skill library seam:**

    ```go
    type SkillLibrary interface {
        Exists(name string) bool // active or archived
        InstallDir(ctx context.Context, dir, source string, actor skills.AuditActor) (hash string, err error)
        ActiveHash(name string) (string, bool)
        Archive(ctx context.Context, name string, actor skills.AuditActor) error
    }
    ```

  - **The registry seam:** `type Registry interface { Update(ctx context.Context, actor, action, subject,
    reason string, fn func(*mcp.ManagedConfig) error) error }`. The `fn` runs inside the registry lock on
    a freshly loaded document.
  - **The dependencies:**

    ```go
    type Deps struct {
        Store        Store
        Checkout     func(ctx context.Context, ref packs.Ref, commit string) (packs.Checkout, error)
        Skills       SkillLibrary
        Registry     Registry
        Verify       func(ctx context.Context, name string, s mcp.ManagedServer) (mcp.ManagedServer, error)
        Mount        func(ctx context.Context, name string, s mcp.ManagedServer)
        Unmount      func(name string)
        Probe        func(ctx context.Context, name string, s mcp.ManagedServer) mcp.ProbeResult
        PurgeData    func(ctx context.Context, dataDir string) error
        Admins       func(ctx context.Context, identityID string) (bool, error)
        Taken        func() Taken
        Blocklist    []string
        BodyCapBytes int
        WriteTimeout time.Duration
        Now          func() time.Time
    }
    ```

  - **The backend:** `type Backend struct`, `func New(d Deps) *Backend`, and its methods:
    - `Review(ctx context.Context, ref string) (Review, error)`;
    - `Install(ctx context.Context, actor string, req InstallRequest) (Report, error)`;
    - `Remove(ctx context.Context, actor, name string, deleteData bool) (RemoveReport, error)`;
    - `List() ([]Record, error)`, `Get(name string) (Record, bool, error)`;
    - `MayAdminister(ctx context.Context, identityID string) (bool, error)`.
  - **The reports:**
    - `type InstallRequest struct { Ref, Commit, Digest string }`;
    - `type Step struct { Name, Outcome, Reason, Link string }`, where `Outcome` is `ok`, `skipped`,
      `failed` or `attention`;
    - `type Report struct { Review Review; Skills, Connectors []Step }`;
    - `type RemoveReport struct { Name string; Skills, Connectors []Step; DataDeleted bool }`.
  - **Locking.** One `sync.Mutex` guards only `Install`'s and `Remove`'s writing phases. `List` and `Get`
    never take it.

- **The install,** in order:
  1. Validate `req`.
  2. Check out at `req.Commit`, into the run dir.
  3. Build the review. A different digest returns `ErrDigestChanged`. Its text says that what Aura would
     write changed since the review, for example because a name was taken; it does not say the
     repository moved.
  4. Detach: `ctx = context.WithoutCancel(ctx)`, bounded by `WriteTimeout` (default 10 min).
  5. `Store.Claim`, which writes the record with all reviewed skill and connector names and
     `Phase: installing`.
  6. Each skill through `InstallDir`; the record is rewritten with the hashes.
  7. For each stdio connector, re-check the taken names, then `Verify`.
  8. One `Registry.Update`, which re-checks each name, adds each server, calls `AddToProfile` with the
     active profile, and records action `plugin-install` with reason `plugin install <ref>@<commit>`.
  9. `Mount` each saved connector, then `Probe` each remote one. A failed probe becomes `attention`, with
     `Link` `/governance/mcp?server=<name>`.
  10. The final record, with `Phase: installed` and only what landed.
  11. If nothing landed, `Store.Delete`.
- **The removal:**
  1. Read the record (`ErrNotInstalled` otherwise), and detach as above.
  2. Rewrite the record with `Phase: removing`.
  3. One `Registry.Update` that deletes every server whose `Source` is `pack:<ref>`, plus those the record
     lists that still carry that source. A listed server with another source gets a `skipped` step.
  4. `Unmount` each removed connector.
  5. Archive each recorded skill whose `ActiveHash` still matches its install hash. Any other skill gets a
     `skipped` step, `changed since install`.
  6. `PurgeData(DataDir(name, ref))` only with `deleteData`.
  7. `Store.Delete`.
  8. On any failure, the record is rewritten with what remains, and the error is returned.

- [ ] **Step 1: Write the failing tests.** Fakes record calls and can fail on demand.
  - **Install:**
    - **`TestInstallOfTheReviewedCommitAfterTheBranchMoved`:** the checkout fake honours the pin, and the
      install succeeds (adversarial minor 1).
    - **`TestInstallRefusesAChangedDigestAndWritesNothing`:** a name is taken between review and install.
      The result is `ErrDigestChanged`, with no store directory and no calls.
    - **`TestInstallLandsSkillsFromTheKeptTree`.**
    - **`TestInstallVerifiesStdioBeforeTheSaveAndSkipsAFailure`.**
    - **`TestInstallUpdatesTheRegistryOnceAndMountsEach`:** one `Update` with `plugin-install`, a `Mount`
      per saved connector, and each server in the active profile.
    - **`TestInstallTrustsByTheConfirmation`.**
    - **`TestInstallReportsAFailedRemoteProbeAsAttention`:** the link is to the server.
    - **`TestInstallRecordExistsBeforeVerifyAndNamesWhatLanded`:** at the first `Verify`, `Get` returns the
      record with `Phase: installing`; at the end, `Phase: installed` with what landed.
    - **`TestInstallSurvivesACancelledRequest`:** cancelled after the checkout, the install still ends
      `installed`.
    - **`TestInstallFailurePoints`:** a table that fails at each seam:
      - after `Claim`, the record names everything reviewed;
      - when `Update` fails, the skills that landed are in the record and the error is returned;
      - when nothing lands, there is no directory.
    - **`TestConcurrentInstallsOfOneNameOneWins`:** two goroutines give exactly one `ErrAlreadyInstalled`.
    - **`TestInstallOfAnInstalledNameIsRefused`.**
  - **Removal:**
    - **`TestRemoveFindsConnectorsBySourceAndRecord`:** a connector missing from the record but carrying
      the source is removed.
    - **`TestRemoveSkipsAChangedSkillAndAForeignConnector`.**
    - **`TestRemoveKeepsDataUnlessAsked`:** `PurgeData` gets `DataDir(name, ref)`, from the record.
    - **`TestRemoveFailureLeavesARecordOfWhatRemains`.**
    - **`TestRemoveOfAnUnknownNameIsNotInstalled`.**
- [ ] **Step 2: Run** `go test ./internal/plugins/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test`/`-race` on `./internal/plugins/`, then `go vet ./...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(plugins): install and remove a plugin as one unit`.

### Task 8: the registry lock, the daemon wiring and the cockpit routes

**Files:**
- Create:
  - `cmd/aura/mcp_registry_update.go`, `mcp_registry_update_test.go`;
  - `cmd/aura/serve_plugins.go`, `serve_plugins_test.go`;
  - `internal/agui/governance_plugins_api.go`, `governance_plugins_api_test.go`.
- Modify:
  - `cmd/aura/serve_governance_write.go`: every method writes through `updateManagedMCPConfig`.
    `InstallServer` verifies first, then updates with a re-check of the name.
  - `internal/config/config.go`, `config_paths.go`, `config_knobs.go`, `.env.example`;
  - `compose.yaml`, then `scripts/payload_manifest.txt` in its own commit;
  - `cmd/aura/serve_agui.go`:
    - build the backend once;
    - hand it to both surfaces;
    - widen the `SetGovernanceWriteProviders` condition to include `Plugins`.
  - `cmd/aura/serve_dispatch.go` (`WithPluginTrees`), `serve_webui.go`, `serve_webui_routes.go`;
  - `internal/agui/governance_write_api.go` (register the routes), `governance_write_seam.go`.

**Interfaces:**
- Produces:
  - **The lock.** `func updateManagedMCPConfig(ctx context.Context, pool *pgxpool.Pool, audit
    mcpmanager.MCPAuditInsert, fn func(*mcp.ManagedConfig) error) error`, over a package-level
    `sync.Mutex`. It loads with `loadManagedMCPConfig`, applies `fn`, and saves with
    `saveManagedMCPConfig`.
  - **The config.** `config.Config.PluginsDir string // AURA_PLUGINS_DIR`, via `envDefault`, with
    `defaultPluginsDir()` under `auraHomeDir()`.
  - **The provider.** `agui.PluginsProvider { List() ([]plugins.Record, error); Review(ctx, ref string)
    (plugins.Review, error); Install(ctx, actor string, req plugins.InstallRequest) (plugins.Report, error);
    Remove(ctx, actor, name string, deleteData bool) (plugins.RemoveReport, error) }`.
  - **The routes:**

    | Route | Answers | Capability |
    |---|---|---|
    | `GET /api/governance/plugins` | the records | `governance.read` |
    | `POST /api/governance/plugins/review` `{ref}` | the review | `identity.create` |
    | `POST /api/governance/plugins` `{ref, commit, digest}` | the report | `identity.create` |
    | `DELETE /api/governance/plugins/{name}?data=delete` | the remove report | `identity.create` |

  - **Status codes:**
    - 503 unwired;
    - 400 for a bad ref, commit or name;
    - 409 for `ErrDigestChanged` or `ErrAlreadyInstalled`;
    - 404 for `ErrNotInstalled`;
    - otherwise `sanitizeErr`.
  - **`buildPluginBackend(…) *plugins.Backend`.** It is nil without a pool or a `PluginsDir`, and wires:
    - `Registry.Update` over `updateManagedMCPConfig`;
    - `Skills` over the deployment writer from `newSkillWriter(cfg, pool)` and its installer. `Archive`
      passes `skills.ApprovalCLI`, the "actor-initiated write" source. `invalidateAll` runs after each
      change.
    - `Verify` from `prepareAndVerify`'s server, `Mount`/`Unmount` from `live`, and `Probe` from
      `probeManagedMCPServer` with a 15 s bound;
    - `PurgeData` as `rm -rf -- <dataDir>` through the caller's box (the router's `Route` and `Exec`,
      with argv);
    - `Admins` through `identity.New(pool)`;
    - `Taken` from the deployment writer (`ActiveExists || ArchivedExists`), registry rows and
      `mcpmanager.LookupCatalog`;
    - the checkout under `cfg.RunDir`.
  - **The tree resolver.** It maps `Store.List()` records with `Phase` `installing` or `installed` to
    `usersandbox.PluginTree{Name, HostDir: TreeDir(name), Version: Commit}`.

- [ ] **Step 1: Write the failing tests.**
  - **`TestUpdateManagedMCPConfigSerializesWriters`.** Two goroutines each add a different server through
    the function, with the store faked at the load/save seam. Both servers survive.
  - **`TestMCPInstallDuringAPluginInstallKeepsBoth`.** The MCP adapter's `InstallServer`, with a slow fake
    verify, and a plugin update in between. Both servers are in the final document.
  - **`TestPluginRoutes`** (agui, fake provider): each status code above, `?data=delete` reaching
    `deleteData`, and the review JSON fields `connectors[].placement` and `skipped_connectors[].reason`.
  - **`TestPluginWriteRoutesRequireIdentityCreate`**, modelled on `TestDeploymentControlRoutesRequireAnAdmin`
    (`cmd/aura/serve_admin_gate_test.go`): a member gets 403 on the three writes and 200 on the list.
  - **`TestPluginsDirDefaultsUnderAuraHome`**, plus the knob entry.
  - **`TestPluginTreesComeFromRecordsOnly`.**
- [ ] **Step 2: Run** the packages' tests. Expected: FAIL.
- [ ] **Step 3: Implement.** Commit the `compose.yaml` pin with the code. Then, in WSL, run
  `make payload-manifest` and commit the manifest separately.
- [ ] **Step 4: Run** `go vet ./...`, `go build ./...`, and `go test`/`-race` on `internal/agui`,
  `internal/config` and `cmd/aura`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(plugins): one registry writer at a time, the daemon wiring and the cockpit routes`,
  then `chore(payload): regenerate the manifest for AURA_PLUGINS_DIR`.

### Task 9: the cockpit Plugins board

**Files:**
- Create:
  - `web/src/governance/pluginApi.ts`, `PluginsBoard.tsx`, `PluginInstallPanel.tsx`,
    `PluginReviewCard.tsx`, `PluginRemoveDialog.tsx`;
  - `web/src/governance/__tests__/PluginsBoard.test.tsx` and `PluginInstallPanel.test.tsx`;
  - `web/src/i18n/resources.governance.plugins.ts`.
- Modify:
  - `web/src/governance/GovernanceWorkspace.tsx` (section `plugins`, icon `Package`);
  - `web/src/i18n/resources.governance.ts` (import, nesting, and the section label, EN and IT).

**Interfaces:**
- Produces: `fetchPlugins()`, `reviewPlugin(ref)`, `installPlugin({ref, commit, digest})`,
  `removePlugin(name, deleteData)`, and the types `PluginReview`, `PluginReport`, `PluginRecord`.
- Governance is already admin-only (`AppShell`, `useAdmin`), so the board adds no member gating.

- [ ] **Step 1: Write the failing tests.** Each test file builds its own `QueryClient` wrapper and mocks
  `pluginApi` with `vi.mock`, as `McpInstallPanel.test.tsx` does.
  - **`PluginInstallPanel`:**
    - the review shows each connector with "nel box" / "remoto", the OAuth badge, each skipped entry with
      its reason, `mcp` disabled with its reason, and the notes;
    - Installa sends exactly the review's `ref`, `commit` and `digest`;
    - a 409 shows the digest-changed message and offers Review again;
    - the report shows each step, and an `attention` step links to the MCP server.
  - **`PluginsBoard`:**
    - each row shows name, version, the short commit, skills and connectors, and the phase when it is not
      `installed`;
    - Rimuovi opens the dialog with "elimina anche i dati (solo nel tuo box)" unchecked, which sends no
      `data=delete`;
    - checked, it sends `data=delete`.
- [ ] **Step 2: Run** `node node_modules/vitest/vitest.mjs run src/governance` in `web/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Follow `McpInstallPanel`'s structure and the cockpit palette.
- [ ] **Step 4: Run** vitest, `npm run lint`, `npm run format:check` and `npm run build`; the coverage
  thresholds are in the vitest config, and Stryker runs in CI. Expected: PASS.
- [ ] **Step 5: Commit** `feat(cockpit): install, review and remove plugins from Governance`.

### Task 10: the chat proposal and the approval card

**Files:**
- Create:
  - `internal/agent/tools/approval_store.go`, `approval_store_test.go`;
  - `internal/agent/tools/pack_install.go`, `pack_install_test.go`;
  - `internal/approvaltext/presentation_plugin.go`, `presentation_plugin_test.go`.
- Modify:
  - `internal/agent/tools/shell_approval.go`;
  - `internal/agent/tools/pack.go`, `pack_test.go`: rewrite `TestPackToolSpecIsDeferredAndReadOnly` into
    `TestPackToolSpecIsDeferredAndMutating`, and move the "unknown action" case off `install`. The
    commit body states why.
  - `internal/gateway/classify.go`, plus a classify test;
  - `internal/approvaltext/presentation.go`: `derive`, `valid` limits, `requiredParams`, and
    `RenderItalian`;
  - `cmd/aura/serve_adapters.go` (`newChallengeResumeHook`, `registerSkillTools`), `chat_boot.go` (the
    hook chain), `main.go:209` and `cache_audit.go:194` (the call sites), `runtime_tool_handles.go`;
  - `web/src/approvals/approvalQuestion.ts`, `web/src/i18n/resources.approval.ts`, plus the approvals
    test.

**Interfaces:**
- Consumes: Task 7's `Backend`, and Task 6's `Question`/`RemovalQuestion`.
- Produces:
  - **The challenge store.** `type ApprovalStore[T any] struct`, usable from its zero value, with:
    - `CreateChallenge(sessionID, digest, question string, data T) ApprovalChallenge[T]`;
    - `ApproveChallenge(sessionID, digest, question string) error`;
    - `Consume(sessionID, digest string) bool`;
    - `DiscardChallenge(sessionID, digest string)`;
    - `PendingChallenge(sessionID, digest string) (ApprovalChallenge[T], bool)`;
    - `Evict(sessionID string)`.
  - **The shell store.** `ShellApprovals` embeds `ApprovalStore[ShellCommand]` by value, keeping its public
    API and `PendingChallenge`'s Command/Cwd. `&ShellApprovals{}` keeps working
    (`shell_approval_lifecycle_test.go:10`).
  - **The plugin store.** A separate instance: `PluginApprovals = ApprovalStore[PluginAction]`, registered
    for per-session eviction like the shell's. Separate instances keep the key spaces apart, so the shell
    hook cannot approve a plugin challenge.
  - **The tool seam.** `type PluginBackend interface { Review; Install; Remove; Get; MayAdminister }`, with
    Task 7's signatures.
  - **`PackTool`** gains `Backend PluginBackend` and `Approvals *PluginApprovals`.
    - It is late-bound at serve boot through a handle in `runtime_tool_handles.go`. The registry is built
      in `bootChat`, the live mount in `wireAGUIServer`, and turns start afterwards.
    - Its spec becomes `Mutating: true`, `Multiplexed: true`, `OperationScope: OperationScopeAgent`,
      `OperationNormalizer: OperationNormalizerCanonical`, `ReplayPolicy: ReplayToolResult`.
    - Its params: `action` (`list|show|install|remove`), `ref`, `name`, `commit`, `digest`, `delete_data`.
    - Its description names `install` and `remove` and no longer sends the model to `skill_manage`.
  - **The challenge digest:**
    - install: `sha256("install\x00"+ref+"\x00"+commit+"\x00"+reviewDigest)`;
    - remove: `sha256("remove\x00"+name+"\x00"+record.Commit+"\x00"+strconv.FormatBool(deleteData))`.
  - **The resume context** is `{"type":"plugin_pack_approval","approval_sha256":"<hex>"}`.
  - **The resume hook.** `newChallengeResumeHook(approve func(session, digest, question string) error,
    discard func(session, digest string), contextType, digestField string, allow func(context.Context)
    bool) runner.ResumeHook`.
    - `allow` is checked only on an accept, never on an expiry. A refused accept returns nil: the hook runs
      after `CommitResume`, so an error would fail an already-persisted resolve.
    - The shell hook passes `allow` nil.
    - The plugin hook's `allow` reads the tool handle's backend when it is called, and asks
      `MayAdminister` for the ctx principal (`withPrincipal`, set by the cockpit and by Telegram).
  - **The card keys.** `approval.plugin.install` and `approval.plugin.remove`, whose params mirror the
    question lines. `valid` and `requiredParams` know them.
  - **The gateway.** `"plugin_pack": classifyPluginPack`, with `list`/`show` `Safe`, `install`/`remove`
    `Normal`, and anything else `Risky`. If an identity's policy says ask for `plugin_pack`, the gateway's
    card comes too: that is the operator's own policy.

- [ ] **Step 1: Write the failing tests.**
  - **Tool tests** (fake backend, no gateway):
    - **`TestPluginPackInstallAsksFirst`:** a trusted result with `error: plugin_approval_required`, the
      question, `repeat` `{action, ref, commit, digest}`, the resume context, and no `Install` call.
    - **`TestPluginPackInstallRunsOnceAfterAccept`:** a second repeat gives a fresh challenge.
    - **`TestPluginPackRelayedQuestionMismatchRecordsNothing`.**
    - **`TestPluginPackRepeatWithAnotherCommitIsNotApproved`.**
    - **`TestPluginPackRefusesAMember`:** the refusal names Governance → Plugin, and there is no challenge.
    - **`TestPluginPackRemoveAsksFirstAndBindsTheCommit`:** a reinstall at another commit makes the old
      accept count for nothing.
    - **`TestPluginPackWithoutABackendSaysSo`.**
    - **`TestPluginPackRepeatFingerprintDiffersFromTheProposal`**, through `tools.OperationFingerprint`.
  - **Store and hooks:**
    - **`TestApprovalStoreZeroValueAndBinding`**, plus every existing shell approval test unchanged.
    - **`TestShellHookCannotApproveAPluginChallenge`.**
    - **`TestPluginResumeHookCountsOnlyAnAdminAndAlwaysDiscardsOnExpiry`** (cmd/aura).
  - **Card and gateway:**
    - **`TestClassifyPluginPack`**, and `gateway.ValidateClassifiable` passing with the new spec.
    - **`TestDerivePluginInstallAndRemove`:** round-trip of `Question` and `RemovalQuestion`.
    - **`TestDerivePluginInstallRejectsForgedLines`:** a question with two `source:` lines falls back to the
      verbatim question.
    - **`TestRenderItalianPluginInstall`.**
  - **Web:** `approvalQuestion` renders both keys in IT and EN.
- [ ] **Step 2: Run** `go test ./internal/agent/tools/ ./internal/gateway/ ./internal/approvaltext/ ./cmd/aura/
  -run 'Plugin|Approval|Classify|Shell|Pack'` and the approvals vitest. Expected: FAIL.
- [ ] **Step 3: Implement.** The tool's spec text must not depend on whether a backend is bound (the
  prompt cache).
- [ ] **Step 4: Run** `go vet ./...`, `go build ./...`, `go test`/`-race` on the four packages, and vitest.
  Expected: PASS.
- [ ] **Step 5: Commit** `feat(plugins): the agent proposes an install and the operator confirms it`.

### Task 11: the CLI keeps `list` and `show`

**Files:**
- Modify:
  - `cmd/aura/pack.go`: usage, and `runPack` without `install`/`trust`/`runPackWrite`/`packWriteDeps`;
  - `cmd/aura/main.go:153`, the top-level usage;
  - `internal/packs/install.go`: drop `Installer`, `Report`, `StepResult`, `WriteReport` and
    `writeSteps`, and keep what Tasks 6–7 use (`InstallableServers`, `SourceMarker`, `RefFromSource`).
    Remove `RefFromSource` too if nothing calls it.
  - `internal/packs/install_test.go`.
- Delete:
  - `cmd/aura/pack_install.go` and `pack_install_test.go`;
  - `cmd/aura/pack_install_skill_integration_test.go`, whose only check (that `packSkillInstaller` uses
    `LoadDB`) goes away with the function;
  - the install and trust cases of `pack_test.go`.

- [ ] **Step 1: Write the failing tests.**
  - **`TestPackUsageOffersOnlyListAndShow`:** neither `install` nor `trust` appears, and the usage holds
    `install from Governance → Plugin, or ask the agent`.
  - **`TestTopLevelUsageListsPackListAndShowOnly`.**
- [ ] **Step 2: Run** `go test ./cmd/aura/ -run Pack`. Expected: FAIL.
- [ ] **Step 3: Implement** the deletions.
- [ ] **Step 4: Run** `go vet ./...`, `go build ./...`, `go test`/`-race` on `./cmd/aura/` and
  `./internal/packs/`, and the `deadcode` target. Expected: PASS, with nothing new reported.
- [ ] **Step 5: Commit** `refactor(packs): the CLI no longer installs; the operator's surfaces do`.

### Task 12: the broker plugin ships Agent Plugins (`chetto1983/broker-analysis-mcp`)

**Files** (in `D:\broker-analysis-mcp`):
- Create: `broker-analysis/plugin.json`, `broker-analysis/mcp.json`.
- Modify:
  - `tests/test_plugin_manifest.py`, `pyproject.toml`;
  - both other manifests;
  - `broker-analysis/.mcp.json` (`@v0.1.1`).

- [ ] **Step 1: Write the failing test.**
  - `plugin.json` has exactly the permitted fields, the 1.0.0 `$schema`, and the Claude manifest's
    metadata.
  - `mcp.json` has exactly `$schema` (1.0.0) and `mcpServers`, with `"type": "stdio"` and the same
    server.
  - Every version agrees.
- [ ] **Step 2: Run** the repository's tests. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the tests. Expected: PASS.
- [ ] **Step 5: Commit, tag, push.**
  - Commit `Ship the Agent Plugins 1.0 manifests; release 0.1.1`.
  - Tag `v0.1.1` and push the branch and the tag, which is the release spec §6 names.
  - Check that the repository's CI is green, and that the default branch is the tagged commit (Task 13
    A reads "commit at v0.1.1").

### Task 13: gates, review, push, and the lab VM

- [ ] **Step 1: Gates in WSL.**
  - `make quality` and `bash scripts/coverage_docker.sh`, with `internal/plugins` at 85% or more and the
    `usersandbox` delegated report at 85% or more.
  - The web gates.
  - Fix everything red.
- [ ] **Step 2: Final review.** One fresh read-only reviewer (`claude -p --model opus --tools Read Grep
  Glob`) over the whole range, with this plan's Review Focus verbatim. Then one fix pass, each fix
  RED → GREEN.
- [ ] **Step 3: Push** with the WSL recipe, then check every CI job green on the pushed SHA.
- [ ] **Step 4: The lab VM,** after the updater applies the SHA. Check that `aura-image-update.service` is
  inactive before every turn.
  - **A. Install.** The operator installs `chetto1983/broker-analysis-mcp/broker-analysis` from Governance →
    Plugin.
    - The review reads `agent-plugins/1.0.0`, the v0.1.1 commit, 5 skills, and `broker-analysis (box)`.
    - The agent answers a broker question with no restart.
    - `docker top aura-box-<identity>` shows the server.
    - Its `/proc/<pid>/environ` holds `PLUGIN_ROOT=/plugins/broker-analysis`.
  - **B. Removal.** The operator removes the plugin, with the data kept.
    - The tools leave the agent's registry with no restart.
    - `/plugins/broker-analysis` leaves the box at the next boxed call.
  - **C. Chat.** In the operator's conversation, the operator asks for the plugin. The agent proposes it,
    the cockpit card shows the review in Italian, the operator accepts, and the agent answers a broker
    question.
    - The Telegram card is not exercised on the VM; it is unit-tested in Task 10. Say so in the
      verification.
  - **D. The agent's account.** Aura's agent says what the proposal and the card told it and what was
    missing. Its frictions are fixed here or recorded as open.
  - **Cleanup.** The VM ends in the state the operator chooses.
- [ ] **Step 5: Record.**
  - Write `docs/superpowers/verification/2026-10-2x-pack-install-lab-vm.md`, with what was measured and
    what it does not show.
  - Update the Status lines of this plan and the spec.
  - Update the memories `project_plan_audit_2026-10-23` and `project_pack_install_cockpit_and_agent`.

## Decisions taken while writing this plan

- **`AURA_PLUGINS_DIR`, not `AURA_CONFIG_DIR`.**
  - The finding: no Go code reads `AURA_CONFIG_DIR` (grep, 2026-10-25).
  - The variable follows `AURA_MCP_ENV_DIR`: a code default under `~/.aura`, pinned in compose. The image
    payload ships compose to the VM.
- **A new package, `internal/plugins`.** Its logic is counted in coverage, where `cmd/aura` glue is not.
  `internal/packs` stays a reader.
- **Every installed plugin's stdio server starts in `/plugins/<name>` with `PLUGIN_ROOT` and
  `PLUGIN_DATA`, whatever its format.** There is one launch path, and nothing changes for `uvx`. Placeholder
  expansion stays Agent Plugins only, which is the spec's non-goal.
- **Box markers live at `/plugins/.aura/<name>` and are distrusted.** A plugin name starts with an
  alphanumeric, so `.aura` is never one. Every marker read from the box is validated before use, and
  `rm` takes argv. `clearDest` moves to argv too (fix on touch).
- **The registry lock is process-wide, in the daemon.** It cannot stop a concurrent CLI process (`aura mcp
  add`), and that gap predates this plan.
- **`always` is stripped on every install, not only for plugins.** `WriteInstall`'s comment already
  promised it, and the cockpit's single install had the same hole. The operator turns it on afterwards
  with `SetAlways`.
- **A remote server with `headers` is skipped,** because Aura's header transport has no origin check
  (spec §6b).
- **`delete_data` reaches only the remover's own box,** and the card and dialog say "in your box".
- **The record over-states during an install and is rewritten at the end.** Removal tolerates missing
  items and finds connectors by source too.
- **Acceptance 7.** The box tests prove the directory, the environment and the data's survival with a shell
  script. `internal/mcp`'s box-session tests prove the handshake. The VM proves the two together on the
  broker.
- **No full code in this plan.** Execution writes the code once, under TDD, against these signatures and
  tests. The operator's 2026-10-12 correction ("se hai già fatto il codice perché rifarlo?") rules out
  writing it twice.
