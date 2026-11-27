# Browser sidecar implementation plan

**Status (2026-11-27, verified on code):** PARTIAL. Code landed and is on master and in v1.2.0 (7f0242a2d..6a3290e2f, fix pass 965bb3328..db5336994); acceptance is recorded only as the operator's confirmation (`verification/2026-10-15-browser-sidecar-check.md`), and the re-run of predicates 6 and 8 after the fix pass has no artifact (live run).

**Earlier status (2026-10-15):** PARTIAL.
- **Landed:** 7f0242a2d, 5c3104b81, f5e5b7b74, 150cf3f65 and 6a3290e2f.
- **Measured:** on the VM the shell no longer holds browser credentials, and the operator confirmed
  the acceptance predicates.
- **Final review:** "with fixes". Its fix pass landed the same day as 965bb3328, fd11a0004,
  1738407f9, c2a7fb63a and db5336994:
  - the legacy home is swept at boot;
  - admission counts open Chromes;
  - locks are per identity;
  - `Resolve` stops only a box it started;
  - delivered downloads leave the browser volume.

  Every docker_integration test passes, and usersandbox coverage is 910/1058 = 86.01%. The
  owned-surface db_integration gate passed at 55d60fdae: 46828/52960 = 88.4%, package policy
  green (docs/superpowers/verification/2026-10-15-browser-sidecar-fix-pass.md).
- **Open:** the push and the deployment, and the operator's re-run of acceptance predicates 6
  and 8 on the VM.

> **For agentic workers:** Use superpowers:executing-plans, with test-driven-development. The operator selected the complete spec (A) and explicitly requires execution in D:/Aura without a worktree. GPT-6.1 assists with E2E preparation and verification.

**Goal:** Keep logged-in browser credentials outside every filesystem, process and network path available to the agent's shell.

**Architecture:** Extend the existing sandbox spec with a typed box kind and derive mounts and names from it. Reuse DockerBackend, its egress sidecar, archive APIs and ExecStream. Only the trusted browser recipe and live-view adapters select the browser kind; browser downloads cross through a bounded archive transfer into the shell workspace.

**Tech Stack:** Go, moby client, existing agent-browser 0.38.2, Docker runc, WSL, Chromium/Playwright for acceptance.

**Spec:** docs/superpowers/specs/2026-10-11-browser-sidecar-design.md (complete scope approved by operator).

## Global Constraints

- No database migration. New box names: aura-browser-<identity>, egress: aura-egress-browser-<identity>.
- Browser mounts only its own workspace and scratch tmpfs; shell mounts only its own workspace and caches.
- Browser defaults: 2 GiB, 1024 pids, 2 CPUs, 180 s idle. MAX_ACTIVE=0 derives max(1, MemTotal / 2 / browser_memory_limit) from Docker, never from Aura's cgroup.
- Downloads limited to 50 MiB each; completed regular files only. No symlinks, hardlinks, traversal, overwrites or partial final files.
- Existing profiles move before the shell resumes accepting work, with source stopped while copying and verification preceding removal. Migration failures deny shell and browser use.
- Preserve unrelated local edits. Stage explicit paths only. Keep files at or below 600 LOC.
- Run Go vet/build and package tests/race after each implementation batch. Real Docker and agent E2E remain required, with a dated evidence ledger.

## Review Focus

1. A daemon restart must count existing running browser boxes and eventually reap them; in-memory-only admission is insufficient.
2. Shell routes racing profile migration or idle suspension must not resume the old browser or expose old credentials.
3. Download destination directories/suffixes can be pre-planted as symlinks; final publication must not overwrite or follow them.
4. File URLs, screenshot paths and unrecognized CLI arguments must not turn the browser MCP into a file reader or state exporter.
5. Failed migration, extraction or egress startup must leave no callable partially secured box and must remain retryable.

## Decisions taken while writing this plan

