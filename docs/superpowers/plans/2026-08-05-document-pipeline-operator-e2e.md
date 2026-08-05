# Document Pipeline Operator-Driven E2E — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Tasks 5–10 are operator-paired and MUST run in the main session.** They require a human driving the Cockpit in real time. Do not dispatch them to a subagent — a subagent cannot click, and cannot see what the screen showed.

**Goal:** Prove the document pipeline end to end on the production path — operator on the Cockpit, Claude on the backend — then close Amendment #115/#116 with the hermetic two-tenant script.

**Architecture:** Two serial phases against the local Windows Docker stack. Phase 1 is interactive and covers the four gaps the headless script structurally cannot reach; Phase 2 is the unattended `document_pipeline_e2e.sh` gate, which restarts the service mid-run. Everything is gated behind a rehearsed migration and a repaired observability lens.

**Tech Stack:** Go 1.26 (WSL only), Postgres 18, Docker Compose, Garage (S3), Docling, Tempo/Prometheus/Grafana, React Cockpit at `127.0.0.1:9080`.

**Spec:** `docs/superpowers/specs/2026-08-05-document-pipeline-operator-e2e-design.md`

## Global Constraints

- **Never run a `.exe` on the Windows host.** Go toolchain and `sqlc` are WSL only:
  `wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && <cmd>'`
- **No `db_integration` / `docker_integration` / any tagged tier runs at any point in this plan.** Those helpers migrate whatever `AURA_DB_URL` names, which locally is the live `aura`.
- **Every direct SQL read of an identity-scoped table goes through the identity GUC, as `aura_app`.** The GUC is **`app.current_identity`** (`internal/db/tx.go:120`), not `aura.identity_id`. The role matters as much as the GUC: `aura` is `rolsuper=t, rolbypassrls=t`, so it reads every row whether or not the GUC is set — a probe run as `aura` cannot fail its own negative control and proves nothing about scoping. Measured as `aura_app`: **0 rows without the GUC, 4 with it.**
- **Never accept a container healthcheck as proof of reachability** for `tempo` or `prometheus`. Measured 2026-08-05: both report `running`/`healthy` while unreachable.
- `POSTGRES_PASSWORD` is sourced from `.env` and never echoed into logs or command lines that get printed.
- Operator identity: `dc98a3ee-e38e-4288-8d64-27ce4c9cde65` (`dvdmarchetto@gmail.com`, kind `user`).
- Docker network for one-off containers: `aura_default`. DB roles: `aura`, `aura_app`, `aura_migrate`.
- Corpus (read-only, exactly seven files): `D:\tmp\aura-document-pipeline-references\document_ingestion\baseline-corpus` → WSL `/mnt/d/tmp/...`.
- Evidence directory for this run: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/`.
- Commit style: imperative subject, body explains *why*, `Co-Authored-By` trailer. Never `--no-verify`.

## File Structure

| Path | Responsibility |
|---|---|
| `compose.yaml` (modify, tempo + prometheus healthcheck blocks) | Reachability-based liveness for the two netns sidecars |
| `scripts/observability_sidecar_check.sh` (create) | Single source of truth for "are tempo and prometheus actually reachable" — used by the healthcheck fix's test, Phase 0 step 6, and the Phase 2 restart hook |
| `scripts/document_pipeline_restart_hook.sh` (create) | `AURA_DOCUMENT_E2E_RESTART_HOOK` — restarts `aura`, then recreates the sidecars, then waits for reachability |
| `docs/superpowers/verification/2026-08-05-.../PHASE0.md` (create) | Rollback location, rehearsal result, post-migration assertions, trace proof |
| `docs/superpowers/verification/2026-08-05-.../FINDINGS.md` (create) | The CP1–CP7 ledger: what the operator saw, what the backend held, verdict per checkpoint |
| `docs/superpowers/verification/2026-08-05-.../PHASE2.md` (create) | Script report path, `EXPECT_*` values used, pass/fail per named check |

---

### Task 1: Capture the rollback and rehearse 0093 on a copy

Migration 0093 has been applied **nowhere**. It adds `source_kind` and `source_key` as nullable, backfills them (`'legacy'`, `'document:'||id`), dedups colliding `(identity_id, source_kind, source_key)` groups, then tightens to `NOT NULL` plus non-empty CHECKs — and it creates `aura.document_pipeline_stages`, without which the E2E probe cannot run at all. Whether that sequence survives the live rows is empirical. The copy is where we find out.

**Files:**
- Create: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE0.md`

**Interfaces:**
- Produces: a verified `pg_dump` at `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump`, and a green/red verdict on 0093 against real data. Task 3 consumes both.

- [ ] **Step 1: Take the rollback dump**

```bash
mkdir -p /d/tmp/aura-backups
docker exec aura-postgres pg_dump -U aura -d aura -Fc -f /tmp/aura-pre0093.dump
docker cp aura-postgres:/tmp/aura-pre0093.dump /d/tmp/aura-backups/aura-pre0093-2026-08-05.dump
ls -la /d/tmp/aura-backups/aura-pre0093-2026-08-05.dump
```

Expected: a file of non-trivial size (tens of KB at minimum). A zero-byte dump aborts the task.

- [ ] **Step 2: Prove the dump actually restores**

A dump that has never been restored is not a rollback. Restore it into a throwaway database — this doubles as the rehearsal target.

```bash
docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE IF EXISTS aura_0093_rehearsal;"
docker exec aura-postgres psql -U aura -d postgres -c "CREATE DATABASE aura_0093_rehearsal OWNER aura;"
docker exec aura-postgres pg_restore -U aura -d aura_0093_rehearsal --no-owner --role=aura /tmp/aura-pre0093.dump
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT status, count(*) FROM aura.documents GROUP BY status ORDER BY 1;"
```

