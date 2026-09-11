# Management key, plan D: clean reinstall on this PC and the E2E

> **For agentic workers:** execute inline, one task at a time, and tick each box in the same
> commit as the task's evidence.

**Goal:** wipe this PC's Aura state (after a file backup of the ArcadeDB memory), install
through the new package, and prove the management-key design end to end on the live stack.

**Architecture:** the old stack runs from the repository checkout (compose project `aura`).
The new one is installed by `create-aura-appliance` in local mode inside WSL (Ubuntu 26.04,
systemd, Docker Desktop's engine) at `/opt/aura`, so its compose project is `aura` again and it
finds the kept model and package caches. The E2E is a Playwright spec against the installed
stack, driven through the UI.

**Spec:** `docs/superpowers/specs/2026-09-10-management-key-onboarding-design.md`
(§Testing, E2E; §Reinstall on this PC).

## Global Constraints

- Keep: `aura_aura-llama-embed`, `aura_aura-llm`, `aura_aura-ocr-vl`,
  `aura_aura-npm-cache-host`, `aura_aura-pip-cache-host`, `aura_aura-uv-cache-host`, and the
  sandbox package caches `aura-npm-cache`, `aura-pip-cache`, `aura-uv-cache`.
- Backups live in `D:\Backups\aura-2026-09-11\`, outside every volume.
- The management key is asked of the operator in one line; it is never searched for, printed
  or written to a file in the repository.
- The package comes from npm: tag `installer-v0.2.0`, published by CI. The operator runs it and
  answers its questions; nothing is built locally or answered by a script.
- The Aura image is `ghcr.io/chetto1983/aura:edge` published from a commit that contains plans
  A to C.

---

### Task 1: Back up what the wipe destroys

- [x] **Step 1:** stop the writers (`aura`, `aura-ingest`, `arcadedb-mcp`), trigger a native
  ArcadeDB backup of every database (`list databases`, then `trigger backup <name>` through
  `/api/v1/server`, as `scripts/restore_drill.sh` does, with the root password read by
  `read_secret ARCADEDB_PASSWORD`), and copy `/home/arcadedb/backups` out with
  `docker compose cp` (Windows path: under `MSYS_NO_PATHCONV` a `/d/...` path reaches docker
  as `D:\d\...`).
- [x] **Step 2:** stop `arcadedb` and tar the `aura_aura-arcadedb` volume (databases plus
  server users) into the backup directory; `pg_dump -Fc` the `aura` database next to it as a
  safety net.
- [x] **Step 3:** move the checkout's `.env`, `C:\Users\chett\.aura` and WSL's `/root/.aura`
  into the backup directory; list the directory with sizes as evidence.
  Measured: native backups 329M (`aura_memory` among them, fresh at 09:39 UTC),
  `arcadedb-volume.tgz` 12M, `postgres-aura.dump` 1.1M, `dotenv` 28K, `windows-dot-aura` 42M,
  `wsl-root-dot-aura` 216K.

### Task 2: Wipe

- [x] **Step 1:** `docker compose down --remove-orphans` from the checkout. With `.env` moved
  away compose refuses its `:?` variables, so `--env-file` names the backed-up copy.
- [x] **Step 2:** remove every `aura_*` volume but the kept ones, and every `aura-box-*`
  sandbox volume (their identities are gone with Postgres); list what remains.
  `down` left four containers outside the project's services (two `aura-egress-*` sandbox
  proxies, `aura-tempo-1`, `aura-docker-socket-proxy`) holding `aura_aura-tempo` and
  `aura_default`; removed by name. Left: the six kept `aura_aura-*` caches and the three
  sandbox package caches; no Aura container or network.

### Task 3: Install through the package

- [x] **Step 1:** wait for `Publish Aura edge image` green on the pushed commit (06a64e054).
- [x] **Step 2:** push tag `installer-v0.2.0`; `Verify and publish create-aura-appliance`
  publishes 0.2.0 to npm.
- [ ] **Step 3:** in WSL the operator runs `npx create-aura-appliance@0.2.0 --mode local` and
  answers its questions (install dir `/opt/aura`, appliance no, gVisor no). The run ends on the
  wizard URL.
- [ ] **Step 4:** `docker compose -f /opt/aura/compose.yaml ps` all healthy.

A first install from a locally packed tgz, answered by `expect`, reached a healthy stack
(install exit 0, 16 containers, `aura:edge` at `06a64e054`) and was torn down: it was not the
path an operator takes. WSL keeps Node 22.23.2 (official tarball, SHA-256 checked); `wsl.exe`
without `-e` hands the command to the default shell, which expands `$var` before the inner
bash sees it.

### Task 4: The E2E (Definition of Done)

`web/e2e/management-key-onboarding-live.spec.ts`, gated by
`AURA_E2E_LIVE_MANAGEMENT_KEY=1`, the management key passed in the environment only.

- [ ] **Step 1:** the first operator through `/setup/?token=`, then the first-run setup with
  route OpenRouter, the management key and a services cap. Aura restarts once.
- [ ] **Step 2:** `GET https://openrouter.ai/api/v1/keys` lists the admin's key (name = identity
  id, `limit` null) and `aura-services` with the chosen cap.
- [ ] **Step 3:** one admin chat turn; OpenRouter attributes it to the admin's key and the
  spend page lists the admin.
- [ ] **Step 4:** the Credit panel shows "no limit".
- [ ] **Step 5:** an admin provisions a second identity: minted at zero, its turn refused until
  topped up, 403 on the route settings.
- [ ] **Step 6:** commit the spec; web gates green.

### Task 5: Give Claude and Codex their memory back

- [ ] **Step 1:** read ArcadeDB's backup and restore documentation, then restore the old
  `aura-memory` database from the Task 1 backup into the new operator's database; one
  `aura-memory` query returns a known fact.

### Task 6: Push

- [ ] **Step 1:** push, watch CI green; record the measurements in this plan.