- The user's A selects the complete spec and the recommended defaults presented in chat. Upload has no MCP surface and is not added.
- Retiring the seeded local identity deletes its browser credentials rather than transferring an authenticated session to a different principal. Reuse the existing retirement/deprovision seam after inspecting it.
- All model-supplied extraArgs are refused. Aura alone injects --profile. Safe typed per-tool fields are retained; restore, CA, namespace, launch and filesystem options are refused.
- The installed 0.38.2 schema was inventoried on the VM by tools/list without opening browser pages. Screenshot path and screenshotDir both exist; both are refused.
- URL arguments accept public HTTP(S) and exact about:blank. Local files, loopback and private literal addresses are refused. Redirects and site scripts remain governed by egress; page-visible information is explicitly outside the credential-file invariant.
- A per-identity lifecycle lock serializes both kinds, migration, idle sweep and destruction. Browser admission is serialized at the backend and reads Docker's running-container inventory before starting a new identity.
- Profile migration uses the Docker archive API while the shell is stopped. A short-lived migration container mounts the shell volume alone; it never mounts the browser volume. File hashes are checked in the destination before deleting the precise old profile directory. Sockets and Chromium lock symlinks are omitted; other links are rejected.
- Browser key files are written only for Browser kind. Shell containers built before the split are recreated with their volume preserved after migration, which also removes their old key and browser processes.

## File structure

| Files | Responsibility |
|---|---|
| usersandbox/spec.go, translate.go, backend.go | BoxKind and derived workspace mounts; kind on handles |
| usersandbox/docker_backend*.go | Per-kind lifecycle, admission, reconciliation, key placement |
| usersandbox/browser_migration.go (+ tests) | Stopped-source verified credential migration |
| usersandbox/router_browser.go, router.go, reap.go | Browser route, tracking, independent TTL, purge |
| config/config_sandbox.go, config_knobs.go | Browser cap, TTL and admission settings |
| cmd/aura/sandbox_mcp_box.go, serve_browser_live.go, serve_chatgpt_browser.go | Browser-only routing and touches |
| mcptools/bridge_browser_arguments.go (+ tests) | Argument and URL allowlist |
| mcptools/bridge_browser_downloads.go, usersandbox/browser_downloads.go (+ tests) | Completed download crossing and result footer |
| cmd/aura/mcp_tools.go, runtime_handles.go | Download bridge composition |
| docker/aura-sandbox/agent-browser.sh, embedded browser skill | Known download root and model instructions |

### Task 1: Box kinds, config and lifecycle

**Interfaces:** `type BoxKind int; const (Shell BoxKind = iota; Browser)`, `BoxHandle.Kind BoxKind`, `SandboxSpec.Kind BoxKind`. Workspace is derived from identity and kind, not chosen independently. `RouteBrowser(context.Context) (BoxHandle,error)` and `TouchBrowser(string)` parallel the shell route. `WithBrowserLimits(Resources, int)` configures backend caps/admission.

- [ ] Write unit tests proving Browser mounts exactly aura-browser-<id> and scratch, never shell caches, even with an adversarial legacy WorkspaceVol. Assert shell remains on aura-box-<id>.

```go
func TestBrowserMountsNeverExposeShellWorkspace(t *testing.T) {
    hc := toHostConfig(SandboxSpec{IdentityID:"a", Kind:Browser})
    if len(hc.Mounts) != 2 || hc.Mounts[0].Source != "aura-browser-a" { t.Fatal(hc.Mounts) }
}
```

- [ ] Run `go test ./internal/sandbox/usersandbox -run 'Browser|Translate'`; expect failure before BoxKind exists.
- [ ] Derive names and mounts by kind, reuse Resolve/Suspend/Resume/Stop with kind-specific egress and resources, and write key files only for Browser. Browser skips source materialization and cache volume creation.

```go
func workspaceName(id string, kind BoxKind) string {
    if kind == Browser { return "aura-browser-" + id }; return "aura-box-" + id
}
```

- [ ] Add config defaults/parsing/validation tests for all five browser env vars. Wire admission from Docker MemTotal, count existing running boxes, reject another identity without eviction. Test simultaneous first calls and admission recovery after stop/restart.
- [ ] Add lazy RouteBrowser, independent tracking/TTL, destruction of both kinds. Tests cover nil backend, errors, independent touches, racing reap, retry and cold-process destruction.
- [ ] Run package tests/race, vet/build. Commit explicit task files with why.

### Task 2: Verified migration and credential removal

**Interfaces:** internal `migrateBrowserProfile(ctx, identityID, browserHandle) error`, invoked under the same lifecycle lock before any shell work is returned. DockerBackend optional `PrepareBrowserIsolation(ctx,id) error` is used by the router before shell Resolve.

- [ ] Add archive fixtures with normal files, stale sockets, Chrome lock symlinks, traversal, hardlinks and unsupported links. Test copy/hash verification failure retains source, and successful retry deletes source only after verification.