Expected: `92 | f`, and `deleted | 1`, `ready | 3` — matching live exactly. A mismatch means the dump is not faithful; stop.

- [ ] **Step 3: Build the image that carries 0093**

```bash
docker compose build aura
docker images --format "{{.Repository}}:{{.Tag}} {{.ID}} {{.CreatedAt}}" | grep "^aura:local"
```

Expected: a fresh `CreatedAt` (today). The prior image was built 2026-08-03 22:44 and contains zero occurrences of `0093_document_pipeline_convergence`.

- [ ] **Step 4: Confirm the new binary actually carries the migration**

Do not trust the build log; check the artifact. This is the exact check that proved the old image stale.

```bash
docker run --rm --entrypoint sh aura:local -lc \
  'strings /usr/local/bin/aura | grep -c "0093_document_pipeline_convergence"'
```

Expected: a number `>= 1`. A `0` means the migration is not embedded and every later step is meaningless.

- [ ] **Step 5: Run the migration against the copy**

```bash
set -a; . /d/Aura/.env; set +a
docker run --rm --network aura_default \
  -e AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_BOOTSTRAP_URL="postgres://aura:${POSTGRES_PASSWORD}@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_CONFIG_DIR=/tmp \
  --entrypoint sh aura:local -lc 'aura db migrate'
```

Expected: exit 0. A failure here is the whole point of the task — capture the exact error, stop, and report. Live is untouched and no recovery is needed.

- [ ] **Step 6: Assert the rehearsed end state**

```bash
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT count(*) AS docs, count(*) FILTER (WHERE source_kind IS NULL) AS null_kind, count(*) FILTER (WHERE source_key IS NULL) AS null_key FROM aura.documents;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT to_regclass('aura.document_pipeline_stages') AS stages, to_regclass('aura.document_pipeline_quarantine') AS quarantine;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT indexname FROM pg_indexes WHERE schemaname='aura' AND indexname='documents_identity_source_live_idx';"
```

Expected: `93 | f`; `docs=4, null_kind=0, null_key=0`; both `to_regclass` non-null; the partial index present.

- [ ] **Step 7: Drop the rehearsal database**

```bash
docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE aura_0093_rehearsal;"
docker exec aura-postgres rm -f /tmp/aura-pre0093.dump
```

- [ ] **Step 8: Record and commit**

Write `PHASE0.md` with: dump path and size, the rehearsal's before/after `schema_migrations`, the four assertion outputs from step 6, and the `strings` count from step 4.

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE0.md
git commit -m "Rehearse migration 0093 against a copy of live data"
```

---

### Task 1b: Fix 0093's pending-trigger-event failure and re-rehearse

Task 1's rehearsal did its job and **0093 failed against real data**:

```
pq: cannot ALTER TABLE "documents" because it has pending trigger events (55006)
```

**Root cause, measured — and it is not what the first diagnosis said.** There *is* a
pre-existing `DEFERRABLE INITIALLY DEFERRED` foreign key on `aura.documents`, live at version
92:

```
documents_active_version_id_fkey  documents → document_versions  condeferrable=t  condeferred=t
```

Every one of 0093's `UPDATE aura.documents` statements (lines 102–288) queues a deferred RI
check event for it. Deferred events stay pending until COMMIT, and
`ALTER TABLE aura.documents … SET NOT NULL` at line 290 then refuses.

The FKs 0093 adds at lines 384–397 are **not** the cause — they are added after the failure
point and never execute.

Two facts make the fix a single line:

- **No DML follows line 290.** Everything from there to the end of the migration is DDL, so
  one flush placed immediately before it covers the entire remainder.
- **0093 never writes `active_version_id`** — lines 155, 168 and 173 only read it. The flush
  therefore validates data that was already valid at COMMIT under version 92.

Amending 0093 in place remains legitimate: live is at `92`, and the migration is applied
nowhere. That licence ends the moment Task 3 runs.

**Files:**
- Modify: `internal/db/migrations/0093_document_pipeline_convergence.up.sql` (insert one statement before the `ALTER TABLE aura.documents` at line 290)
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE0.md`

**Interfaces:**
- Consumes: the dump at `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump` (already proven restorable by Task 1).
- Produces: a 0093 that reaches `93 / clean` on a faithful copy of live data. Task 3 depends entirely on this.

- [ ] **Step 1: Insert the flush**

Immediately before the `ALTER TABLE aura.documents` block that begins with
`ALTER COLUMN source_kind SET NOT NULL` (line 290), and after the `UPDATE aura.documents`
that remaps `draft`/`processing`/`archived`, insert:

```sql
-- aura.documents carries a DEFERRABLE INITIALLY DEFERRED FK
-- (documents_active_version_id_fkey), so every UPDATE above queues an RI check event that
-- stays pending until COMMIT, and ALTER TABLE refuses to run while any are outstanding
-- (SQLSTATE 55006). Flush them here. No DML follows this point, so one flush covers the
-- rest of the migration.
SET CONSTRAINTS ALL IMMEDIATE;
```

Change nothing else in the file.

- [ ] **Step 2: Restore a faithful copy**

Ownership must be preserved — a `--no-owner --role=aura` restore strips `aura_migrate`'s
ownership of `schema_migrations` and the migration fails a permission check before running
any DDL. That is a rehearsal-harness defect, not a migration defect, and it already cost one
attempt.

