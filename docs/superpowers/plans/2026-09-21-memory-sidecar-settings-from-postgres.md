# Memory Sidecar Configuration From Postgres — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `cmd/arcadedb-mcp` reads its configuration from Postgres `aura.settings` at its own boot instead of from environment variables, so the daemon and the memory sidecar cannot disagree about the embedder.

**Architecture:** At sidecar boot, before any existing env read, open a pgx pool on `AURA_DB_URL`, build a `settings.Store`, and call `settings.OverlayEnv` — which writes the allowlisted non-secret rows into the process environment. The existing env readers then pick them up unchanged, so no per-field mapping is invented. The cloud embedding credential is a secret and never passes through the environment: it is read with `store.Secret`. This is exactly what `cmd/aura-media-index/main.go` already does; the seam is reused, not re-designed.

**Tech Stack:** Go, `github.com/jackc/pgx/v5/pgxpool`, `internal/settings`, `internal/arcadedb`, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-09-21-memory-sidecar-settings-from-postgres-design.md`

## Global Constraints

- No file over 600 LOC; refactor on touch (CLAUDE.md §Behavioral rules).
- `go vet ./... && go build ./...` clean, and `go test -race` on every package touched.
- A `t.Skip` that fires under `$CI` is a falsely-green job: skip-helpers `t.Fatal` when `$CI` is set and a required var is unset.
- No dead code and no orphan TODO. `AURA_EMBED_API_KEY` is dead on arrival and this plan removes it rather than wiring it.
- Commit messages: imperative subject, body explaining **why**, trailer `Co-Authored-By: Codex (gpt-5.6-sol) <noreply@openai.com>` (or the implementing model). Do not push.
- Migration numbering, if one is ever needed: `ls internal/db/migrations/ | tail -1` is the only source of the next slot. **This plan needs no migration** — `aura.settings` already exists.

## File Structure

| File | Responsibility |
|---|---|
| `cmd/arcadedb-mcp/settings.go` (new) | The whole Postgres→environment step for this binary: pool, store, overlay, and the one secret read. Nothing else. Kept out of `main.go`, which is already the biggest file in the package. |
| `cmd/arcadedb-mcp/settings_test.go` (new) | Daemon-free unit tests over the `settings.Lister` seam and over the fail/degrade decision. No Postgres required. |
| `cmd/arcadedb-mcp/main.go` (modify) | `run()` calls the new step first; the embedder's third argument stops being the dead `AURA_EMBED_API_KEY`. |
| `compose.yaml` (modify, `arcadedb-mcp` service block only) | Supplies `AURA_DB_URL` and `AURA_AUTHULA_SECRET` using the exact composition every other service already uses. |
| `.env.example` (modify) | The manual "must AGREE" burden is removed; says what is now authoritative. |

---

### Task 1: The settings step, tested without a database

**Files:**
- Create: `cmd/arcadedb-mcp/settings.go`
- Test: `cmd/arcadedb-mcp/settings_test.go`

**Interfaces:**
- Consumes: `settings.Lister` (`List(ctx context.Context) ([]sqlc.AuraSettings, error)`), `settings.OverlayEnv(ctx context.Context, l Lister) error`, `settings.NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error)`, `(*settings.Store).Secret(ctx context.Context, key string) (string, error)`.
- Produces: `applySettings(ctx context.Context, logger *slog.Logger) (embedAPIKey string, err error)` — called by `run()` before any other env read.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

type fakeLister struct {
	rows []sqlc.AuraSettings
	err  error
}

func (f fakeLister) List(context.Context) ([]sqlc.AuraSettings, error) { return f.rows, f.err }

// The daemon and this sidecar must not disagree about the embedder. The overlay is how they
// are made to agree: a row in aura.settings wins over whatever compose put in the container.
func TestOverlayFromSettingsWinsOverTheContainerEnvironment(t *testing.T) {
	t.Setenv("AURA_EMBED_MODEL", "from-compose")
	rows := []sqlc.AuraSettings{{Key: "AURA_EMBED_MODEL", Value: "qwen/qwen3-embedding-8b"}}
	if err := overlayFrom(t.Context(), fakeLister{rows: rows}); err != nil {
		t.Fatalf("overlayFrom: %v", err)
	}
	if got := os.Getenv("AURA_EMBED_MODEL"); got != "qwen/qwen3-embedding-8b" {
		t.Errorf("AURA_EMBED_MODEL = %q, want the settings row to win", got)
	}
}

// A configured database that cannot be read is NOT a reason to run on stale container env:
// that is the silent divergence this change exists to remove.
func TestOverlayFailsLoudWhenTheStoreCannotBeRead(t *testing.T) {
	err := overlayFrom(t.Context(), fakeLister{err: errors.New("connection refused")})
	if err == nil {
		t.Fatal("overlayFrom returned nil on an unreadable store; boot must fail instead")
	}
}

// No database configured at all is the documented degraded mode, and it must SAY so.
func TestNoDatabaseConfiguredDegradesAndSaysSo(t *testing.T) {
	t.Setenv("AURA_DB_URL", "")
	var buf syncBuffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	key, err := applySettings(t.Context(), logger)
	if err != nil {
		t.Fatalf("applySettings without AURA_DB_URL: %v", err)
	}
	if key != "" {
		t.Errorf("secret = %q, want empty without a store to read it from", key)
	}
	if !buf.contains("AURA_DB_URL") {
		t.Errorf("log = %q, want it to name the missing variable", buf.String())
	}
}
```