```go
// Independent expected hash for a fixture body; never computed by the migration helper.
// A malicious header ../Cookies must be rejected before the destination receives it.
hdr := &tar.Header{Name:"../Cookies", Typeflag:tar.TypeReg, Size:0}
```

- [ ] Run focused migration tests and watch failure. Implement bounded spooling and hash manifest using existing CopyFrom/CopyToContainer, no secret output. Stop the old shell first, remove/recreate its container after verified migration, and retain only its ordinary workspace/cache volumes.
- [ ] Add real Docker integration: seed a synthetic login/profile in old shell, route shell first and browser first in separate runs, assert no old files/key/pids remain in shell and browser restores login after suspend.
- [ ] Run tests/race, vet/build. Commit explicit task files.

### Task 3: MCP/live-view routing and browser arguments

**Interfaces:** Browser routing selection is server-side for the exact trusted browser recipe command; an arbitrary configured stdio server remains in Shell. `validateBrowserArguments(tool string,args map[string]any) error` precedes profile injection.

- [ ] Add route tests proving browser MCP, regular live view and ChatGPT login all use Browser, while non-browser MCP uses Shell. Browser touches never touch shell TTL.
- [ ] Add bridge tests for every per-tool allowlist field, forbidden first-class restore/CA/namespace/idle args, unknown fields and extraArgs, screenshot path/dir, private/file URLs and safe public URLs. Change the old --headed forwarding test because model-controlled launch flags are deliberately removed.

```go
for _, raw := range []string{
 `{"url":"file:///run/aura/agent-browser.key"}`,
 `{"url":"http://127.0.0.1:9222/json"}`,
 `{"restore":true}`, `{"screenshotDir":"/workspace/downloads"}`,
} { /* execute the mounted recipe tool and require an error before server call */ }
```

- [ ] Watch tests fail; route through RouteBrowser, enforce allowlists and grade agent_browser_eval as MCPActionDestructive. Verify approval metadata through the existing risk tests.
- [ ] Run tests/race, vet/build. Commit explicit files.

### Task 4: Download crossing and skill

**Interfaces:** consumer seam `BrowserDownloads.Collect(ctx context.Context) (string,error)` on MountOptions and MountedServer. Production adapter uses `SandboxRouter.CollectBrowserDownloads(ctx) (string,error)`. Errors append a bounded warning to the browser result; completed files are announced in a reserved footer.

- [ ] Write tests for .crdownload in progress, complete transfer once by (name,size,mtime), restart deduplication, 50 MiB boundary, duplicate names, truncated source, source/destination links and concurrent completion. Destination uses an exclusive staging file plus no-replace final publication, so failed copies never appear complete.
- [ ] Watch focused tests fail. List only /workspace/downloads through a fixed in-box command, read regular files with no-follow validation through archive APIs, copy to shell with WriteFileStream and publish. Persist deduplication metadata in the browser volume, inaccessible to shell. Use base names and numeric suffixes; reject archive links and unexpected extra entries.

```go
type BrowserDownloads interface { Collect(context.Context) (string, error) }
// Example model footer:
// Download ready: /workspace/downloads/report.pdf (8388608 bytes).
// Download in progress: report.pdf.crdownload (1048576 bytes).
```

- [ ] Set AGENT_BROWSER_DOWNLOAD_PATH=/workspace/downloads in the browser entry point. Remove shell fallback from browser-aura, describe completion via the next browser call and eval approval.
- [ ] Run tests/race, image contract and vet/build; commit explicit files.

### Task 5: Acceptance, review and shipping

- [ ] GPT-6.1 E2E helper writes/runs disposable-identity fixtures with real agent turns on the lab VM, using frontend credentials without logging values. Record deployment IDs before/after.
- [ ] Run the spec's 11 acceptance predicates against actual routing, with a real-site live view, slow 8 MiB download, two identities and MAX_ACTIVE=1 refusal. Record RAM/pids, stop/start cookie preservation, purge and migration evidence.
- [ ] Run owned-surface coverage and delegated docker_integration coverage (85% floors), mutation spot check (70%), regression suite and final independent review. Fix critical/important findings with reproducing tests.
- [ ] Update spec/PRD with measured behavior and limits, dated verification ledger, plan checkboxes. Preserve unrelated dirty paths while committing.
- [ ] Execute required post-commit verification and push gates/CI once the full job is validated. Do not claim completion with any required gate missing.