```bash
docker cp /d/tmp/aura-backups/aura-pre0093-2026-08-05.dump aura-postgres:/tmp/aura-pre0093.dump
docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE IF EXISTS aura_0093_rehearsal;"
docker exec aura-postgres psql -U aura -d postgres -c "CREATE DATABASE aura_0093_rehearsal OWNER aura;"
docker exec aura-postgres pg_restore -U aura -d aura_0093_rehearsal /tmp/aura-pre0093.dump
```

- [ ] **Step 3: Verify the copy is faithful before trusting it**

```bash
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT status, count(*) FROM aura.documents GROUP BY 1 ORDER BY 1;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT conname, condeferrable, condeferred FROM pg_constraint WHERE conname='documents_active_version_id_fkey';"
```

Expected: `92 | f`; `deleted | 1` and `ready | 3`; the FK present and `t | t`. If the FK is
absent or not deferred, the copy does not reproduce the failure and the rehearsal proves
nothing.

- [ ] **Step 4: Rebuild the image so it carries the amended migration**

The migration is embedded in the binary. An unbuilt image runs the old SQL.

```bash
docker compose build aura
docker run --rm --entrypoint sh aura:local -lc \
  'grep -ac "SET CONSTRAINTS ALL IMMEDIATE" /usr/local/bin/aura'
```

Expected: `>= 1`. A `0` means the amended file is not in the binary and every later step is
meaningless.

- [ ] **Step 5: Migrate the copy**

```bash
set -a; . /d/Aura/.env; set +a
docker run --rm --network aura_default \
  -e AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_DB_BOOTSTRAP_URL="postgres://aura:${POSTGRES_PASSWORD}@postgres:5432/aura_0093_rehearsal?sslmode=disable" \
  -e AURA_CONFIG_DIR=/tmp \
  --entrypoint sh aura:local -lc 'aura db migrate'
```

Expected: exit 0.

If it fails on a *different* error, that is a new finding — capture it verbatim and report.
Do not stack a second speculative fix on top of this one; one diagnosed cause per round.

- [ ] **Step 6: Assert the rehearsed end state**

```bash
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c "SELECT version, dirty FROM public.schema_migrations;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT count(*) AS docs, count(*) FILTER (WHERE source_kind IS NULL) AS null_kind, count(*) FILTER (WHERE source_key IS NULL) AS null_key FROM aura.documents;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT to_regclass('aura.document_pipeline_stages') AS stages, to_regclass('aura.document_pipeline_quarantine') AS quarantine;"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT indexname FROM pg_indexes WHERE schemaname='aura' AND indexname='documents_identity_source_live_idx';"
docker exec aura-postgres psql -U aura -d aura_0093_rehearsal -c \
  "SELECT conname, condeferrable, condeferred FROM pg_constraint WHERE conname='documents_active_version_id_fkey';"
```

Expected: `93 | f`; `docs=4, null_kind=0, null_key=0`; both `to_regclass` non-null; the
partial index present; and the deferrable FK still `t | t` — the flush must not have left it
altered.

- [ ] **Step 7: Confirm live is still untouched**

```bash
docker exec aura-postgres psql -U aura -d aura -c "SELECT version, dirty FROM public.schema_migrations;"
```

Expected: `92 | f`. This task never migrates live.

- [ ] **Step 8: Drop the rehearsal database and commit**

```bash
docker exec aura-postgres psql -U aura -d postgres -c "DROP DATABASE aura_0093_rehearsal;"
docker exec aura-postgres rm -f /tmp/aura-pre0093.dump
git add internal/db/migrations/0093_document_pipeline_convergence.up.sql docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE0.md
git commit -m "Flush deferred FK events before 0093 tightens documents"
```

---

### Task 2: Replace the observability healthchecks that lie

Measured 2026-08-05: after `docker restart aura`, both `tempo` and `prometheus` report `running`/`healthy` while unreachable (`wget http://aura:3200` → connection refused). Tempo's healthcheck validates a *config file* and never touches the network; prometheus's probes loopback from **inside its own orphaned namespace**. Both pass while dead to everything else. That is a falsely-green signal, and in Phase 2 it would blind the backend lens at exactly the lease-reclaim assertion.

Liveness for a netns sidecar must be asserted **from outside the namespace**.

**Files:**
- Create: `scripts/observability_sidecar_check.sh`
- Modify: `compose.yaml` (the `tempo` and `prometheus` `healthcheck` blocks)
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE0.md`

**Interfaces:**
- Produces: `scripts/observability_sidecar_check.sh` — exits 0 only when **both** `aura:3200/ready` and `aura:9090/-/ready` answer from outside the aura namespace. Task 3 step 5 and Task 11 both call it.

- [ ] **Step 1: Write the failing check**

Create `scripts/observability_sidecar_check.sh`:

```bash
#!/usr/bin/env bash
# Liveness for the two sidecars that share aura's network namespace.
#
# Their own healthchecks cannot detect the failure mode that matters: after the
# aura container is restarted or recreated, both processes stay alive attached to
# a namespace nobody can reach, and keep reporting healthy. Measured 2026-08-05.
# Reachability must therefore be asserted from OUTSIDE the namespace.
set -Eeuo pipefail
probe() {
  docker exec aura-grafana-1 wget -q -T 5 -O /dev/null "$1"
}
fail=0
probe "http://aura:3200/ready"    || { echo "tempo unreachable at aura:3200/ready"; fail=1; }
probe "http://aura:9090/-/ready"  || { echo "prometheus unreachable at aura:9090/-/ready"; fail=1; }
[[ "$fail" -eq 0 ]] && echo "observability sidecars reachable"
exit "$fail"
```

- [ ] **Step 2: Reproduce the false green — the test**

```bash
chmod +x /d/Aura/scripts/observability_sidecar_check.sh
bash /d/Aura/scripts/observability_sidecar_check.sh          # baseline: expect "reachable", exit 0
docker restart aura >/dev/null
docker inspect aura-tempo-1 aura-prometheus-1 --format '{{.Name}} {{.State.Status}} {{.State.Health.Status}}'
bash /d/Aura/scripts/observability_sidecar_check.sh; echo "check exit=$?"
```

Expected, and this **is** the test: Docker prints `running healthy` for both, while the script prints `tempo unreachable` / `prometheus unreachable` and exits 1. Docker's verdict and reality disagree — that disagreement is the defect being fixed.

- [ ] **Step 3: Make grafana's healthcheck assert the sidecars are reachable**

Neither sidecar can detect its own orphaning. A loopback probe runs *inside* the dead
namespace and passes; and `tempo`'s image is **distroless — no shell, no wget** (measured:
`exec: "sh": executable file not found in $PATH`), which is precisely why its healthcheck
validates a config file. A `CMD-SHELL` test on tempo would fail permanently and block
grafana's `depends_on: tempo → service_healthy`.

Grafana is the one container that can see across the boundary: it has a shell, it sits
outside aura's namespace, and it already depends on both. Give it the assertion.

In `compose.yaml`, replace the `grafana` healthcheck test:

```yaml
    healthcheck:
      test: ["CMD", "/usr/bin/wget", "--spider", "-q", "http://127.0.0.1:3000/api/health"]
