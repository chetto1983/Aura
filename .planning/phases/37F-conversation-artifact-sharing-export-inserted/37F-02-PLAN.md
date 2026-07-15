---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 02
type: execute
wave: 2
depends_on: ["37F-01"]
files_modified:
  - internal/db/migrations/0040_shared_links.up.sql
  - internal/db/migrations/0040_shared_links.down.sql
  - internal/config/config_share.go
  - internal/config/config.go
  - internal/config/config_share_test.go
autonomous: true
requirements: [WEBSHARE-02, WEBSHARE-03]

must_haves:
  truths:
    - "aura.shared_links and aura.share_audit exist after `aura db migrate`, in ONE migration"
    - "A public shared_links row without expires_at or without token_hash is rejected by the database itself"
    - "A duplicate token_hash is impossible — the unique partial index rejects it"
    - "share_audit is append-only for aura_app (no UPDATE, no DELETE grant)"
    - "Deleting a conversation removes its shared_links rows via FK CASCADE"
    - "The scheduler accepts kind='share_expiry_sweep'"
    - "AURA_SHARE_PUBLIC_ENABLED defaults to false and AURA_SHARE_MAX_EXPIRY_DAYS defaults to 90"
    - "`aura db migrate` down then up round-trips cleanly, leaving no dirty tracker"
  artifacts:
    - path: "internal/db/migrations/0040_shared_links.up.sql"
      provides: "shared_links + share_audit tables, indexes, grants, scheduler kind widen"
      contains: "CREATE TABLE aura.shared_links"
    - path: "internal/db/migrations/0040_shared_links.down.sql"
      provides: "reverse: delete widened kind rows, narrow CHECK, drop tables children-first"
      contains: "DROP TABLE"
    - path: "internal/config/config_share.go"
      provides: "ShareConfig + loadShare() over envutil"
      contains: "AURA_SHARE_PUBLIC_ENABLED"
  key_links:
    - from: "internal/db/migrations/0040_shared_links.up.sql"
      to: "aura.conversations(id)"
      via: "FK ON DELETE CASCADE"
      pattern: "REFERENCES aura\\.conversations\\(id\\)[^;]*ON DELETE CASCADE"
    - from: "internal/config/config_share.go"
      to: "internal/envutil"
      via: "BoolDefault / IntDefault"
      pattern: "envutil\\.(Bool|Int)Default"
  prohibitions:
    - "MUST NOT use migration slot 0036, 0037, 0038 or 0039 — all four are TAKEN by Phase 42"
    - "MUST NOT REPLACE the scheduler_tasks_kind_check member list — widen it by appending, copying the current list FROM DISK"
    - "MUST NOT give share_audit an FK on identity_id — an ON DELETE CASCADE would destroy the audit trail (the family precedent is text + no FK)"
    - "MUST NOT type share_audit.identity_id as uuid — it must hold the literal 'local' that auditIdentityKeys appends"
    - "MUST NOT grant aura_app UPDATE or DELETE on share_audit — the ledger is append-only"
    - "MUST NOT add AURA_SHARE_* to internal/settings AllowedKeys — that allowlist is model-backend-only by explicit invariant + guard test"
    - "MUST NOT store a plaintext token in any column"
---

<objective>
Land migration `0040` (`aura.shared_links` + `aura.share_audit` + the `share_expiry_sweep` scheduler
kind) and the two share config knobs. This is the storage + policy substrate every later 37F plan
builds on.

Purpose: give the share lifecycle a schema that makes the fail-closed invariants **database-enforced**,
not merely code-enforced — a public link with no expiry must be rejected by a CHECK, not only by Go.
Output: one migration pair + `internal/config/config_share.go`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@CLAUDE.md
</context>

## Artifacts this plan produces

`aura.shared_links`, `aura.share_audit`, migration `0040_shared_links.{up,down}.sql`, scheduler kind
`share_expiry_sweep`, `config.ShareConfig`, `AURA_SHARE_PUBLIC_ENABLED`, `AURA_SHARE_MAX_EXPIRY_DAYS`.

<tasks>

