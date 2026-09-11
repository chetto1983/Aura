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
- The package is built from a clean clone of `master` (`npm pack`), not from the working tree.
- The Aura image is `ghcr.io/chetto1983/aura:edge` published from a commit that contains plans
  A to C.

---

### Task 1: Back up what the wipe destroys

- [ ] **Step 1:** stop the writers (`aura`, `aura-ingest`, `arcadedb-mcp`), trigger a native
  ArcadeDB backup of every database (`list databases`, then `backup database <name>` through
  `/api/v1/server` with the root password read by `read_secret ARCADEDB_PASSWORD`), and copy
  `/home/arcadedb/backups` out with `docker cp`.
- [ ] **Step 2:** stop `arcadedb` and tar the `aura_aura-arcadedb` volume (databases plus
  server users) into the backup directory; `pg_dump -Fc` the `aura` database next to it as a
  safety net.
- [ ] **Step 3:** move the checkout's `.env`, `C:\Users\chett\.aura` and WSL's `/root/.aura`
  into the backup directory; list the directory with sizes as evidence.

### Task 2: Wipe

- [ ] **Step 1:** `docker compose down --remove-orphans` from the checkout.
- [ ] **Step 2:** remove every `aura_*` volume but the kept ones, and every `aura-box-*`
  sandbox volume (their identities are gone with Postgres); list what remains.

### Task 3: Install through the package

- [ ] **Step 1:** wait for `Publish Aura edge image` green on the pushed commit.
- [ ] **Step 2:** in WSL, `npm install -g` the packed `create-aura-appliance-0.2.0.tgz` and run
  `create-aura-appliance --mode local` under `expect`: install dir `/opt/aura`, appliance no,
  gVisor no, confirm yes. The run must end on the wizard URL; the output stays in WSL.
- [ ] **Step 3:** `docker compose -f /opt/aura/compose.yaml ps` all healthy; the kept caches
  are mounted (no model download beyond the HEAD probe).

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