```

with:

```yaml
    healthcheck:
      # Also asserts the two sidecars that share aura's network namespace. Neither can
      # detect its own orphaning: after aura is recreated their processes stay alive
      # attached to a dead namespace, and a loopback probe from inside it still passes
      # (measured 2026-08-05). tempo is distroless and cannot run a shell test at all.
      # Grafana is outside that namespace and already depends on both, so it is where
      # the reachability assertion belongs — and a dashboard without its datasources is
      # genuinely degraded, not merely reporting on someone else's problem.
      test: ["CMD-SHELL", "wget -q -T 3 -O /dev/null http://127.0.0.1:3000/api/health && wget -q -T 3 -O /dev/null http://aura:3200/ready && wget -q -T 3 -O /dev/null http://aura:9090/-/ready"]
```

Leave the `tempo` and `prometheus` healthchecks **unchanged**. Add a one-line comment above
each pointing at grafana's as the reachability authority, so the next reader does not
"improve" them into a false green.

The external `scripts/observability_sidecar_check.sh` stays, and remains what Phase 0 step 6
and the restart hook call — a script gives an actionable message and does not depend on
grafana's own state. The two agree by construction; they probe the same endpoints.

- [ ] **Step 4: Restore the sidecars and verify both authorities agree**

```bash
docker compose --profile observability up -d --no-deps --force-recreate tempo prometheus
docker compose --profile observability up -d --no-deps --force-recreate grafana
bash /d/Aura/scripts/observability_sidecar_check.sh; echo "check exit=$?"
docker inspect aura-grafana-1 --format 'grafana health={{.State.Health.Status}}'
```

Expected: `observability sidecars reachable`, exit 0, and grafana `healthy`.

- [ ] **Step 5: Prove the new healthcheck actually catches the orphaning**

This is the test that the fix works, and it is the same reproduction as step 2 — but now
Docker's own verdict must change too.

```bash
docker restart aura >/dev/null
# grafana's healthcheck interval is 15s; allow two cycles before judging
for _ in $(seq 1 12); do
  status="$(docker inspect aura-grafana-1 --format '{{.State.Health.Status}}')"
  [[ "$status" == "unhealthy" ]] && break
  sleep 5
done
docker inspect aura-grafana-1 --format 'grafana health={{.State.Health.Status}}'
bash /d/Aura/scripts/observability_sidecar_check.sh; echo "check exit=$?"
```

Expected: grafana `unhealthy` **and** the script exits 1. Before this change Docker reported
everything healthy while the sidecars were unreachable; now the two agree.

- [ ] **Step 6: Restore working state**

```bash
docker compose --profile observability up -d --no-deps --force-recreate tempo prometheus
docker compose --profile observability up -d --no-deps --force-recreate grafana
bash /d/Aura/scripts/observability_sidecar_check.sh; echo "check exit=$?"
```

Expected: exit 0.

- [ ] **Step 7: Commit**

```bash
git add scripts/observability_sidecar_check.sh compose.yaml
git commit -m "Assert observability sidecar liveness from outside aura's namespace"
```

---

### Task 3: Cut over — apply 0093 to live in the proven order

The order below is measured, not chosen. The sidecars must be recreated **after** `aura`, or they attach to a namespace that is about to be replaced and die silently.

**Files:**
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE0.md`

**Interfaces:**
- Produces: live `aura` at `93 / clean`, `aura.document_pipeline_stages` present, traces provably flowing. Every later task depends on all three.

- [ ] **Step 1: Recreate aura on the new image**

```bash
docker compose up -d --force-recreate aura
docker inspect aura --format 'health={{.State.Health.Status}}'
docker logs aura-migrate --tail 20
```

Expected: `health=healthy`; the migrate log shows 0093 applying.

- [ ] **Step 2: Assert the live end state**

```bash
docker exec aura aura db status
docker exec aura-postgres psql -U aura -d aura -c \
  "SELECT to_regclass('aura.document_pipeline_stages') AS stages, to_regclass('aura.document_pipeline_quarantine') AS quarantine;"
docker exec aura-postgres psql -U aura -d aura -c \
  "SELECT count(*) AS docs, count(*) FILTER (WHERE source_kind IS NULL) AS null_kind, count(*) FILTER (WHERE source_key IS NULL) AS null_key FROM aura.documents;"
```