Add the tiny `syncBuffer` helper in the same file (a `bytes.Buffer` behind a mutex with a `contains(string) bool`), because `slog` writes from the calling goroutine and the test reads after.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/arcadedb-mcp/ -run 'Overlay|NoDatabaseConfigured' -v`
Expected: FAIL — `undefined: overlayFrom`, `undefined: applySettings`.

- [ ] **Step 3: Write the implementation**

```go
package main

// settings.go is this binary's ONE step from Postgres to its environment. aura.settings is
// the configuration authority for the product; compose supplies only what the sidecar cannot
// learn from Postgres, namely where Postgres is and the key that opens its sealed rows.
//
// The indirection through os.Setenv is deliberate and is the shape cmd/aura and
// cmd/aura-media-index already use: the existing env readers below keep working unchanged,
// so no per-field mapping exists to drift.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

// overlayFrom applies the allowlisted non-secret rows onto the process environment. Split
// from applySettings so the decision is testable without a database.
func overlayFrom(ctx context.Context, l settings.Lister) error {
	if err := settings.OverlayEnv(ctx, l); err != nil {
		return fmt.Errorf("settings overlay: %w", err)
	}
	return nil
}

// applySettings overlays aura.settings onto the environment and returns the shared cloud
// credential, which is a secret and therefore never reaches the environment where every
// child process would inherit it.
//
// With AURA_DB_URL unset the sidecar runs on container env alone and SAYS so: that is the
// pre-Postgres behaviour, kept so a deployment that has not been re-templated still boots,
// but it is logged at WARN because in that mode nothing guarantees this sidecar and the
// daemon embed into the same vector space. A URL that IS set and cannot be read is a hard
// failure: running on stale env would reintroduce exactly that silent divergence.
func applySettings(ctx context.Context, logger *slog.Logger) (string, error) {
	dbURL := strings.TrimSpace(os.Getenv("AURA_DB_URL"))
	if dbURL == "" {
		logger.Warn("settings: AURA_DB_URL unset; running on container environment only, " +
			"which does not guarantee the daemon and this sidecar share an embedder")
		return "", nil
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return "", fmt.Errorf("settings database: %w", err)
	}
	defer pool.Close()

	store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
	if err != nil {
		return "", fmt.Errorf("settings store: %w", err)
	}
	if err = overlayFrom(ctx, store); err != nil {
		return "", err
	}
	// The same single credential the daemon's cloud routes ride (config.EmbedRoute returns
	// LLM.APIKey). There is deliberately no embed-specific key: a second one could only ever
	// be the one that is stale.
	key, err := store.Secret(ctx, "OPENROUTER_API_KEY")
	if err != nil {
		return "", fmt.Errorf("settings secret: %w", err)
	}
	return key, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/arcadedb-mcp/ -run 'Overlay|NoDatabaseConfigured' -v`
Expected: PASS, all three.

- [ ] **Step 5: Run the gates for the package**

Run: `go vet ./cmd/arcadedb-mcp/ && go build ./... && go test -race ./cmd/arcadedb-mcp/`
Expected: no findings, build clean, tests pass under the race detector.

- [ ] **Step 6: Commit**

```bash
git add cmd/arcadedb-mcp/settings.go cmd/arcadedb-mcp/settings_test.go
git commit -m "feat(memory): read the sidecar's settings from Postgres at boot"
```

---

### Task 2: Wire it into boot and delete the dead credential

**Files:**
- Modify: `cmd/arcadedb-mcp/main.go` — `run()` around lines 48-75.

**Interfaces:**
- Consumes: `applySettings(ctx, logger) (string, error)` from Task 1.
- Produces: nothing new; `arcadedb.NewSidecarEmbedder(baseURL, model, apiKey string, timeout time.Duration) *SidecarEmbedder` now receives a credential that actually exists.

Background the implementer needs: `AURA_EMBED_API_KEY` is read at `main.go:68` and is set by **no** compose file and **no** `.env.example`. Grep the tree: the only other hits are two live integration tests. The memory sidecar therefore cannot authenticate to a cloud embedder today at all. It is not wired up — it is deleted, and the shared credential takes its place.

- [ ] **Step 1: Write the failing test**

```go
// The sidecar used to read AURA_EMBED_API_KEY, which nothing ever set, so a cloud embedder
// could never be authenticated. The credential now comes from the settings store, like every
// other cloud leg in the product.
func TestEmbedCredentialDoesNotComeFromTheDeadEnvVar(t *testing.T) {
	t.Setenv("AURA_EMBED_API_KEY", "this-variable-is-set-by-nothing")
	body, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if strings.Contains(string(body), "AURA_EMBED_API_KEY") {
		t.Error("main.go still reads AURA_EMBED_API_KEY; the credential must come from applySettings")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/arcadedb-mcp/ -run TestEmbedCredentialDoesNotComeFromTheDeadEnvVar -v`
Expected: FAIL — `main.go still reads AURA_EMBED_API_KEY`.

- [ ] **Step 3: Change `run()`**

Replace the opening of `run` so the overlay happens before anything reads the environment, and feed the embedder the real credential:

```go
func run(logger *slog.Logger) error {
	ctx := context.Background()
	// BEFORE configFromEnv and before the embedder: every reader below sees the overlaid
	// values, which is the whole point of doing this at boot rather than per call.
	embedAPIKey, err := applySettings(ctx, logger)
	if err != nil {
		return err
	}
	cfg, addr, err := configFromEnv()
	if err != nil {
		return err
	}
	bodyMaxBytes, err := positiveInt64Env("AURA_ARCADEDB_MCP_BODY_MAX_BYTES", defaultBodyMaxBytes)
	if err != nil {
		return err
	}
	embedder := arcadedb.NewSidecarEmbedder(
		os.Getenv("AURA_EMBED_BASE_URL"),
		os.Getenv("AURA_EMBED_MODEL"),
		embedAPIKey,
		0,
	)
```

Keep the existing comment block above `embedder` — it explains why the dense leg is optional, which is still true. If `run` already builds a `ctx` further down, reuse that one rather than adding a second.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/arcadedb-mcp/ -v`
Expected: PASS, including the new test and the pre-existing ones.

- [ ] **Step 5: Run the gates**

Run: `go vet ./... && go build ./... && go test -race ./cmd/arcadedb-mcp/`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/arcadedb-mcp/main.go cmd/arcadedb-mcp/settings_test.go
git commit -m "fix(memory): replace the dead AURA_EMBED_API_KEY with the shared credential"
```

---

### Task 3: Give the container what it needs, and nothing more

**Files:**
- Modify: `compose.yaml` — inside the `arcadedb-mcp` service's `environment:` block ONLY. Re-read the file immediately before editing; another agent has been editing this file today.

**Interfaces:**
- Consumes: the DSN composition already used by every other service.
- Produces: `AURA_DB_URL` and `AURA_AUTHULA_SECRET` in the sidecar container.

- [ ] **Step 1: Add the two variables**

Copy the composition verbatim from the `aura` service (compose.yaml:67 and :70) so there is one spelling in the file, not two:

```yaml
      # The sidecar reads its own configuration from aura.settings at boot
      # (cmd/arcadedb-mcp/settings.go). These two are the bootstrap it cannot learn from
      # Postgres: where Postgres is, and the key that opens the sealed secret rows.
      AURA_DB_URL: postgres://${AURA_DB_APP_ROLE:-aura_app}:${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in .env}@postgres:5432/${POSTGRES_DB:-aura}?sslmode=disable
      AURA_AUTHULA_SECRET: ${AURA_AUTHULA_SECRET:?AURA_AUTHULA_SECRET required in .env}
```

- [ ] **Step 2: Decide the startup order and write down the decision**

The `aura` service gates its boot on this sidecar's healthcheck, and that healthcheck is deliberately liveness-only. Check whether `arcadedb-mcp` already declares `depends_on: postgres`. If it does not, adding it is correct — a sidecar that fails boot because Postgres is not up yet would cascade into the daemon starting with zero memory tools. If you decide against it, say why in the commit message. Do not leave the question unanswered.

- [ ] **Step 3: Verify compose still parses and the block is the only change**

Run: `docker compose config --quiet && git diff --stat compose.yaml`
Expected: no output from the first command; the diff touches only the `arcadedb-mcp` block.

- [ ] **Step 4: Commit**

```bash
git add compose.yaml
git commit -m "build(memory): hand the sidecar its Postgres bootstrap"
```

---

### Task 4: Retire the manual agreement burden from the docs

**Files:**
- Modify: `.env.example` — the "memory sidecar embedder" block (search for `AURA_MEMORY_EMBED_BASE_URL`).

The block currently tells the operator the sidecar's embedder "must AGREE with `AURA_EMBED_BASE_URL` above", because nothing enforced it. The overlay now does: both containers read the same `aura.settings` rows. The paragraph must stop asking for something the code now guarantees, and must say what is left that it does not.

- [ ] **Step 1: Rewrite the block**

State three things, and nothing that is not measured: (1) the embedder is chosen in the cockpit and stored in `aura.settings`, and both the daemon and the sidecar read it at their own boot; (2) `AURA_MEMORY_EMBED_BASE_URL` remains only as a compose-level override for a deployment that deliberately wants the sidecar somewhere else, and using it reintroduces the divergence by hand; (3) the base carries no trailing `/v1` — this client appends `/v1/embeddings` itself.

- [ ] **Step 2: Verify the env catalogue test still passes**

Run: `go test ./cmd/aura/ -run EnvExample -v`
Expected: PASS. It checks active `NAME=value` lines; commented examples are documentation.

- [ ] **Step 3: Commit**

```bash
git add .env.example
git commit -m "docs(memory): the embedder agreement is enforced, not requested"
```

---

### Task 5: Prove it on the live stack

Unit tests prove the decision; they do not prove the sidecar boots against a real Postgres and reads a real row. CLAUDE.md §DEFINITION OF DONE requires the live run.

- [ ] **Step 1: Bring the stack up**

Run: `make db-migrate memory-up`

- [ ] **Step 2: Write a row the sidecar must pick up**

Set the embedding model from the cockpit Settings page (Backends pane), or write the row directly, then restart only the sidecar:

```bash
docker compose restart aura-arcadedb-mcp
docker compose logs --no-log-prefix aura-arcadedb-mcp | tail -20
```

Expected: the `dense retrieval enabled` line names the base URL from `aura.settings`, not the compose default — and no `AURA_DB_URL unset` warning.

- [ ] **Step 3: Prove the failure path fails**

A gate that cannot fail is not a gate. Point `AURA_DB_URL` at a closed port, restart the sidecar, and confirm it exits non-zero with `settings database:` in the log rather than starting on stale env.

- [ ] **Step 4: Record the result**

Append the two observed log lines to this plan under a `## Live run` heading, with the date. A claim without its evidence is the thing this project keeps paying for.

---

## Self-review notes

- **Spec coverage.** Spec facts 1-4 → Task 1. Fact 7 (the dark credential) → Task 2. Facts 6 and 8 → Task 3. Fact 8's "manual burden" → Task 4. Fact 9 (the daemon's new embed shape) is read-only context and constrains Task 2's credential choice.
- **Open question the spec raised and this plan ANSWERS:** the spec asked whether a separate embed credential row should exist. It should not — the daemon's `EmbedRoute` returns `LLM.APIKey`, so the sidecar uses the same `OPENROUTER_API_KEY` secret, exactly as `cmd/aura-media-index` does. A second key could only ever be the stale one.
- **Open question this plan does NOT answer:** whether boot-time-only is enough. The overlay makes the sidecar correct at restart, not live. Nothing here builds a reload path; if one is wanted it is separate work with its own spec.
- **ArcadeDB credentials stay in compose.** `internal/settings`' package comment states the allowlist exists precisely so a settings row can never clobber connection/security env (`POSTGRES_*`, `ARCADEDB_PASSWORD`, `AURA_WEB_AUTH_SECRET`). Moving them would contradict a stated invariant to save one line of YAML.