<task type="auto">
  <name>Task 1: Migration 0040 up — shared_links + share_audit + scheduler kind widen</name>
  <read_first>
    - **RE-VERIFY THE SLOT FIRST:** run `ls internal/db/migrations/ | tail -1`. At plan time this returned `0039_compaction_rollout.*` and `0040` was free. This project runs multiple phases in flight — if `0040` is now taken, use the next free slot and say so in the SUMMARY. A collision dirties the golang-migrate tracker and blocks every later migration.
    - **RE-READ THE KIND LIST FROM DISK:** `grep -rn "scheduler_tasks_kind_check" -A3 internal/db/migrations/*.up.sql | grep -i "check (kind"`. At plan time the latest was `0034_scheduler_sandbox_reap_kind.up.sql:16` with members `('reminder','agent_job','backup_postgres','backup_neo4j','skill_ttl_sweep','identity_purge','sandbox_reap')`. Copy the CURRENT list, then append. Do NOT retype it from this plan.
    - `internal/db/migrations/0035_assets_source_kind_agent.up.sql` — the header template (Source line / concrete failure mode with SQLSTATE / grant-ownership note)
    - `internal/db/migrations/0034_scheduler_sandbox_reap_kind.up.sql:12-21` — the drop+re-add kind-widen, verbatim
    - `internal/db/migrations/0004_identity.up.sql:13-18` — `capability_grants` shape (free-text capability, PK)
    - `internal/db/migrations/0010_skill_audit.up.sql` — `skill_audit.identity_id text NOT NULL DEFAULT 'local'` (the audit-family column type; ~:29)
    - `internal/db/migrations/0022_*.up.sql` — `mcp_audit.actor_identity_id text NOT NULL` (the same family rule)
    - `internal/agui/audit_store.go:99-104` — `auditIdentityKeys` appends the literal `'local'`; this is WHY `share_audit.identity_id` must be `text`, not `uuid`
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ5 — the DDL and the per-column rationale
  </read_first>
  <action>
    Create `internal/db/migrations/0040_shared_links.up.sql`.

    Header, following 0035's template: `-- Source: Phase 37F (Conversation & Artifact Sharing / Export)
    / WEBSHARE-02 / WEBSHARE-03 / D-11 / D-13 / D-14.` Then the concrete failure this migration fixes
    (share create has nowhere to persist tier/token/expiry/revocation, and an unaudited share is an SC3
    violation), the reason both tables land together (a partial apply would let a share be created
    unaudited), and the grant/ownership note (`aura_migrate` owns DDL; `aura_app` gets DML).

    **`aura.shared_links`** — columns: `id uuid PRIMARY KEY`; `owner_identity_id uuid NOT NULL REFERENCES
    aura.identities(id) ON DELETE CASCADE`; `conversation_id uuid NOT NULL REFERENCES
    aura.conversations(id) ON DELETE CASCADE`; `tier text NOT NULL CHECK (tier IN ('internal','public'))`;
    `token_hash bytea`; `snapshot_id uuid NOT NULL`; `snapshot_bucket text NOT NULL`;
    `format_options jsonb NOT NULL DEFAULT '{}'::jsonb`; `expires_at timestamptz`;
    `revoked_at timestamptz`; `created_at timestamptz NOT NULL DEFAULT now()`;
    `updated_at timestamptz NOT NULL DEFAULT now()`.

    Add the table-level constraint `shared_links_tier_shape`:
    `(tier='public' AND token_hash IS NOT NULL AND expires_at IS NOT NULL) OR (tier='internal' AND
    token_hash IS NULL)`. Comment WHY: this is D-04's mandatory-expiry and D-13's hashed-token made
    **database-enforced** — a public link with no expiry cannot exist even if a Go bug tries to write one.

    `token_hash` is `bytea`, not `text`: SHA-256 is 32 raw bytes and `bytea` removes the hex-vs-base64
    ambiguity. Cite the `secret_key_enc bytea` precedent (0030). Comment that this column holds
    `SHA-256(token)` and that **no plaintext token is stored anywhere** (D-13).

    Indexes:
    - `CREATE UNIQUE INDEX shared_links_token_hash_idx ON aura.shared_links (token_hash) WHERE token_hash IS NOT NULL;`
      — comment that this is BOTH the duplicate-mint guard AND the D-13 hash-indexed-equality lookup index
      (`WHERE token_hash = $1`), so the resolve is an index probe and never a table scan.
    - `CREATE INDEX shared_links_owner_idx ON aura.shared_links (owner_identity_id, created_at DESC);`
    - `CREATE INDEX shared_links_conversation_idx ON aura.shared_links (conversation_id);`
    - `CREATE INDEX shared_links_expiry_idx ON aura.shared_links (expires_at) WHERE revoked_at IS NULL;`
      — the sweep's due-set index.

    **`aura.share_audit`** — columns: `id uuid PRIMARY KEY DEFAULT gen_random_uuid()`;
    `identity_id text NOT NULL`; `share_link_id uuid`; `conversation_id uuid`;
    `action text NOT NULL CHECK (action IN ('create','update','revoke','expire','open'))`; `tier text`;
    `detail text NOT NULL DEFAULT ''`; `created_at timestamptz NOT NULL DEFAULT now()`.
    Index: `CREATE INDEX share_audit_identity_idx ON aura.share_audit (identity_id, created_at DESC);`

    Comment the three deliberate non-obvious choices: (a) `identity_id` is `text` with **NO FK** —
    matching `skill_audit`/`mcp_audit` verbatim; an FK with CASCADE would destroy the audit trail when
    an identity is deleted, the exact opposite of a ledger's purpose, and `text` is also what lets the
    union leg hold the literal `'local'` that `auditIdentityKeys` appends; (b) `share_link_id` and
    `conversation_id` carry **no FK** — the audit outlives the link and the conversation; (c) the ledger
    is **append-only**.

    Grants: `GRANT SELECT, INSERT, UPDATE, DELETE ON aura.shared_links TO aura_app;` and
    `GRANT SELECT, INSERT ON aura.share_audit TO aura_app;` — with an inline comment stating the
    asymmetry IS the audit-integrity statement, not an oversight.

    **Scheduler kind widen** — copy `0034`'s drop+re-add verbatim, appending `'share_expiry_sweep'` to the
    list you read from disk. Comment that the `0009`/`0010` inline column CHECK is auto-named
    `scheduler_tasks_kind_check`, which is why it is dropped by that name.
  </action>
  <verify>
    <automated>bash -c 'set -e; grep -q "CREATE TABLE aura.shared_links" internal/db/migrations/0040_shared_links.up.sql; grep -q "CREATE TABLE aura.share_audit" internal/db/migrations/0040_shared_links.up.sql; grep -q "share_expiry_sweep" internal/db/migrations/0040_shared_links.up.sql; grep -vE "^\s*--" internal/db/migrations/0040_shared_links.up.sql | grep -q "GRANT SELECT, INSERT *ON aura.share_audit"; ! grep -vE "^\s*--" internal/db/migrations/0040_shared_links.up.sql | grep -qE "GRANT[^;]*(UPDATE|DELETE)[^;]*ON aura.share_audit"; echo UP-SQL-OK'</automated>
  </verify>
  <acceptance_criteria>
    - `ls internal/db/migrations/0040_shared_links.up.sql` succeeds and no OTHER `0040_*` migration exists.
    - Comment-stripped source contains `CREATE TABLE aura.shared_links` and `CREATE TABLE aura.share_audit`: `grep -vE '^\s*--' internal/db/migrations/0040_shared_links.up.sql | grep -c "CREATE TABLE"` returns `2`.
    - `grep -vE '^\s*--' internal/db/migrations/0040_shared_links.up.sql | grep -c "REFERENCES aura.conversations(id)"` returns `1`, and the same line contains `ON DELETE CASCADE`.
    - `share_audit.identity_id` is declared `text`: `grep -vE '^\s*--' …up.sql | grep -qE "identity_id\s+text\s+NOT NULL"`.
    - `share_audit` has NO `REFERENCES`: the `CREATE TABLE aura.share_audit` block contains zero `REFERENCES` tokens.
    - The unique partial index exists: `grep -q "CREATE UNIQUE INDEX shared_links_token_hash_idx" …up.sql` and the line contains `WHERE token_hash IS NOT NULL`.
    - `shared_links_tier_shape` CHECK exists and mentions both `token_hash IS NOT NULL` and `expires_at IS NOT NULL` for the public branch.
    - The widened kind list is the CURRENT on-disk list plus exactly one new member: the `CHECK (kind IN (...))` in `0040` contains every member present in the latest pre-existing kind CHECK, plus `'share_expiry_sweep'`, and nothing removed.
    - `aura_app` has no UPDATE/DELETE on `share_audit` (asserted by the automated verify's negated grep).
    - Applies cleanly against a disposable DB: `bash scripts/coverage_docker.sh` reaches the migrate step without a dirty-tracker error (full run is Task 3's gate).
  </acceptance_criteria>
  <done>`0040_shared_links.up.sql` creates both tables with the tier-shape CHECK, the unique partial token_hash index, the three secondary indexes, the asymmetric grants, and the appended scheduler kind — every non-obvious choice carrying a WHY comment.</done>
</task>

<task type="auto">
  <name>Task 2: Migration 0040 down — reverse in dependency order without dirtying the tracker</name>
  <read_first>
    - `internal/db/migrations/0034_scheduler_sandbox_reap_kind.down.sql` — the whole file. This is the down most likely to be gotten wrong: it deletes the rows the widening admitted BEFORE narrowing the CHECK, and deletes FK-children (`agent_job_runs`) first.
    - `internal/db/migrations/0035_assets_source_kind_agent.down.sql` — the same rule stated for data, with the reason inline
    - `internal/db/migrations/0004_identity.down.sql:1-3` — the "capability_grants FK ON DELETE CASCADE means dropping it explicitly first is …" comment: the house style for explaining drop order
    - The `0040_shared_links.up.sql` you just wrote — the down must reverse exactly it
  </read_first>
  <action>
    Create `internal/db/migrations/0040_shared_links.down.sql`.

    Header: state that a down which narrows the kind CHECK must FIRST remove the rows the widening
    admitted — a live `share_expiry_sweep` task row violates the restored CHECK and aborts the whole down
    mid-chain, leaving a dirty database.

    Order, strictly:
    1. `DELETE FROM aura.agent_job_runs WHERE task_id IN (SELECT id FROM aura.scheduler_tasks WHERE kind = 'share_expiry_sweep');`
       — FK children first. Comment that `agent_job_runs` FKs `scheduler_tasks(id)` ON DELETE CASCADE so
       this is explicit for parity with 0034, not strictly required.
    2. `DELETE FROM aura.scheduler_tasks WHERE kind = 'share_expiry_sweep';`
    3. Drop + re-add `scheduler_tasks_kind_check` with the **pre-37F** member list — i.e. the list you
       read from disk in Task 1, WITHOUT `'share_expiry_sweep'`.
    4. `DROP TABLE IF EXISTS aura.share_audit;` then `DROP TABLE IF EXISTS aura.shared_links;` — children
       before parents. Comment that neither table is referenced BY another table, so the order here is
       stylistic parity with 0004's stated rule rather than an FK requirement.

    Do NOT drop `aura.identities` or `aura.conversations`. Do NOT touch any 0036-0039 object.
  </action>
  <verify>
    <automated>bash -c 'set -e; grep -q "DROP TABLE IF EXISTS aura.share_audit" internal/db/migrations/0040_shared_links.down.sql; grep -q "DROP TABLE IF EXISTS aura.shared_links" internal/db/migrations/0040_shared_links.down.sql; grep -q "DELETE FROM aura.scheduler_tasks WHERE kind = .share_expiry_sweep." internal/db/migrations/0040_shared_links.down.sql; ! grep -qE "CHECK \(kind IN \([^)]*share_expiry_sweep" internal/db/migrations/0040_shared_links.down.sql; echo DOWN-SQL-OK'</automated>
  </verify>
  <acceptance_criteria>
    - The DELETE of `share_expiry_sweep` scheduler rows appears at a lower line number than the `ADD CONSTRAINT scheduler_tasks_kind_check` line: verified by `grep -n` ordering.
    - The `agent_job_runs` DELETE appears at a lower line number than the `scheduler_tasks` DELETE.
    - The restored CHECK does NOT contain `share_expiry_sweep` (asserted by the automated verify's negated grep).
    - The restored CHECK member list is byte-identical to the latest pre-37F kind list read from disk in Task 1.
    - `DROP TABLE ... share_audit` appears at a lower line number than `DROP TABLE ... shared_links`.
    - Round-trips on a disposable DB: `aura db migrate` up → down → up leaves no dirty tracker (exercised by Task 3's gate).
  </acceptance_criteria>
  <done>`0040_shared_links.down.sql` reverses the up in strict dependency order — kind rows deleted before the CHECK narrows, FK children before parents — and round-trips without dirtying the migrate tracker.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Share config knobs — AURA_SHARE_PUBLIC_ENABLED (default false) + AURA_SHARE_MAX_EXPIRY_DAYS (default 90)</name>
  <read_first>
    - **RE-MEASURE FIRST:** `wc -l internal/config/config.go`. At plan time this was **592/600** — a LOC landmine that neither RESEARCH (R-01/R-02/R-03) nor PATTERNS caught. The wiring delta must stay ≤ 2 LOC or `make quality` fails pre-push AND blocks every commit (the hook scans the whole tree).
    - `internal/config/config_compaction.go:1-30` and `internal/config/config_sandbox.go` — the per-subsystem config split precedent. `config.go:261` shows the one-line `Sandbox SandboxConfig` field wiring.
    - `internal/envutil/envutil.go:19-45` — `IntDefault(key, fallback)` / `BoolDefault(key, fallback)`; the silent-fallback contract (a malformed value absorbs to the fallback, never a fatal boot)
    - `internal/config/config_env.go:1-14` — the header stating WHY the env helpers were split out (refactor-on-touch, ≤600 LOC)
    - `internal/settings/settings.go:44-62` — `AllowedKeys`. Read it to confirm the share knobs do NOT belong there: it is a model-backend-only allowlist whose package doc states it exists so "a settings row can never clobber connection/security env".
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ2 (the org kill-switch) + §R-08 (the loopback fail-open)
  </read_first>
  <behavior>
    - `AURA_SHARE_PUBLIC_ENABLED` unset ⇒ `ShareConfig.PublicEnabled == false` (fail-closed default)
    - `AURA_SHARE_PUBLIC_ENABLED=true` ⇒ `PublicEnabled == true`
    - `AURA_SHARE_PUBLIC_ENABLED=garbage` ⇒ `PublicEnabled == false` (silent fallback, never a fatal boot)
    - `AURA_SHARE_MAX_EXPIRY_DAYS` unset ⇒ `MaxExpiryDays == 90`
    - `AURA_SHARE_MAX_EXPIRY_DAYS=30` ⇒ `MaxExpiryDays == 30`
    - `AURA_SHARE_MAX_EXPIRY_DAYS=garbage` ⇒ `MaxExpiryDays == 90` (silent fallback)
    - `AURA_SHARE_MAX_EXPIRY_DAYS=0` and negative values ⇒ clamped to the 90 default (a zero/negative cap would make every public mint fail or never expire; neither is a sane operator intent)
  </behavior>
  <action>
    Create `internal/config/config_share.go` — a per-subsystem config file, following
    `config_compaction.go` / `config_sandbox.go`. Do NOT put this in `config.go` (592/600).

    Define `ShareConfig` with `PublicEnabled bool` and `MaxExpiryDays int`, and an unexported
    `loadShare() ShareConfig` reading `envutil.BoolDefault("AURA_SHARE_PUBLIC_ENABLED", false)` and
    `envutil.IntDefault("AURA_SHARE_MAX_EXPIRY_DAYS", 90)`, clamping a non-positive `MaxExpiryDays` back
    to 90.

    The file header must state the two non-obvious WHYs (CLAUDE.md: comments only where the why is
    non-obvious — both of these qualify):
    - **Why `PublicEnabled` defaults to `false`:** it is the D-02 org kill-switch AND the closure for
      R-08. `RequireCapability` returns `next` unchanged when `!SecretConfigured`
      (`internal/agui/auth.go:282`), so on loopback the `share.public` capability gate does not exist.
      The kill-switch is re-checked **inside** the share-create handler, where no such bypass applies.
      Two independent gates; one survives loopback. A default of `true` would make loopback dev able to
      mint public links with no gate at all.
    - **Why this is an env knob and not an `aura.settings` row:** `settings.AllowedKeys` is a static
      model-backend-only allowlist that exists specifically so a settings row can never reach
      connection/security env; a share kill-switch is security env. `settings.OverlayEnv` is also
      boot-time only.

    Wire into `config.go` with **at most 2 LOC**: one `Share ShareConfig` field on `Config` (mirroring
    `Sandbox SandboxConfig` at `:261`) and one `Share: loadShare(),` in the composer. Re-run
    `wc -l internal/config/config.go` after the edit; if it exceeds 598, split something out of
    `config.go` in the SAME commit (CLAUDE.md DEEP REFACTOR ON TOUCH) rather than shipping at the cap.

    Write `internal/config/config_share_test.go` as a table-driven test over the `<behavior>` cases,
    using `t.Setenv` (which auto-restores). Plain unit test — **no build tag**.

    ⚠️ Known footgun: `.env` exports `AURA_WEB_AUTH_SECRET` and leaks into `make coverage`, breaking
    config tests. Do not add a `.env` dependency here; `t.Setenv` only.
  </action>
  <verify>
    <automated>go test ./internal/config/ -run 'TestShare' -count=1 && go vet ./internal/config/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/config/ -run 'TestShare' -count=1` passes, covering all 7 `<behavior>` rows.
    - `wc -l internal/config/config.go` returns ≤ 598 (was 592; the delta is ≤ 2 unless a split landed).
    - `wc -l internal/config/config_share.go` returns ≤ 600.
    - `bash scripts/check-file-size.sh` exits 0.
    - `grep -c "AURA_SHARE_PUBLIC_ENABLED\|AURA_SHARE_MAX_EXPIRY_DAYS" internal/config/config_share.go` returns ≥ 2.
    - `grep -rn "AURA_SHARE" internal/settings/` returns NOTHING — the knobs are not in the settings allowlist.
    - The file header names `auth.go:282` as the reason the default is `false`.
    - `go test -race ./internal/config/ -count=1` passes.
    - `go build ./...` succeeds.
  </acceptance_criteria>
  <done>`internal/config/config_share.go` exposes `ShareConfig{PublicEnabled=false, MaxExpiryDays=90}` by default over `envutil`, wired into `Config` in ≤2 LOC, with the R-08 rationale in the header and a table-driven test green.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Go code → database | A Go bug that tries to mint a public link with no expiry, or a duplicate token, must be rejected by the schema itself — the CHECK and the unique index are the last line, not the first. |