Expected: `93 / false`; both tables present; `docs=4, null_kind=0, null_key=0` — identical to the rehearsal.

**If this fails:** restore from `D:\tmp\aura-backups\aura-pre0093-2026-08-05.dump` and stop. Do not improvise a forward fix on live data.

- [ ] **Step 3: Recreate the observability sidecars, in this order only**

```bash
docker compose --profile observability up -d --no-deps --force-recreate tempo prometheus
docker compose --profile observability up -d --no-deps grafana
bash /d/Aura/scripts/observability_sidecar_check.sh
```

Expected: exit 0.

- [ ] **Step 4: Prove traces flow — by finding a span, not by reading a healthcheck**

```bash
for i in $(seq 1 10); do curl -s -o /dev/null http://127.0.0.1:9080/api/documents; done
docker logs aura --since 2m 2>&1 | grep -ci "otel error" | xargs echo "otel errors:"
docker exec aura-grafana-1 wget -qO- "http://aura:3200/api/search?limit=3"
```

Expected: `otel errors: 0`, and a `traces` array containing at least one entry with `"rootServiceName":"aura"`. An empty array means the lens is dark — fix before continuing, because every later checkpoint leans on it.

- [ ] **Step 5: Confirm the Cockpit is reachable for the operator**

```bash
curl -s -o /dev/null -w "cockpit=%{http_code}\n" http://127.0.0.1:9080/
curl -s -o /dev/null -w "healthz=%{http_code}\n" http://127.0.0.1:9080/healthz
```

Expected: `cockpit=200`, `healthz=200`.

- [ ] **Step 6: Record and commit**

Append to `PHASE0.md`: the live `db status`, both `to_regclass` results, the otel-error count, and the trace ID that proved the lens live.

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE0.md
git commit -m "Apply 0093 to the live deployment and restore the observability lens"
```

---

### Task 4: Build the operator-identity probe harness

`scripts/document_pipeline_e2e_probe.go` already encodes the assertions and reads through `db.WithIdentityTxRaw`, the RLS-correct channel. `status` needs only `--identity --asset`. `snapshot` additionally needs `--sha256`, `--expected-embed-model`, `--expected-embed-version`, `--expected-docling-producer`, `--state`, `--label`. Reuse it; do not write a second probe.

**Files:**
- Create: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md`

**Interfaces:**
- Produces: a built probe binary at `/tmp/aura-e2e-probe` inside WSL, and the shell function shape used by Tasks 5–10.

- [ ] **Step 1: Build the probe in WSL**

```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && \
  go build -o /tmp/aura-e2e-probe ./scripts/document_pipeline_e2e_probe.go ./scripts/document_pipeline_e2e_support.go && \
  ls -la /tmp/aura-e2e-probe'
```

Expected: a binary. A build failure here is a real defect in the probe sources and is fixed, not worked around.

- [ ] **Step 2: Establish the RLS-correct read used by every checkpoint**

Every direct SQL read below uses this shape. A naked query returns zero rows under RLS and passes against nothing.

```bash
docid() {
  docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
    "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
     SELECT id, status, source_kind, source_key, deleted_at FROM aura.documents ORDER BY created_at DESC LIMIT 5; COMMIT;"
}
docid
```

Expected: the 4 pre-existing rows. **If this returns zero rows, the GUC name is wrong** — read it out of `internal/db` and correct it before any checkpoint runs, because every assertion in Tasks 5–10 depends on this shape being right.

- [ ] **Step 3: Record the baseline and create the ledger**

Create `FINDINGS.md` with a table stub — one row per checkpoint CP1–CP7, columns: *Checkpoint*, *Operator observed*, *Backend held*, *Verdict*, *Evidence*. Fill the header and the pre-run baseline (4 documents, 1 version row).

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md
git commit -m "Open the operator E2E findings ledger with a measured baseline"
```

---

### Task 5 (OPERATOR-PAIRED): CP1–CP2 — ingest and the status vocabulary on real rows

*Covers: status rendering, small file then long window.*

Smallest file first. The 31 MB PDF exists to hold the in-flight window open long enough to observe, not to be the first thing that can fail.

**Files:**
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md`

- [ ] **Step 1: Operator uploads the small file**

Operator, in the Cockpit: upload `documenti da stampare.pdf` (5 KB). Report which tab it appears in, what badge and tone it carries, and whether it moves without a manual refresh.

- [ ] **Step 2: Claude samples the lifecycle throughout**

Run repeatedly from upload until terminal, recording every distinct status seen:

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT d.id, d.status AS doc_status, v.status AS ver_status, v.version_number \
   FROM aura.documents d LEFT JOIN aura.document_versions v ON v.document_id=d.id \
   ORDER BY d.created_at DESC LIMIT 3; COMMIT;"
```

- [ ] **Step 3: Assert the regression guard**

The live database currently holds **3 `ready` documents behind 1 `document_versions` row** — the exact shape the recorder bug produced. The new upload must not add to that count.

```bash
docker exec aura-postgres psql -U aura -d aura -c "SELECT count(*) AS version_rows FROM aura.document_versions;"
```

Expected: **2**, up from the measured baseline of 1. A `ready` document with no version row is a CP1 failure and blocks everything downstream.

- [ ] **Step 4: Operator uploads the long-window files**

Operator: upload `Clienti.xlsx` (331 KB), then `TESEBRO000050EN.pdf` (31 MB). Report every distinct status/badge the UI shows for the large file across its whole lifetime.

- [ ] **Step 5: Claude follows the convert in Tempo and captures the producer**

```bash
docker exec aura-grafana-1 wget -qO- "http://aura:3200/api/search?limit=20"
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT DISTINCT stage, status, producer_version FROM aura.document_pipeline_stages ORDER BY 1; COMMIT;"
```

**Capture `producer_version` for the `convert` stage verbatim.** This is `AURA_DOCUMENT_E2E_EXPECT_DOCLING_PRODUCER` for Task 11. It cannot be known before this moment — the table did not exist before 0093, so the value is discovered here and nowhere earlier.

- [ ] **Step 6: Compare the two vocabularies**

Pass criterion: the set of statuses the UI rendered equals the set the database actually held. A status the UI showed that no row ever carried, or a row status the UI could not render, is a finding **either way**.

The UI's in-flight set deliberately spans reachable and aspirational statuses, with a comment saying so. This step measures which are reachable. It does **not** license deleting the others.

- [ ] **Step 7: Record CP1 and CP2 in the ledger, and commit**

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md
git commit -m "Record CP1-CP2: ingest lifecycle and status vocabulary on real rows"
```

---

### Task 6 (OPERATOR-PAIRED): CP3 — the agent cites the document

**Files:**
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md`

- [ ] **Step 1: Operator asks the ground-truth question**

Operator, in the Cockpit chat:

> Quanti clienti hanno Localita TORINO nel file Clienti.xlsx? Conta le righe del file, non usare la scheda. Rispondi in italiano includendo il numero di versione, l'impronta SHA-256 completa e la citazione canonica della fonte.

Ground truth: **699**.

- [ ] **Step 2: Claude reads the tool sequence from the trace**

```bash
docker exec aura-grafana-1 wget -qO- "http://aura:3200/api/search?tags=&limit=20"
```

Expected in the trace: `document_search` called. For a spreadsheet computation, `document_open` and `shell_exec` as well.

- [ ] **Step 3: Assert the persisted conversation model**

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT id, model FROM aura.conversations ORDER BY created_at DESC LIMIT 1; COMMIT;"
```

Expected: `deepseek/deepseek-v4-flash-0731:nitro` (the running `AURA_LLM_MODEL`).

- [ ] **Step 4: Verdict**

Pass: answer contains **699**; `document_search` was called; the persisted model matches; **no** `web_search` or `web_fetch` appears. A correct number reached without `document_search` is a failure, not a pass — it means the answer did not come from the corpus.

- [ ] **Step 5: Record and commit**

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md
git commit -m "Record CP3: agent answers from the corpus with citation evidence"
```

---

### Task 7 (OPERATOR-PAIRED): CP4 — the delete-in-flight window

*Covers: delete-in-flight. **A defect is the expected outcome.***

`SoftDeleteDocument` only sets `status='deleting'` and enqueues a job; `deleted_at` is written solely by `FinalizeDocumentDelete` after the worker erases the objects. Between the two, the dying row still occupies the partial unique index, and the upsert's `WHERE documents.status <> 'deleting'` guard turns that into `ErrDocumentDeleteInFlight`. That error is **not routed to an HTTP status at the API edge** — the store returns it, a test pins it, nothing else.

**Files:**
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md`

- [ ] **Step 1: Operator deletes, then immediately re-uploads**

Operator: delete `Clienti.xlsx` from the action menu, then **immediately** re-upload the same file — inside the async window, before it disappears from the library. Report exactly what the UI showed: an error, a toast, a spinner, or nothing.

- [ ] **Step 2: Claude confirms the window was genuinely open**

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT id, status, deleted_at FROM aura.documents WHERE status='deleting'; COMMIT;"
```

Expected during the window: at least one row with `status='deleting'` and `deleted_at IS NULL`. If this returns nothing, the re-upload happened after finalize and the checkpoint must be retried — a pass here would be measuring the wrong thing.

- [ ] **Step 3: Capture what the edge actually returned**

```bash
docker logs aura --since 5m 2>&1 | grep -iE "delete_in_flight|ErrDocumentDeleteInFlight|23505|status=5[0-9][0-9]" | tail -20
```

- [ ] **Step 4: Verdict — honesty, not success**

Record the real HTTP status, the real response body, and what the operator saw. If it surfaces as a 500 or as silence, **that is the finding** and it becomes work. It is not a Phase 1 failure and does not block CP5.

- [ ] **Step 5: Record and commit**

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md
git commit -m "Record CP4: what the delete-in-flight window surfaces at the API edge"
```

---

### Task 8 (OPERATOR-PAIRED): CP5 — delete then re-upload, after finalize

*Covers: repeat-source ingest. This is the production proof owed for commit `7248b5880`.*

`aura.documents_source_unique` spanned every row including deleted ones, so a deleted document owned its source forever and re-ingesting returned `23505`. It was replaced with a partial unique index `WHERE deleted_at IS NULL` plus an `ON CONFLICT` making the insert an atomic get-or-create. The unit work proved the seams; only this proves the story.

**Files:**
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md`

- [ ] **Step 1: Operator waits for the tombstone, then re-uploads**

Operator: wait until `Clienti.xlsx` has disappeared from the library, then upload the same file again. Report whether it appears and reaches a ready state.

- [ ] **Step 2: Claude confirms the old row is genuinely tombstoned**

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT id, status, source_key, deleted_at IS NOT NULL AS tombstoned FROM aura.documents \
   WHERE source_key LIKE '%Clienti%' ORDER BY created_at; COMMIT;"
```

Expected: the old row with `tombstoned=t`, and a **new** row with `tombstoned=f` carrying the same `source_key`.