| environment → process | `AURA_SHARE_PUBLIC_ENABLED` is the one gate that survives loopback (`!SecretConfigured`), so its default is a security property. |
| identity deletion → audit trail | An FK on `share_audit.identity_id` would cascade-delete the evidence of what an identity shared. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-03 | Elevation of Privilege | `RequireCapability` loopback pass-through (`auth.go:282`) | mitigate | `AURA_SHARE_PUBLIC_ENABLED` defaults `false`; re-checked in-handler (plan 37F-10), so the gate exists even when the mount-level capability check vanishes. |
| T-37F-11 | Information Disclosure | plaintext token at rest | mitigate | `token_hash bytea` holds `SHA-256(token)` only; no column can hold a plaintext token; the `shared_links_tier_shape` CHECK forces `token_hash IS NOT NULL` for public. |
| T-37F-10 | Repudiation | share act unaudited | mitigate | `share_audit` lands in the SAME migration as `shared_links` — a partial apply cannot yield a table that records shares without a table that audits them. Append-only grants (no UPDATE/DELETE for `aura_app`) prevent post-hoc tampering. |
| T-37F-15 | Tampering | audit trail destroyed by identity delete | mitigate | `share_audit.identity_id` is `text` with NO FK, matching `skill_audit`/`mcp_audit`; the ledger outlives the identity, the link, and the conversation. |
| T-37F-16 | Denial of Service | migration slot collision dirties the tracker | mitigate | Slot re-verified from disk at execute time; down migration deletes widened-kind rows before narrowing the CHECK so a rollback cannot abort mid-chain. |
| T-37F-17 | Spoofing | duplicate/guessable token row | mitigate | `shared_links_token_hash_idx` UNIQUE partial index makes a duplicate mint impossible and serves the indexed-equality lookup (amended D-13). |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | No new dependency in this plan (stdlib + existing `envutil` only). |
</threat_model>

<verification>
- `go build ./... && go vet ./...`
- `go test -race ./internal/config/ -count=1`
- `bash scripts/check-file-size.sh` → 0 (config.go must not breach 600)
- Migration round-trip on a **disposable** DB only: `bash scripts/coverage_docker.sh` (provisions `aura_cov`, drops it on exit). **NEVER** run the gate against the live `aura` DB — `coverage_gate.sh:35` refuses it locally; this closed the 2026-07-10 footgun that wiped the live deployment's auth tables.
</verification>

<success_criteria>
Migration 0040 applies and reverses cleanly on a disposable DB, both tables exist with
database-enforced fail-closed invariants (public ⇒ token_hash + expires_at; unique token_hash;
append-only audit; conversation FK CASCADE), the scheduler accepts `share_expiry_sweep`, and the two
share knobs default fail-closed (`false` / 90 days) without touching the settings allowlist.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-02-SUMMARY.md` when done.
Record the migration slot actually used and the post-edit `wc -l internal/config/config.go`.
</output>