- [ ] **Step 3: Assert the fresh document has a working version**

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT d.id, d.status, v.version_number, v.status FROM aura.documents d \
   JOIN aura.document_versions v ON v.document_id=d.id \
   WHERE d.deleted_at IS NULL AND d.source_key LIKE '%Clienti%'; COMMIT;"
```

Expected: exactly one live row, joined to a real version. A live document with no version row means the re-upload half-succeeded — a failure.

- [ ] **Step 4: Assert no 23505 occurred**

```bash
docker logs aura --since 10m 2>&1 | grep -c "23505" | xargs echo "23505 count:"
```

Expected: `0`. Any occurrence means the partial index is not doing its job on the production path.

- [ ] **Step 5: Record and commit**

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md
git commit -m "Record CP5: repeat-source ingest proven on the production path"
```

---

### Task 9 (OPERATOR-PAIRED): CP6 — the workspace surfaces

*Covers: the rest of the workspace. Never exercised by the script at all.*

**Files:**
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md`

- [ ] **Step 1: Operator walks each surface**

Operator, reporting what each shows: open the details drawer; open the events panel; use the filter bar and search; rename a document; open the storage-orphans panel.

- [ ] **Step 2: Claude asserts the rename persisted**

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT id, title, updated_at FROM aura.documents WHERE deleted_at IS NULL ORDER BY updated_at DESC LIMIT 3; COMMIT;"
```

Expected: the new title, with a fresh `updated_at`.

- [ ] **Step 3: Claude asserts the events panel matches the ledger**

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT event_type, count(*) FROM aura.document_events GROUP BY 1 ORDER BY 1; COMMIT;"
```

Expected: the event types the panel displayed, with matching counts.

- [ ] **Step 4: Claude asserts the orphans panel matches reality**

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT status, count(*) FROM aura.storage_objects GROUP BY 1 ORDER BY 1; COMMIT;"
```

Expected: consistent with what `StorageOrphansPanel` rendered. Divergence is a finding.

- [ ] **Step 5: Record and commit**

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md
git commit -m "Record CP6: workspace surfaces against backend state"
```

---

### Task 10 (OPERATOR-PAIRED): CP7 — teardown and findings triage

**Files:**
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md`

- [ ] **Step 1: Operator deletes the remaining test documents**

Operator: delete every document uploaded during CP1–CP6 through the UI. Leave the 4 pre-existing rows alone.

- [ ] **Step 2: Claude asserts tombstones and object removal**

```bash
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
  "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
   SELECT status, count(*) FROM aura.documents GROUP BY 1 ORDER BY 1; \
   SELECT status, count(*) FROM aura.storage_objects GROUP BY 1 ORDER BY 1; COMMIT;"
```

Expected: the pre-run baseline restored — 3 `ready`, and `deleted` grown by the number of test documents. No `deleting` rows left stuck.

- [ ] **Step 3: Complete the ledger and classify every finding**

For each of CP1–CP7 fill *Operator observed*, *Backend held*, *Verdict*, *Evidence*. Then classify each finding as **blocking Phase 2**, **fix-now**, or **carry-forward**. Say which, and why, for each one.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/FINDINGS.md
git commit -m "Close the operator E2E with a triaged findings ledger"
```

---

### Task 11: Build the Phase 2 restart hook and preflight the script

`scripts/document_pipeline_e2e.sh` has never been run. It requires a restart hook that does not exist and four `EXPECT_*` values, one of which (`DOCLING_PRODUCER`) is only knowable after Task 5 step 5.

The hook must recreate the sidecars after restarting `aura`. Measured 2026-08-05: `docker restart aura` leaves tempo and prometheus alive, `healthy`, and unreachable — so without this the backend lens dies silently at exactly the lease-reclaim assertion the restart exists to prove.

**Files:**
- Create: `scripts/document_pipeline_restart_hook.sh`
- Create: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE2.md`

**Interfaces:**
- Consumes: the `convert`-stage `producer_version` captured in Task 5 step 5; `scripts/observability_sidecar_check.sh` from Task 2.
- Produces: an executable hook satisfying the script's ownership and permission preconditions.

- [ ] **Step 1: Write the hook**

Create `scripts/document_pipeline_restart_hook.sh`:

```bash
#!/usr/bin/env bash
# AURA_DOCUMENT_E2E_RESTART_HOOK — restart aura mid-run to prove lease reclaim.
#
# Restarting aura replaces its network namespace. tempo and prometheus run with
# network_mode: service:aura and survive as processes attached to the dead
# namespace, still reporting healthy while unreachable (measured 2026-08-05).
# Recreating them here is what keeps the run observable across the restart.
set -Eeuo pipefail
# `docker compose` resolves compose.yaml from the working directory, and this hook
# is invoked from an arbitrary one by the E2E script.
cd /mnt/d/Aura
docker restart aura >/dev/null
for _ in $(seq 1 60); do
  [[ "$(docker inspect aura --format '{{.State.Health.Status}}')" == "healthy" ]] && break
  sleep 2
done
[[ "$(docker inspect aura --format '{{.State.Health.Status}}')" == "healthy" ]] || {
  echo "aura did not return to healthy after restart" >&2; exit 1; }
docker compose --profile observability up -d --no-deps --force-recreate tempo prometheus >/dev/null
for _ in $(seq 1 30); do
  bash "$(dirname "$0")/observability_sidecar_check.sh" >/dev/null 2>&1 && exit 0
  sleep 2
done
echo "observability sidecars did not become reachable after restart" >&2
exit 1
```

- [ ] **Step 2: Satisfy the script's ownership preconditions**

The script rejects a hook that is relative, a symlink, non-executable, not owned by the WSL operator, or group/world writable.

```bash
wsl -e bash -lc 'install -m 0755 /mnt/d/Aura/scripts/document_pipeline_restart_hook.sh $HOME/aura-restart-hook.sh && \
  install -m 0755 /mnt/d/Aura/scripts/observability_sidecar_check.sh $HOME/observability_sidecar_check.sh && \
  ls -la $HOME/aura-restart-hook.sh && stat -c "%u %a" $HOME/aura-restart-hook.sh && id -u'
```

Expected: mode `755`, owner UID equal to `id -u`. Note the hook resolves its sibling check script via `dirname "$0"`, so both must land in the same directory.

- [ ] **Step 3: Test the hook standalone, before the script depends on it**

```bash
wsl -e bash -lc '$HOME/aura-restart-hook.sh; echo "hook exit=$?"'
bash /d/Aura/scripts/observability_sidecar_check.sh
```

Expected: `hook exit=0`, and the sidecars reachable afterwards. This is the test that the hook actually repairs what the restart breaks.

- [ ] **Step 4: Record the four EXPECT_\* values**

```
AURA_DOCUMENT_E2E_EXPECT_MODEL=deepseek/deepseek-v4-flash-0731:nitro
AURA_DOCUMENT_E2E_EXPECT_EMBED_MODEL=google/embeddinggemma-300m
AURA_DOCUMENT_E2E_EXPECT_EMBED_VERSION=0f741b5a6585bd53aeb15cd1372c56f2a0f65e12
AURA_DOCUMENT_E2E_EXPECT_DOCLING_PRODUCER=<verbatim convert-stage producer_version from Task 5 step 5>
```

The first three are read off the running container (`AURA_LLM_MODEL`, `AURA_DOCUMENT_CHUNK_TOKENIZER`, `AURA_EMBED_REVISION`). Re-read them from the **rebuilt** container, not from this document — they must describe the image being tested.

- [ ] **Step 5: Commit**

```bash
git add scripts/document_pipeline_restart_hook.sh docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE2.md
git commit -m "Add a restart hook that keeps the E2E observable across the restart"
```

---

### Task 12: Run the hermetic two-tenant gate

Unattended. It restarts the service and takes over an hour; the operator must not be in the Cockpit. The corpus is `realpath`-locked to the exact seven files, so there is no smoke subset — the first run is the full run.

**Files:**
- Modify: `docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE2.md`

- [ ] **Step 1: Run it**

```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH
set -a; . /mnt/d/Aura/.env; set +a
export AURA_DOCUMENT_E2E_PRODUCTION_CONFIRM=I_ACKNOWLEDGE_PRODUCTION_E2E
export AURA_DOCUMENT_E2E_BASE_URL=http://127.0.0.1:9080
export AURA_DOCUMENT_E2E_RESTART_HOOK=$HOME/aura-restart-hook.sh
export AURA_DOCUMENT_E2E_REPORT=$HOME/aura-e2e-report-2026-08-05.json
export AURA_DOCUMENT_E2E_EXPECT_MODEL=deepseek/deepseek-v4-flash-0731:nitro
export AURA_DOCUMENT_E2E_EXPECT_EMBED_MODEL=google/embeddinggemma-300m
export AURA_DOCUMENT_E2E_EXPECT_EMBED_VERSION=0f741b5a6585bd53aeb15cd1372c56f2a0f65e12
export AURA_DOCUMENT_E2E_EXPECT_DOCLING_PRODUCER=<from Task 11 step 4>
cd /mnt/d/Aura && bash scripts/document_pipeline_e2e.sh; echo "e2e exit=$?"'
```

Run in the background; it exceeds any foreground timeout.

- [ ] **Step 2: Read the report, not the exit code alone**

```bash
wsl -e bash -lc 'python3 -m json.tool $HOME/aura-e2e-report-2026-08-05.json | head -80'
```

Every one of the script's named checks must be `PASS`. A check absent from the report is recorded as `FAIL / not_completed` by design — treat it as a failure, never as a skip.

- [ ] **Step 3: Confirm observability survived the mid-run restart**

```bash
bash /d/Aura/scripts/observability_sidecar_check.sh
docker exec aura-grafana-1 wget -qO- "http://aura:3200/api/search?limit=3"
```

Expected: reachable, with spans covering the run. If the lens went dark despite the hook, that is a hook defect and the lease-reclaim evidence for this run is unproven.

- [ ] **Step 4: Confirm the script cleaned up after itself**

```bash
docker exec aura-postgres psql -U aura -d aura -c "SELECT id, name, kind FROM aura.identities ORDER BY created_at;"
```

Expected: only `aura-cli` and `dvdmarchetto@gmail.com`. Leftover tenants mean cleanup did not complete.

- [ ] **Step 5: Write the report and commit**

Fill `PHASE2.md` with the pass/fail table per named check, the `EXPECT_*` values actually used, wall-clock duration, and the observability verdict.

```bash
git add docs/superpowers/verification/2026-08-05-document-pipeline-operator-e2e/PHASE2.md
git commit -m "Record the hermetic two-tenant document pipeline gate result"
```

---

## Closing gates

Not part of a task, but the phase does not close without them:

- `make quality-full` with the stack up — **not run since before these fixes landed**.
- Combined coverage ≥85% across the full tag matrix; mutation ≥70% on the lifecycle/retrieval/delete critical files.
- Quality snapshot (PRD amendment #20): rows whose CI-gate glob matches `internal/documents/**` and `web/src/**` will flag stale. Verify locally first — it must print `ok: … checked N row(s)`.
- Push, and confirm CI is green. **28 commits are currently unpushed**, most of them the operator's parallel v2.1.0 planning; the review range must be chosen to exclude them, as they are contiguous only by accident.
