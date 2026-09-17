# Studio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A cockpit `studio` mode where any signed-in identity generates an image or a video
from one bar, without an agent turn, and keeps the results in a personal history.

**Architecture:**
- **Backend.** The paid paths move out of the tools into `mediagen.VideoSubmitter` and
  `mediagen.ImageGenerator`, shared by `video_generate`, `image_generate` and a new Studio API.
  Studio results are `aura.media_job` rows with `surface = 'studio'` and no conversation: a
  video row is the durable job the watcher already supervises, an image row is inserted
  already completed.
- **Frontend.** A new `web/src/studio/` module: a pure form model, a react-query data layer,
  and a page built from the cockpit's shadcn components — Lumina's layout, Aura's colours.

**Tech Stack:** Go 1.27 (`mediagen`, `agui`, `assets`, sqlc, golang-migrate), Postgres
`aura.*`, React 19 + Vite, `@tanstack/react-query` 5, shadcn (new-york, `radix-ui`), vitest,
Playwright.

**Spec:** `docs/superpowers/specs/2026-09-17-video-studio-design.md`

## Global Constraints

- **Files.** No file above 600 LOC. Refactor on touch: `internal/agui/server.go` is at 599 LOC,
  so a line added there needs a split first.
- **Migrations.** The number is `ls internal/db/migrations/ | tail -1` + 1, measured at landing
  (0128 is the head on 2026-09-17); the same commit updates the head pin and its sentence in
  `internal/db/db_unit_test.go`.
- **Integration tests.** Only on the disposable `aura_cov` database, never the live `aura` one.
- **Mutation testing.** CI only; never run `go-mutesting` or Stryker locally.
- **Paid runs.** A paid generation needs the operator's go in the conversation.
- **Tests.** Never modify a test to make it pass unless the test itself is wrong, and justify
  it in the commit.
- **Git.** Work on `master`, stage explicit paths, do not push unless the operator asks, and end
  every commit with `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.
- **Colours and type are Aura's** theme tokens, light and dark — Lumina gives the layout only.
- **Strings** exist in both `en` and `it` (parity gate).
- **`localStorage`** is always wrapped in try/catch.
- **Studio HTTP status per tool code:**

  | Code | Status |
  |---|---|
  | `unsupported`, `model_rejected`, `asset_not_found`, `too_large`, `content_blocked` | 422 |
  | `no_key` | 409 |
  | `no_credit` | 402 |
  | `job_failed`, `outcome_unknown` | 502 |

- **Defaults** are the cheapest declared options: lowest resolution, shortest duration, audio
  off, no seed.
- **Price SKU order** for `VideoSecondPrice`: `duration_seconds_{with|without}_audio_{res}`,
  then `duration_seconds_{with|without}_audio`, then `duration_seconds_{res}`, then
  `duration_seconds`, then the same four as `cents_per_second` divided by 100.
- **Go gates after every Go edit:** `go vet ./...`, `go build ./...`, the touched packages'
  tests, and `-race` in WSL (`export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`).
- **Web gates after every web edit:** `npm run typecheck`, `npm run lint`, vitest for the
  touched files, `npm run dup`, `npm run deadcode`.

---

## File map

**Go**
- `internal/db/migrations/0129_media_job_studio.{up,down}.sql` — `surface`, `kind`, their
  checks, the history index.
- `internal/db/queries/media_jobs.sql` — insert with surface and kind; a completed-image
  insert; completion marks a Studio row delivered and checks the modality by kind; the history
  query.
- `internal/db/queries/assets.sql` — `ListRecentImageAssets`.
- `internal/db/sqlc/*` — regenerated.
- `internal/mediagen/job.go` — `Surface`, `Kind` on `Job`, `JobAudit.LastFrameAssetID`.
- `internal/mediagen/store.go`, `internal/mediagen/store_studio.go` — validation, `ListStudio`,
  `InsertImage`.
- `internal/mediagen/types.go` — `VideoInput.LastFrameAssetID`, `VideoInput.Seed`,
  `Model.Name/Description/Seed`.
- `internal/mediagen/clamp.go` — end frame and seed.
- `internal/mediagen/catalog.go`, `catalog_cache.go`, `catalog_price.go` — name, description,
  seed; `Catalog.Entry`; `VideoSecondPrice`.
- `internal/mediagen/client_video.go` — `VideoRequest.Seed`.
- `internal/mediagen/submit.go`, `internal/mediagen/generate_image.go` — the two shared paths.
- `internal/assets/{store,service}.go` — `ListRecentImages`, `FinalizeUnprocessed`.
- `internal/agent/tools/{video_generate,video_generate_collect,image_generate,media_delivery}.go`
  — use the shared paths; `last_frame_asset_id`.
- `internal/skills/embed/media-generation-aura/SKILL.md` — one line for the end frame.
- `internal/agui/server_seams.go` (new), `server.go`, `studio_api.go`, `studio_dto.go`,
  `idempotency_http.go`.
- `cmd/aura/{serve_media,serve_studio,serve_webui_studio,serve_webui,serve.go}`.

**Web**
- `web/src/components/ui/{toggle-group,toggle,switch,slider,dropdown-menu,kbd}.tsx` — registry.
- `web/src/shell/modes.ts`, `ModeTabBar.tsx`, `MobileAppSidebar.tsx`, `web/src/AppShell.tsx`.
- `web/src/i18n/resources.studio.ts`, `resources.ts`.
- `web/src/studio/studioApi.ts`, `studioForm.ts`, `useStudio.ts`, `frameUpload.ts`,
  `modelChoice.ts`.
- `web/src/studio/StudioWorkspace.tsx`, `StudioBar.tsx`, `ImageTile.tsx`, `OptionsPopover.tsx`,
  `AdvancedPopover.tsx`, `RatioTiles.tsx`, `StudioStage.tsx`, `StudioHistory.tsx`.
- `web/src/studio/__tests__/*`.
- `web/e2e/studio-live.spec.ts`.

---

### Task 1: Studio rows in `aura.media_job`

**Files:**
- Create: `internal/db/migrations/0129_media_job_studio.{up,down}.sql`
- Modify: `internal/db/queries/media_jobs.sql`, `internal/db/db_unit_test.go`,
  `internal/mediagen/job.go`, `internal/mediagen/store.go`
- Create: `internal/mediagen/store_studio.go`
- Test: `internal/mediagen/store_test.go`, `internal/mediagen/store_integration_test.go`

**Interfaces:**
- Produces:
  - `type Surface string` with `SurfaceChat`/`SurfaceStudio`;
  - `Job.Surface Surface` and `Job.Kind Kind` (the existing `Kind`, `KindImage`/`KindVideo`);
  - `func (s *Store) ListStudio(ctx context.Context, ownerID, beforeID string, kind Kind, limit int) ([]Job, error)`;
  - `func (s *Store) InsertImage(ctx context.Context, job Job) (Job, error)`;
  - `const StudioPageMax = 48`.

- [x] **Step 1: Measure.** `ls internal/db/migrations/ | tail -1` must print
  `0128_media_job.up.sql`. Re-run the read-only live count and expect `0`:

  ```bash
  MSYS_NO_PATHCONV=1 wsl -e bash -lc 'docker exec aura-postgres sh -c "psql -U \"\$POSTGRES_USER\" -d \"\${POSTGRES_DB:-aura}\" -At -c \"SELECT count(*) FILTER (WHERE conversation_id = '"''"' OR tool_call_id = '"''"') FROM aura.media_job\""'
  ```

- [x] **Step 2: Write the migration.** `0129_media_job_studio.up.sql`:

  ```sql
  -- Studio generations (Studio plan, Task 1). Number measured with
  -- `ls internal/db/migrations/ | tail -1` at landing time (0128_media_job was the head).
  --
  -- The cockpit Studio generates without an agent turn, so its rows belong to no conversation
  -- and no tool call, and it generates images as well as video. surface says which surface
  -- asked, kind says what was made, and the two CHECKs keep the shapes apart: a chat job
  -- without its conversation could never be delivered, a Studio row with one would be offered
  -- to that conversation's recovery, and an image is generated synchronously, so its row is
  -- only ever written finished — the watcher, which reads active and completed-undelivered
  -- rows, must never find one to poll.
  --
  -- The column is surface, not origin: a job's origin is already the submission base URL its
  -- request records (mediagen.JobAudit.Origin). An image has no provider job, so its
  -- provider_job_id is a locally minted image-<uuid>; the UNIQUE index then still stops a
  -- replayed insert from recording one paid generation twice.
  --
  -- Measured before landing on 2026-09-17: 6 live rows, none with an empty conversation or
  -- tool call.

  ALTER TABLE aura.media_job
    ADD COLUMN surface text NOT NULL DEFAULT 'chat' CHECK (surface IN ('chat', 'studio')),
    ADD COLUMN kind    text NOT NULL DEFAULT 'video' CHECK (kind IN ('image', 'video'));

  ALTER TABLE aura.media_job ADD CONSTRAINT media_job_surface_scope CHECK (
    (surface = 'chat'   AND conversation_id <> '' AND tool_call_id <> '') OR
    (surface = 'studio' AND conversation_id =  '' AND tool_call_id =  ''));

  ALTER TABLE aura.media_job ADD CONSTRAINT media_job_image_is_finished CHECK (
    kind = 'video' OR (status = 'completed' AND delivered_at IS NOT NULL));

  -- Backs the Studio history (ListStudioMediaJobs), newest first.
  CREATE INDEX media_job_studio_idx ON aura.media_job (identity_id, created_at DESC, id DESC)
    WHERE surface = 'studio';

  COMMENT ON COLUMN aura.media_job.surface IS
      'Which surface asked for the generation (migration 0129): chat (an agent tool call in a conversation) or studio (the cockpit Studio, no conversation, delivered on completion).';
  COMMENT ON COLUMN aura.media_job.kind IS
      'What was generated (migration 0129): video (a provider job the watcher supervises) or image (a synchronous generation, recorded already completed and delivered).';
  ```

  `0129_media_job_studio.down.sql`:

  ```sql
  DROP INDEX IF EXISTS aura.media_job_studio_idx;
  ALTER TABLE aura.media_job DROP CONSTRAINT IF EXISTS media_job_image_is_finished;
  ALTER TABLE aura.media_job DROP CONSTRAINT IF EXISTS media_job_surface_scope;
  ALTER TABLE aura.media_job DROP COLUMN IF EXISTS kind;
  ALTER TABLE aura.media_job DROP COLUMN IF EXISTS surface;
  ```

- [x] **Step 3: Update the queries** in `internal/db/queries/media_jobs.sql`:
  - **`InsertMediaJob`** gains `surface` and `kind` (`$9`, `$10`).
  - **New, for a finished image:**

    ```sql
    -- name: InsertCompletedMediaJob :one
    -- One synchronous image generation, recorded finished. The asset must be the owner's
    -- accepted, undeleted agent image with no thread: the Studio's own result, never another
    -- identity's or a chat delivery.
    INSERT INTO aura.media_job (
        identity_id, conversation_id, tool_call_id, provider_job_id, model, request,
        status, cost_usd, surface, kind, asset_id, completed_at, delivered_at
    )
    SELECT sqlc.arg(identity_id), '', '', sqlc.arg(provider_job_id), sqlc.arg(model),
           sqlc.arg(request), 'completed', sqlc.narg(cost_usd)::numeric, 'studio', 'image',
           assets.id, now(), now()
    FROM aura.assets
    WHERE assets.id = sqlc.arg(asset_id)
      AND assets.identity_id = sqlc.arg(identity_id)
      AND assets.thread_id = ''
      AND assets.source_kind = 'agent'
      AND assets.modality = 'image'
      AND assets.status = 'accepted'
      AND assets.deleted_at IS NULL
    RETURNING *;
    ```

  - **`CompleteMediaJob`** gains, after `completed_at = now(),`:

    ```sql
        delivered_at = CASE WHEN media_job.surface = 'studio' THEN now() ELSE media_job.delivered_at END,
    ```

    and its asset guard's `assets.modality = 'video'` becomes
    `assets.modality = media_job.kind`.
  - **New history query:**

    ```sql
    -- name: ListStudioMediaJobs :many
    -- One history page, newest first, optionally of one kind. before_id is the last row of the
    -- previous page; an id the owner does not hold compares as NULL and yields an empty page.
    SELECT * FROM aura.media_job
    WHERE media_job.identity_id = sqlc.arg(identity_id)
      AND media_job.surface = 'studio'
      AND (sqlc.narg(kind)::text IS NULL OR media_job.kind = sqlc.narg(kind)::text)
      AND (sqlc.narg(before_id)::uuid IS NULL OR (media_job.created_at, media_job.id) < (
          SELECT b.created_at, b.id FROM aura.media_job b
          WHERE b.id = sqlc.narg(before_id)::uuid AND b.identity_id = sqlc.arg(identity_id)))
    ORDER BY media_job.created_at DESC, media_job.id DESC
    LIMIT sqlc.arg(row_limit);
    ```

  Run `sqlc generate` (`/c/Users/chett/go/bin/sqlc`) from the repo root.

- [x] **Step 4: Pin the head** in `internal/db/db_unit_test.go`: append to the comment
  ` 0129 adds media_job.surface and media_job.kind, so the cockpit Studio's conversationless
  image and video rows are told apart from chat jobs.`, and change `128` to `129` in the
  condition and the message.

- [x] **Step 5: Write the failing unit test** in `internal/mediagen/store_test.go`:

  ```go
  func TestValidateNewJobSurfaceScope(t *testing.T) {
  	request, err := JobRequest(VideoRequest{Model: "m", Prompt: "p"}, JobAudit{Origin: "https://openrouter.ai/api/v1"})
  	if err != nil {
  		t.Fatal(err)
  	}
  	base := Job{Model: "m", Request: request, Status: StatusPending, ProviderJobID: "gen-1", Kind: KindVideo}
  	cases := []struct {
  		name    string
  		mutate  func(*Job)
  		wantErr bool
  	}{
  		{"chat with conversation and call", func(j *Job) { j.Surface, j.ConversationID, j.ToolCallID = SurfaceChat, "c", "t" }, false},
  		{"chat without conversation", func(j *Job) { j.Surface, j.ToolCallID = SurfaceChat, "t" }, true},
  		{"studio without conversation", func(j *Job) { j.Surface = SurfaceStudio }, false},
  		{"studio with conversation", func(j *Job) { j.Surface, j.ConversationID = SurfaceStudio, "c" }, true},
  		{"unknown surface", func(j *Job) { j.Surface, j.ConversationID, j.ToolCallID = "api", "c", "t" }, true},
  		{"image is not submitted as a job", func(j *Job) { j.Surface, j.Kind = SurfaceStudio, KindImage }, true},
  	}
  	for _, tc := range cases {
  		t.Run(tc.name, func(t *testing.T) {
  			job := base
  			tc.mutate(&job)
  			if err := validateNewJob(job); (err != nil) != tc.wantErr {
  				t.Fatalf("validateNewJob err = %v, wantErr %v", err, tc.wantErr)
  			}
  		})
  	}
  }
  ```

  Run `go test ./internal/mediagen/ -run TestValidateNewJobSurfaceScope`: FAIL, undefined.

- [x] **Step 6: Implement.**
  - **`job.go`:** add above `Job`:

    ```go
    // Surface says which surface asked for a generation. A chat job was asked for by an agent
    // tool call and is delivered into its conversation; a Studio row has no conversation and is
    // delivered when it finishes.
    type Surface string

    // The two surfaces aura.media_job.surface admits (migration 0129).
    const (
    	SurfaceChat   Surface = "chat"
    	SurfaceStudio Surface = "studio"
    )
    ```

    `Job` gains `Surface Surface` and `Kind Kind` after `IdentityID`, and its doc says an image
    row is written finished. `JobAudit` gains
    `LastFrameAssetID string `json:"last_frame_asset_id,omitempty"``.
  - **`store.go`:**
    - `validateNewJob` becomes:

      ```go
      func validateNewJob(job Job) error {
      	switch {
      	case job.Kind != KindVideo:
      		return fmt.Errorf("mediagen: only a video job is submitted and supervised, not %q", job.Kind)
      	case !job.Status.active():
      		return fmt.Errorf("mediagen: a new job must be pending or in_progress, not %q", job.Status)
      	case job.Model == "":
      		return errors.New("mediagen: a new job needs its model")
      	case job.Surface == SurfaceChat && (job.ConversationID == "" || job.ToolCallID == ""):
      		return errors.New("mediagen: a chat job needs its conversation and tool call")
      	case job.Surface == SurfaceStudio && (job.ConversationID != "" || job.ToolCallID != ""):
      		return errors.New("mediagen: a Studio job belongs to no conversation or tool call")
      	case job.Surface != SurfaceChat && job.Surface != SurfaceStudio:
      		return fmt.Errorf("mediagen: unknown job surface %q", job.Surface)
      	}
      	if _, err := job.Audit(); err != nil {
      		return fmt.Errorf("mediagen: a new job needs a request built by JobRequest, or no resume can check its origin: %w", err)
      	}
      	_, err := validProviderID(job.ProviderJobID)
      	return err
      }
      ```

    - `Insert` passes `Surface: string(job.Surface)` and `Kind: string(job.Kind)`.
    - `jobFromRow` sets `Surface: Surface(row.Surface), Kind: Kind(row.Kind)`.
    - Extract `withJobs` (the listing twin of `withJob`) and have `Recoverable` use it:

      ```go
      // withJobs runs one owned listing in the owner's identity transaction and maps every row
      // inside it, so a row that cannot be mapped rolls the statement back.
      func (s *Store) withJobs(ctx context.Context, ownerID string, run func(*sqlc.Queries) ([]sqlc.AuraMediaJob, error)) ([]Job, error) {
      	var jobs []Job
      	err := db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
      		rows, err := run(q)
      		if err != nil {
      			return err
      		}
      		jobs = make([]Job, 0, len(rows))
      		for _, row := range rows {
      			job, err := jobFromRow(row)
      			if err != nil {
      				return err
      			}
      			jobs = append(jobs, job)
      		}
      		return nil
      	})
      	if err != nil {
      		return nil, err
      	}
      	return jobs, nil
      }
      ```

    - `Complete`'s doc gains: "A Studio row is marked delivered in the same statement: there is
      no conversation to claim it."
  - **`store_studio.go`:**

    ```go
    package mediagen

    import (
    	"context"
    	"errors"
    	"fmt"

    	"github.com/jackc/pgx/v5"
    	"github.com/jackc/pgx/v5/pgtype"

    	"github.com/chetto1983/aura/internal/db"
    	"github.com/chetto1983/aura/internal/db/sqlc"
    	"github.com/chetto1983/aura/internal/pgnumeric"
    )

    // StudioPageMax bounds one Studio history page.
    const StudioPageMax = 48

    // ListStudio returns one page of the owner's Studio rows, newest first, of kind when it is
    // not empty. beforeID is the last row of the previous page, empty for the first; a
    // malformed one names no row and yields an empty page. limit is clamped to [1, StudioPageMax].
    func (s *Store) ListStudio(ctx context.Context, ownerID, beforeID string, kind Kind, limit int) ([]Job, error) {
    	owner, err := db.ParseUUID("owner id", ownerID)
    	if err != nil {
    		return nil, err
    	}
    	var before pgtype.UUID
    	if beforeID != "" {
    		if before, err = db.ParseUUID("before id", beforeID); err != nil {
    			return []Job{}, nil
    		}
    	}
    	limit = min(max(limit, 1), StudioPageMax)
    	filter := pgtype.Text{}
    	if kind != "" {
    		filter = pgtype.Text{String: string(kind), Valid: true}
    	}
    	return s.withJobs(ctx, ownerID, func(q *sqlc.Queries) ([]sqlc.AuraMediaJob, error) {
    		return q.ListStudioMediaJobs(ctx, sqlc.ListStudioMediaJobsParams{
    			IdentityID: owner, Kind: filter, BeforeID: before,
    			RowLimit: int32(limit), //nolint:gosec // clamped to StudioPageMax above.
    		})
    	})
    }

    // InsertImage records one synchronous image generation, already completed and delivered: an
    // image is paid for and produced in a single call, so there is nothing to supervise. The
    // asset must be the owner's accepted agent image with no thread, the Studio's own result;
    // any other asset matches nothing and the row is refused rather than written unbacked.
    func (s *Store) InsertImage(ctx context.Context, job Job) (Job, error) {
    	if err := validateImageRecord(job); err != nil {
    		return Job{}, err
    	}
    	owner, err := db.ParseUUID("owner id", job.IdentityID)
    	if err != nil {
    		return Job{}, err
    	}
    	asset, err := db.ParseUUID("asset id", job.AssetID)
    	if err != nil {
    		return Job{}, err
    	}
    	cost, err := pgnumeric.NullableFromFloat(job.CostUSD)
    	if err != nil {
    		return Job{}, err
    	}
    	row, err := s.withJob(ctx, job.IdentityID, func(q *sqlc.Queries) (sqlc.AuraMediaJob, error) {
    		out, err := q.InsertCompletedMediaJob(ctx, sqlc.InsertCompletedMediaJobParams{
    			IdentityID: owner, ProviderJobID: job.ProviderJobID, Model: job.Model,
    			Request: job.Request, CostUsd: cost, AssetID: asset,
    		})
    		if errors.Is(err, pgx.ErrNoRows) {
    			return out, &Error{Code: "asset_not_found", Message: "The generated image is not available."}
    		}
    		return out, err
    	})
    	if err != nil {
    		return Job{}, err
    	}
    	return row, nil
    }

    func validateImageRecord(job Job) error {
    	switch {
    	case job.Kind != KindImage:
    		return fmt.Errorf("mediagen: InsertImage records an image, not %q", job.Kind)
    	case job.Surface != SurfaceStudio:
    		return errors.New("mediagen: only the Studio records a finished generation")
    	case job.Model == "" || job.AssetID == "":
    		return errors.New("mediagen: an image record needs its model and asset")
    	}
    	_, err := validProviderID(job.ProviderJobID)
    	return err
    }
    ```

    Match the generated param names in `internal/db/sqlc/media_jobs.sql.go` (`Kind`, `BeforeID`,
    `RowLimit`, `CostUsd`, `AssetID`) after `sqlc generate`.
  - **Fixtures.** Every test that builds a `Job` for `Insert` (`grep -rn "ProviderJobID:"
    internal --include=*_test.go`) sets `Surface` and `Kind`. The tool's `record` (Task 3 moves
    it) sets `Surface: mediagen.SurfaceChat, Kind: mediagen.KindVideo`.

- [x] **Step 7: Write the failing integration tests** in
  `internal/mediagen/store_integration_test.go` (`//go:build db_integration`), reusing the
  file's helpers:
  - `TestStoreStudioJobRoundTrip` — a Studio video job with no conversation reads back with
    `Surface == SurfaceStudio` and `Kind == KindVideo`.
  - `TestStoreRejectsChatJobWithoutConversation` — a raw `q.InsertMediaJob` with
    `Surface: "chat"` and an empty conversation fails with check violation `23514`.
  - `TestStoreCompleteMarksStudioDelivered` — completing a Studio job with an accepted agent
    video asset (`thread_id = ''`) sets `DeliveredAt`, and `Recoverable` does not list it; a
    chat job completed the same way keeps `DeliveredAt == nil`.
  - `TestStoreInsertImageRecordsAFinishedGeneration` — an accepted agent image asset with no
    thread produces a completed, delivered row of kind image; a video asset, another
    identity's asset and a thread-scoped asset each fail with `asset_not_found`; the watcher's
    `Recoverable` never lists it.
  - `TestStoreListStudioPages` — three Studio video rows, one image row, one chat job, and
    another identity's Studio row: the first page is newest-first and excludes the chat and
    foreign rows; `before` walks to the next page; `kind=image` returns only the image row; a
    malformed `before` returns an empty page.

  Run them on the disposable database (read `scripts/coverage_docker.sh`'s header for the exact
  invocation; it provisions and drops only `aura_cov`):

  ```bash
  bash scripts/coverage_docker.sh
  ```

  or, with the `aura_cov` DSNs exported as that script does,
  `go test -tags db_integration -run TestStore ./internal/mediagen/`.

- [x] **Step 8: Verify.** `go vet ./... && go build ./... && go test ./internal/mediagen/ ./internal/agent/tools/ ./internal/db/`, then `go test -race ./internal/mediagen/` in WSL.

- [x] **Step 9: Commit.**
  `git commit -m "feat(mediagen): record the Studio's image and video generations"`, with the
  body explaining the conversationless rows, the finished-image rule, the minted
  `image-<uuid>` and the measured live rows.

---

### Task 2: Studio inputs and results in `assets`

**Files:**
- Modify: `internal/db/queries/assets.sql`, `internal/assets/store.go`,
  `internal/assets/service.go`
- Test: the file holding the `Finalize` unit tests (`grep -rln "Finalize(" internal/assets/*_test.go`),
  `internal/assets/store_integration_test.go`

**Interfaces:**
- Produces:
  - `func (s *Service) ListRecentImages(ctx context.Context, identityID string, limit int) ([]Asset, error)`;
  - `func (s *Service) FinalizeUnprocessed(ctx context.Context, identityID, assetID string, modality Modality) (Asset, error)`;
  - `var ErrWrongModality = errors.New("assets: the asset is not of the expected modality")`.

- [x] **Step 1: Add the query** after `ListAssetsForLibrary`:

  ```sql
  -- name: ListRecentImageAssets :many
  -- The images an identity can pick as a Studio frame or reference: usable (the statuses the
  -- cockpit's isReadyAsset accepts) and not deleted, newest first, from any thread or none.
  SELECT * FROM aura.assets
  WHERE identity_id = $1
    AND modality = 'image'
    AND status IN ('accepted', 'processing', 'searchable', 'embedding', 'complete')
    AND deleted_at IS NULL
  ORDER BY created_at DESC
  LIMIT $2;
  ```

  Run `sqlc generate`.

- [x] **Step 2: Write the failing unit tests** beside the existing `Finalize` tests, reusing
  their fakes:
  - `TestFinalizeUnprocessedAcceptsWithoutEnqueueing` — status `accepted`, zero processing
    enqueues.
  - `TestFinalizeUnprocessedRefusesAnotherModality` — a document asset answers
    `ErrWrongModality`, and the object store is never read.

  Run `go test ./internal/assets/ -run FinalizeUnprocessed`: FAIL, undefined.

- [x] **Step 3: Implement.** In `service.go`, split `Finalize` into `accept` plus the enqueue,
  add the modality check, and add both new methods:

  ```go
  // ErrWrongModality refuses an asset finalized for a use that needs another modality.
  var ErrWrongModality = errors.New("assets: the asset is not of the expected modality")

  // FinalizeUnprocessed accepts an upload that is the input of one generation rather than
  // knowledge: the same checks as Finalize, and no processing, so no vision summary is paid for
  // and nothing is filed for the index. The asset must be of modality.
  func (s *Service) FinalizeUnprocessed(ctx context.Context, identityID, assetID string, modality Modality) (Asset, error) {
  	return s.accept(ctx, identityID, assetID, modality)
  }

  // recentImagesMax bounds the Studio picker.
  const recentImagesMax = 48

  // ListRecentImages lists the identity's usable images, newest first, for the Studio picker.
  func (s *Service) ListRecentImages(ctx context.Context, identityID string, limit int) ([]Asset, error) {
  	if s.Store == nil {
  		return nil, fmt.Errorf("asset service is not configured")
  	}
  	return s.Store.ListRecentImages(ctx, identityID, min(max(limit, 1), recentImagesMax))
  }
  ```

  `accept` is the old `Finalize` body up to `MarkAccepted`, with
  `if modality != "" && asset.Modality != modality { return Asset{}, ErrWrongModality }` right
  after `GetForIdentity`. `Finalize` calls `accept(ctx, identityID, assetID, "")` and keeps the
  enqueue and its failure branch. `store.go` gains `ListRecentImages`, mirroring
  `ListForLibrary`.

- [x] **Step 4: Add the integration test** `TestStoreListRecentImages`: an accepted image, a
  failed image, a deleted image, an accepted document and another identity's image — only the
  first is listed. Run it on `aura_cov`.

- [x] **Step 5: Verify.** `go test ./internal/assets/` and `-race` in WSL; the existing
  `Finalize` tests pass unchanged.

- [x] **Step 6: Commit.**
  `git commit -m "feat(assets): accept a Studio input image without processing it"`.

---

### Task 3: Shared generation paths

**Files:**
- Modify: `internal/mediagen/types.go`, `clamp.go`, `catalog.go`, `catalog_cache.go`,
  `catalog_price.go`, `client_video.go`
- Create: `internal/mediagen/submit.go`, `internal/mediagen/generate_image.go`
- Modify: `internal/agent/tools/video_generate.go`, `video_generate_collect.go`,
  `image_generate.go`, `media_delivery.go`; `cmd/aura/serve_media.go`;
  `internal/skills/embed/media-generation-aura/SKILL.md`
- Test: `internal/mediagen/{catalog_price_test.go,clamp_test.go,catalog_test.go,submit_test.go,generate_image_test.go}`,
  `internal/agent/tools/*_test.go`

**Interfaces:**
- Consumes: Task 1's `Surface`, `Job.Kind`, `JobAudit.LastFrameAssetID`.
- Produces:

  ```go
  func VideoSecondPrice(skus map[string]string, resolution string, audio bool) (float64, bool)
  func (c *Catalog) Entry(ctx context.Context, baseURL string, kind Kind, model string) (*Model, []string, error)
  // Model gains: Name, Description string; Seed bool
  // VideoInput gains: LastFrameAssetID string; Seed *int
  // VideoRequest gains: Seed *int `json:"seed,omitempty"`

  type VideoSubmission struct {
  	Owner, ConversationID, ToolCallID, Model string
  	Surface Surface
  	Input   VideoInput
  }
  type VideoSubmitter struct {
  	Credentials MediaCredentials; Catalog *Catalog; Client *Client
  	References ReferenceReader; Jobs JobStore; MaxImageBytes int64
  }
  func (s *VideoSubmitter) Configured() bool
  func (s *VideoSubmitter) Submit(ctx context.Context, sub VideoSubmission) (Job, error)

  type ImageGeneration struct {
  	Owner, Model string
  	Input        ImageInput
  }
  type GeneratedImage struct {
  	Result      ImageResult
  	Prompt      string
  	Used        ImageInput
  	Adjustments []string
  }
  type ImageGenerator struct {
  	Credentials MediaCredentials; Catalog *Catalog; Client *Client
  	References ReferenceReader; MaxImageBytes int64
  }
  func (g *ImageGenerator) Configured() bool
  func (g *ImageGenerator) Generate(ctx context.Context, gen ImageGeneration) (GeneratedImage, error)
  ```

- [x] **Step 1: Write the failing price test** in `catalog_price_test.go`:

  ```go
  func TestVideoSecondPrice(t *testing.T) {
  	veoLite := map[string]string{ // google/veo-3.1-lite, live catalog 2026-09-17
  		"duration_seconds_with_audio":         "0.08",
  		"duration_seconds_without_audio":      "0.05",
  		"duration_seconds_with_audio_720p":    "0.05",
  		"duration_seconds_without_audio_720p": "0.03",
  	}
  	cases := []struct {
  		name       string
  		skus       map[string]string
  		resolution string
  		audio      bool
  		want       float64
  		ok         bool
  	}{
  		{"resolution and audio SKU", veoLite, "720p", false, 0.03, true},
  		{"resolution with audio", veoLite, "720p", true, 0.05, true},
  		{"unpriced resolution falls back to the audio SKU", veoLite, "1080p", true, 0.08, true},
  		{"no resolution given", veoLite, "", false, 0.05, true},
  		{"plain duration SKU", map[string]string{"duration_seconds": "0.1"}, "720p", true, 0.1, true},
  		{"resolution-only SKU", map[string]string{"duration_seconds_720p": "0.2", "duration_seconds": "0.4"}, "720P", false, 0.2, true},
  		{"cents per second", map[string]string{"cents_per_second_with_audio": "7"}, "", true, 0.07, true},
  		{"unparseable value is skipped", map[string]string{"duration_seconds_720p": "x", "duration_seconds": "0.4"}, "720p", false, 0.4, true},
  		{"negative value is skipped", map[string]string{"duration_seconds": "-1"}, "", false, 0, false},
  		{"no per-second SKU", map[string]string{"per_generation": "1"}, "720p", false, 0, false},
  	}
  	for _, tc := range cases {
  		t.Run(tc.name, func(t *testing.T) {
  			got, ok := VideoSecondPrice(tc.skus, tc.resolution, tc.audio)
  			if ok != tc.ok || math.Abs(got-tc.want) > 1e-12 {
  				t.Fatalf("VideoSecondPrice = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
  			}
  		})
  	}
  }
  ```

- [x] **Step 2: Implement `VideoSecondPrice`** in `catalog_price.go`:

  ```go
  // VideoSecondPrice returns the USD per second of a clip at this resolution and audio choice,
  // from the most specific SKU the model declares: resolution and audio, then audio, then
  // resolution, then the plain rate; dollars before cents. The naming was checked against the
  // costs measured for google/veo-3.1-lite on 2026-09-17, so a model that names its SKUs
  // otherwise reports ok=false rather than a guess.
  func VideoSecondPrice(skus map[string]string, resolution string, audio bool) (float64, bool) {
  	sound := "_without_audio"
  	if audio {
  		sound = "_with_audio"
  	}
  	suffixes := []string{sound, ""}
  	if res := strings.ToLower(strings.TrimSpace(resolution)); res != "" {
  		suffixes = []string{sound + "_" + res, sound, "_" + res, ""}
  	}
  	for _, unit := range []struct {
  		prefix  string
  		divisor float64
  	}{{"duration_seconds", 1}, {"cents_per_second", 100}} {
  		for _, suffix := range suffixes {
  			raw, declared := skus[unit.prefix+suffix]
  			if !declared {
  				continue
  			}
  			value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
  			if err != nil || !(value >= 0) || math.IsInf(value, 1) {
  				continue
  			}
  			return value / unit.divisor, true
  		}
  	}
  	return 0, false
  }
  ```

- [x] **Step 3: Catalog name, description and seed.** Failing test in `catalog_test.go`:
  a `videos/models` payload with `"name": "Google: Veo 3.1 Lite"`, `"description": "…"`,
  `"seed": true` and an `images/models` payload with a name and description produce `Model`s
  carrying them; a payload without them leaves the fields empty and `Seed` false.

  Implementation in `catalog.go`: `imageModelRow` and `videoModelRow` gain
  `Name string `json:"name"`` and `Description string `json:"description"``, `videoModelRow`
  gains `Seed bool `json:"seed"``, and both constructors copy them into `Model`, whose new
  fields are documented as "as the provider names it; empty when it declares none".

- [x] **Step 4: End frame and seed in the clamp.** Failing tests in `clamp_test.go`:

  ```go
  func TestClampVideoLastFrame(t *testing.T) { /* unsupported when the model lists only first_frame; kept when it lists both; passed through for a nil model */ }
  func TestClampVideoSeed(t *testing.T) { /* dropped with a note when Model.Seed is false; kept when true; passed through for a nil model */ }
  ```

  Implementation in `types.go` (`VideoInput` gains `LastFrameAssetID string` and `Seed *int`),
  `client_video.go` (`VideoRequest` gains `Seed *int `json:"seed,omitempty"``) and `clamp.go`:

  ```go
  	if in.LastFrameAssetID != "" && !slices.Contains(m.FrameImages, "last_frame") {
  		return VideoInput{}, nil, &Error{Code: "unsupported", Message: "The selected video model cannot end on a given image. " +
  			"Nothing was generated. Remove the end frame, or choose a video model that accepts one."}
  	}
  ```

  and, beside the audio note:

  ```go
  	if in.Seed != nil && !m.Seed {
  		out.Seed = nil
  		notes.add("seed is not supported by this model; omitted")
  	}
  ```

  `ClampVideo` copies `Seed` into `out` the way it copies `Audio` (a fresh pointer).

- [x] **Step 5: Move the catalog lookup.** Add `Catalog.Entry` (and the
  `uncheckedOptionsNote` constant) to `catalog_cache.go` with the doc `mediaCatalogEntry` has
  today, delete `mediaCatalogEntry` and the constant from `internal/agent/tools/media_delivery.go`,
  and move the tools test that pinned it into `internal/mediagen` unchanged in substance.

- [x] **Step 6: The two shared paths.** Failing tests first:
  - `submit_test.go`: a Studio submission records `Surface`, `Kind`, the empty conversation, a
    `first_frame` plus `last_frame` body, the seed, and the audit's end frame; a refusal
    before the POST leaves zero submits; an `Insert` failure answers coded `job_failed` after
    exactly one POST; `Configured()` is false for a zero value and a nil pointer.
  - `generate_image_test.go`: a generation returns the bytes, the MIME, the cost, the clamped
    input and the adjustments; a reference beyond the model's maximum refuses before the paid
    call; an unreadable catalog still generates and carries the unchecked-options note.

  Then write `submit.go` exactly as in the Interfaces block:

  ```go
  // Submit charges exactly once. Credentials, the catalog entry, the clamp, the frames and the
  // persisted request are settled before the one POST, which is never repeated; the accepted
  // job is then recorded.
  func (s *VideoSubmitter) Submit(ctx context.Context, sub VideoSubmission) (Job, error) {
  	baseURL, apiKey, err := s.Credentials.For(ctx, sub.Owner)
  	if err != nil {
  		return Job{}, err
  	}
  	entry, adjustments, err := s.Catalog.Entry(ctx, baseURL, KindVideo, sub.Model)
  	if err != nil {
  		return Job{}, err
  	}
  	input, notes, err := ClampVideo(sub.Input, entry)
  	if err != nil {
  		return Job{}, err
  	}
  	adjustments = append(adjustments, notes...)
  	req, err := s.request(ctx, sub.Owner, sub.Model, input)
  	if err != nil {
  		return Job{}, err
  	}
  	persisted, err := JobRequest(req, JobAudit{
  		Origin: baseURL, FirstFrameAssetID: input.FirstFrameAssetID, LastFrameAssetID: input.LastFrameAssetID,
  		ReferenceAssetIDs: input.ReferenceAssetIDs, Adjustments: adjustments,
  	})
  	if err != nil {
  		return Job{}, err
  	}
  	remote, err := s.Client.SubmitVideo(ctx, baseURL, apiKey, req)
  	if err != nil {
  		return Job{}, err
  	}
  	return s.record(ctx, sub, persisted, remote)
  }

  // request reads the kept frames and references and builds the provider body.
  func (s *VideoSubmitter) request(ctx context.Context, owner, model string, input VideoInput) (VideoRequest, error) {
  	req := VideoRequest{
  		Model: model, Prompt: input.Prompt, Duration: input.Duration, Resolution: input.Resolution,
  		AspectRatio: input.AspectRatio, GenerateAudio: input.Audio, Seed: input.Seed,
  	}
  	for _, frame := range []struct{ assetID, frameType string }{
  		{input.FirstFrameAssetID, "first_frame"},
  		{input.LastFrameAssetID, "last_frame"},
  	} {
  		if frame.assetID == "" {
  			continue
  		}
  		image, err := LoadReferences(ctx, s.References, owner, []string{frame.assetID}, s.MaxImageBytes)
  		if err != nil {
  			return VideoRequest{}, err
  		}
  		req.FrameImages = append(req.FrameImages, FrameReference{Type: image[0].Type, ImageURL: image[0].ImageURL, FrameType: frame.frameType})
  	}
  	references, err := LoadReferences(ctx, s.References, owner, input.ReferenceAssetIDs, s.MaxImageBytes)
  	if err != nil {
  		return VideoRequest{}, err
  	}
  	req.InputReferences = references
  	return req, nil
  }

  // record persists the accepted job. Only pending and in_progress rows exist before the
  // watcher has looked, so any answer but pending is stored in_progress. A lost Insert is the
  // excluded submission interval: logged for reconciliation, never submitted again.
  func (s *VideoSubmitter) record(ctx context.Context, sub VideoSubmission, request json.RawMessage, remote RemoteVideo) (Job, error) {
  	status := StatusInProgress
  	if remote.Status == StatusPending {
  		status = StatusPending
  	}
  	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), videoJobRecordTimeout)
  	defer cancel()
  	job, err := s.Jobs.Insert(recordCtx, Job{
  		IdentityID: sub.Owner, Surface: sub.Surface, Kind: KindVideo, ConversationID: sub.ConversationID,
  		ToolCallID: sub.ToolCallID, ProviderJobID: remote.ID, Model: sub.Model, Request: request,
  		Status: status, CostUSD: remote.CostUSD,
  	})
  	if err != nil {
  		slog.Error("mediagen: the provider accepted a video job that could not be recorded; it is not submitted again",
  			"owner", sub.Owner, "surface", sub.Surface, "provider_job_id", remote.ID, "err", redact.String(err.Error()))
  		return Job{}, &Error{Code: "job_failed", Message: "The video was submitted but could not be recorded, so it cannot be delivered. It was not submitted again.", cause: err}
  	}
  	return job, nil
  }
  ```

  with `const videoJobRecordTimeout = 10 * time.Second` and its comment moved from the tool.

  And `generate_image.go`:

  ```go
  package mediagen

  import "context"

  // ImageGeneration is one image to generate: whose it is, with which model, and what was asked.
  type ImageGeneration struct {
  	Owner string
  	Model string
  	Input ImageInput
  }

  // GeneratedImage is one paid image and what produced it: the clamped input as the provider
  // received it, and the notes the clamp made.
  type GeneratedImage struct {
  	Result      ImageResult
  	Prompt      string
  	Used        ImageInput
  	Adjustments []string
  }

  // ImageGenerator is the one path that pays for an image: the image_generate tool and the
  // cockpit Studio both generate through it. Unlike video there is no job — the call returns
  // the bytes — so the caller stores them.
  type ImageGenerator struct {
  	Credentials   MediaCredentials
  	Catalog       *Catalog
  	Client        *Client
  	References    ReferenceReader
  	MaxImageBytes int64
  }

  // Configured reports whether every dependency is present; a caller refuses before any paid
  // request otherwise.
  func (g *ImageGenerator) Configured() bool {
  	return g != nil && g.Credentials != nil && g.Catalog != nil && g.Client != nil &&
  		g.References != nil && g.MaxImageBytes > 0
  }

  // Generate settles credentials, the catalog entry, the clamp and the references before the
  // one paid call, which is never repeated.
  func (g *ImageGenerator) Generate(ctx context.Context, gen ImageGeneration) (GeneratedImage, error) {
  	baseURL, apiKey, err := g.Credentials.For(ctx, gen.Owner)
  	if err != nil {
  		return GeneratedImage{}, err
  	}
  	entry, adjustments, err := g.Catalog.Entry(ctx, baseURL, KindImage, gen.Model)
  	if err != nil {
  		return GeneratedImage{}, err
  	}
  	input, notes, err := ClampImage(gen.Input, entry)
  	if err != nil {
  		return GeneratedImage{}, err
  	}
  	references, err := LoadReferences(ctx, g.References, gen.Owner, input.ReferenceAssetIDs, g.MaxImageBytes)
  	if err != nil {
  		return GeneratedImage{}, err
  	}
  	result, err := g.Client.GenerateImage(ctx, baseURL, apiKey, ImageRequest{
  		Model: gen.Model, Prompt: input.Prompt, AspectRatio: input.AspectRatio, References: references,
  	})
  	if err != nil {
  		return GeneratedImage{}, err
  	}
  	return GeneratedImage{Result: result, Prompt: input.Prompt, Used: input, Adjustments: append(adjustments, notes...)}, nil
  }
  ```

- [x] **Step 7: Rewire the tools.**
  - **`video_generate.go`:** fields become
    `{Submitter *mediagen.VideoSubmitter; Settings mediagen.Settings; Jobs mediagen.JobStore; Watcher *mediagen.Watcher; VideoAssets mediagen.ReferenceReader; MaxVideoBytes int64}`;
    `configured()` checks `g.Submitter.Configured()` and the rest; `clamp`, `request`, `record`
    and the timeout constant are deleted; `submit` reads the settings and calls
    `g.Submitter.Submit` with `Surface: mediagen.SurfaceChat` and the tool-call context's
    conversation and call ids. The schema gains

    ```json
    "last_frame_asset_id": {"type": "string", "description": "Asset id of an image the video should end on; needs first_frame_asset_id and a model that accepts an end frame."},
    ```

    and `Execute` refuses an end frame with no start frame (`unsupported`, before any call).
  - **`video_generate_collect.go`:** `videoGenerateUsed` gains `LastFrameAssetID` and `Seed`,
    filled from the audit and the request.
  - **`image_generate.go`:** fields become
    `{Generator *mediagen.ImageGenerator; Settings mediagen.Settings; Assets AssetDeliverer}`;
    `Execute` reads the model from settings, calls `g.Generator.Generate`, then stages,
    ingests and delivers exactly as today.
  - **`cmd/aura/serve_media.go`:** `mediaDeps` gains `submitter *mediagen.VideoSubmitter` and
    `imager *mediagen.ImageGenerator`, built in `newMediaDeps` once `credentials`, `catalog`,
    `client`, `references` and (for the submitter) `jobs` exist; `wireMediaTools` and
    `wireVideoTool` hand them over and keep their all-or-nothing posture.
  - **Tests.** Update the tool fixtures to build the tools over the shared paths. Two
    expectation changes are real and go in the commit body: the settings read now precedes the
    credential read, and the tools no longer own the clamp.
  - **New tool tests:** an end frame without a start frame, and an end frame on a model without
    `last_frame`, both refuse with no provider call.
  - **Skill.** In `internal/skills/embed/media-generation-aura/SKILL.md`, the tool-rules list
    gains: "`last_frame_asset_id` makes the clip end on an image; it needs
    `first_frame_asset_id`, and a model without end frames refuses it (nothing billed)."

- [x] **Step 8: Verify.** `go vet ./... && go build ./... && go test ./internal/mediagen/ ./internal/agent/tools/ ./cmd/aura/ ./internal/skills/...`, `-race` in WSL for `mediagen` and
  `tools`, and `bash scripts/check-file-size.sh`.

- [x] **Step 9: Commit.**
  `git commit -m "refactor(mediagen): generate every image and video through one path"`.

---

### Task 4: Studio HTTP API

**Files:**
- Create: `internal/agui/server_seams.go` (the `Set*` methods moved out of `server.go`,
  unchanged), `internal/agui/studio_api.go`, `internal/agui/studio_dto.go`,
  `cmd/aura/serve_studio.go`, `cmd/aura/serve_webui_studio.go`
- Modify: `internal/agui/server.go`, `internal/agui/idempotency_http.go`,
  `cmd/aura/serve_webui.go`, `cmd/aura/serve.go`
- Test: `internal/agui/studio_api_test.go`, `cmd/aura/serve_studio_test.go`,
  `cmd/aura/serve_webui_auth_test.go`

**Interfaces:**
- Produces:

  ```go
  type StudioBackend interface {
  	Models(ctx context.Context, kind mediagen.Kind) (defaultModel string, models []mediagen.Model, err error)
  	SubmitVideo(ctx context.Context, owner string, req StudioVideoRequest) (mediagen.Job, error)
  	GenerateImage(ctx context.Context, owner string, req StudioImageRequest) (mediagen.Job, error)
  	History(ctx context.Context, owner, beforeID string, kind mediagen.Kind, limit int) ([]mediagen.Job, error)
  	Library(ctx context.Context, owner string, limit int) ([]assets.Asset, error)
  	FinalizeUpload(ctx context.Context, owner, assetID string) (assets.Asset, error)
  }
  func (s *Server) SetStudio(backend StudioBackend)
  ```

  and the wire contract of spec §A2, which Task 5 consumes.

- [x] **Step 1: Split `server.go` first.** Move every `func (s *Server) Set…` into
  `server_seams.go` with no edits; `go build ./internal/agui/` passes and `server.go` drops
  well under 600 LOC. Commit alone:
  `refactor(agui): move the server's dependency seams to their own file`.

- [x] **Step 2: Write the failing handler tests** in `internal/agui/studio_api_test.go`, with a
  fake backend and the package's authenticated-request helper:
  - `TestStudioModelsCarryNamesAndPrices` — a video model with the veo-lite SKUs, both frame
    images, audio and seed yields four price rows (720p/false at 0.03 among them), its name,
    description, `seed: true`; an image model yields its aspect ratios, `reference_max` and its
    per-image price range; `?kind=` absent → 400; `?kind=audio` → 400.
  - `TestStudioVideoCreateAnswersTheRecord` — 201, the DTO reads the persisted request, and the
    fake received the body including `seed` and both frame ids.
  - `TestStudioImageCreateAnswersTheRecord` — 201 with `kind: "image"`, `status: "completed"`
    and the asset id.
  - `TestStudioMapsRefusals` — table over `unsupported`→422, `no_key`→409, `no_credit`→402,
    `outcome_unknown`→502, `assets.ErrWrongModality`→422, and a plain error →500 whose body
    never echoes the error text.
  - `TestStudioRejectsUnknownFields` — a body with an extra key → 400.
  - `TestStudioHistoryPassesCursorKindAndLimit` — `?before=j1&kind=image&limit=5` reaches the
    fake; no limit → 24; 500 → 48; `limit=x` → 400.
  - `TestStudioLibraryAndUploadFinalize` — the library omits object keys; finalizing a document
    → 422; finalizing an image → 200.
  - `TestStudioNeedsABackendAndAPrincipal` — nil backend → 503, no principal → 401.

- [x] **Step 3: Implement `studio_dto.go`** — request bodies, the model and record DTOs, and
  the error mapping:

  ```go
  // StudioVideoRequest is POST /api/studio/videos' body.
  type StudioVideoRequest struct {
  	Model             string `json:"model"`
  	Prompt            string `json:"prompt"`
  	Duration          int    `json:"duration"`
  	Resolution        string `json:"resolution"`
  	AspectRatio       string `json:"aspect_ratio"`
  	Audio             *bool  `json:"audio"`
  	Seed              *int   `json:"seed"`
  	FirstFrameAssetID string `json:"first_frame_asset_id"`
  	LastFrameAssetID  string `json:"last_frame_asset_id"`
  }

  // StudioImageRequest is POST /api/studio/images' body.
  type StudioImageRequest struct {
  	Model             string   `json:"model"`
  	Prompt            string   `json:"prompt"`
  	AspectRatio       string   `json:"aspect_ratio"`
  	ReferenceAssetIDs []string `json:"reference_asset_ids"`
  }
  ```

  The model DTO carries `id`, `name`, `description`, and per kind the fields of spec §A2;
  `studioModel(model, kind)` fills the video price matrix with `mediagen.VideoSecondPrice`
  over each declared resolution (or `""` when none) and each audio choice the model allows, and
  the image prices with `mediagen.ImagePrice` and `ImageTokenPricePerMillion`. The record DTO
  is `{id, kind, status, model, prompt, used, adjustments, cost_usd, asset_id, error,
  created_at, completed_at}`, read from `job.Submission()`; a request that cannot be decoded
  logs and leaves prompt, used and adjustments empty. `writeStudioError` maps
  `ErrMediaCatalogLocalRoute` → 409 `local_route`, `assets.ErrWrongModality` → 422
  `unsupported`, `pgx.ErrNoRows` → 404, a coded `*mediagen.Error` → its status from the table,
  and anything else → 500 with a generic sentence after logging the redacted cause.

- [x] **Step 4: Implement `studio_api.go`** — `StudioBackend`, `registerStudioRoutes`,
  `studioCaller` (503 without a backend, 401 without a principal), the six handlers, a
  `studioKind` helper (`image`/`video`, and 400 otherwise) and a `studioLimit` helper
  (absent → default, clamped to the ceiling, non-numeric → 400). `server.go` gains the
  `studio StudioBackend` field and `s.registerStudioRoutes(mux)` beside the asset routes;
  `server_seams.go` gains `SetStudio`; `idempotency_http.go` gains
  `"POST /api/studio/videos": httpMutationMeta("studio_video_create")`,
  `"POST /api/studio/images": httpMutationMeta("studio_image_create")` and
  `"POST /api/studio/uploads/{id}/finalize": httpMutationMeta("studio_upload_finalize")`.

- [x] **Step 5: Write the failing wiring tests** in `cmd/aura/serve_studio_test.go`:
  - an unlisted model is refused (`unsupported`) with no call to the submitter or the
    generator;
  - an unavailable catalog answers coded `job_failed` saying nothing was generated;
  - `SubmitVideo` passes `SurfaceStudio`, empty conversation and call, and tracks the job with
    no inline waiter;
  - `GenerateImage` ingests the bytes as an agent image with no thread and records the row with
    the cost and a minted `image-<uuid>` provider id;
  - a Studio completion (empty conversation) enqueues nothing in
    `backgroundCompletionDispatcher`.

- [x] **Step 6: Implement `cmd/aura/serve_studio.go`** — `studioBackend` over the live media
  dependencies:

  ```go
  // SubmitVideo submits only a model the catalog lists: the Studio names its model per request,
  // so an unlisted one is a stale or forged form, never a model to try on the operator's money.
  func (b studioBackend) SubmitVideo(ctx context.Context, owner string, req agui.StudioVideoRequest) (mediagen.Job, error) {
  	if err := b.listed(ctx, mediagen.KindVideo, req.Model); err != nil {
  		return mediagen.Job{}, err
  	}
  	job, err := b.submit(ctx, mediagen.VideoSubmission{
  		Owner: owner, Surface: mediagen.SurfaceStudio, Model: req.Model,
  		Input: mediagen.VideoInput{
  			Prompt: req.Prompt, Duration: req.Duration, Resolution: req.Resolution, AspectRatio: req.AspectRatio,
  			FirstFrameAssetID: req.FirstFrameAssetID, LastFrameAssetID: req.LastFrameAssetID,
  			Audio: req.Audio, Seed: req.Seed,
  		},
  	})
  	if err != nil {
  		return mediagen.Job{}, err
  	}
  	b.track(job)
  	return job, nil
  }

  // GenerateImage pays once, stores the image as the identity's own asset outside every
  // conversation, and records the generation. A stored image whose record fails is reported as
  // such: the asset is there, so nothing is generated again.
  func (b studioBackend) GenerateImage(ctx context.Context, owner string, req agui.StudioImageRequest) (mediagen.Job, error) {
  	if err := b.listed(ctx, mediagen.KindImage, req.Model); err != nil {
  		return mediagen.Job{}, err
  	}
  	generated, err := b.generate(ctx, mediagen.ImageGeneration{
  		Owner: owner, Model: req.Model,
  		Input: mediagen.ImageInput{Prompt: req.Prompt, AspectRatio: req.AspectRatio, ReferenceAssetIDs: req.ReferenceAssetIDs},
  	})
  	if err != nil {
  		return mediagen.Job{}, err
  	}
  	providerID := "image-" + uuid.NewString()
  	asset, err := b.assets.IngestAgentFile(ctx, assets.AgentIngestRequest{
  		IdentityID: owner, ThreadID: "", SourceRef: "studio:" + providerID,
  		FileName: "generated" + imageExtension(generated.Result.MIMEType), MIMEType: generated.Result.MIMEType,
  		Modality: assets.ModalityImage, SizeBytes: int64(len(generated.Result.Bytes)),
  		Reader: bytes.NewReader(generated.Result.Bytes),
  	})
  	if err != nil {
  		return mediagen.Job{}, &mediagen.Error{Code: "job_failed", Message: "The image was generated but could not be saved."}
  	}
  	request, err := mediagen.ImageRecord(generated, b.origin)
  	if err != nil {
  		return mediagen.Job{}, err
  	}
  	return b.jobs.InsertImage(ctx, mediagen.Job{
  		IdentityID: owner, Surface: mediagen.SurfaceStudio, Kind: mediagen.KindImage,
  		ProviderJobID: providerID, Model: req.Model, Request: request, AssetID: asset.ID,
  		CostUSD: generated.Result.CostUSD,
  	})
  }
  ```

  `mediagen.ImageRecord(generated GeneratedImage, origin string) (json.RawMessage, error)` goes
  in `generate_image.go` beside `JobRequest`, recording the same shape: the prompt (redacted by
  the same rule), the aspect ratio, the reference asset ids, the adjustments and the submission
  origin, so the record DTO reads an image row exactly as it reads a video row. Add it and its
  test in Task 3's step 6 — it belongs to the same file — and note here that Task 4 consumes
  it.

  `imageExtension` reuses the tools' `imageExtensions` table; move that map into `mediagen`
  next to `VideoExtension` as `ImageExtension(mimeType string) (string, error)` and have the
  tool call it, rather than keeping two copies.

  `wireStudio(server, chat, media, watcher)` serves the Studio only when the submitter and the
  generator are configured, the settings exist, the watcher exists and the job store is there;
  `serve.go` calls it after `wireMediaCatalog`. `serve_webui_studio.go` mounts the six routes:
  the reads on the bare handler, the three POSTs behind `agentRunCapability`.
  `serve_webui_auth_test.go` gains them in its 401 and gate tables.

- [x] **Step 7: Verify.** `go vet ./... && go build ./... && go test ./internal/agui/ ./cmd/aura/`, `-race` in WSL, `bash scripts/check-file-size.sh`, and
  `golangci-lint run ./internal/agui/... ./cmd/aura/...` in WSL.

- [x] **Step 8: Commit.** `git commit -m "feat(studio): serve the cockpit Studio's API"`.

---

### Task 5: Studio mode and data layer in the cockpit

**Files:**
- Registry: `web/src/components/ui/{toggle,toggle-group,switch,slider,dropdown-menu,kbd}.tsx`
- Modify: `web/src/shell/modes.ts`, `ModeTabBar.tsx`, `MobileAppSidebar.tsx`,
  `web/src/AppShell.tsx`, `web/src/i18n/resources.ts`
- Create: `web/src/i18n/resources.studio.ts`, `web/src/studio/studioApi.ts`,
  `web/src/studio/studioForm.ts`, `web/src/studio/frameUpload.ts`,
  `web/src/studio/modelChoice.ts`, `web/src/studio/useStudio.ts`, and a title-only
  `web/src/studio/StudioWorkspace.tsx`
- Test: `web/src/studio/__tests__/{studioForm,studioApi,frameUpload}.test.ts`, plus the shell
  tests that enumerate the modes

**Interfaces:**
- Produces:

  ```ts
  export type StudioKind = 'image' | 'video';
  export interface StudioModel {
    readonly id: string; readonly name: string; readonly description: string;
    readonly aspect_ratios: readonly string[];
    readonly durations?: readonly number[]; readonly resolutions?: readonly string[];
    readonly first_frame?: boolean; readonly last_frame?: boolean; readonly audio?: boolean; readonly seed?: boolean;
    readonly reference_max?: number;
    readonly prices?: readonly { readonly resolution: string; readonly audio: boolean; readonly usd_per_second: number }[];
    readonly usd_per_image_min?: number; readonly usd_per_image_max?: number;
    readonly usd_per_million_tokens_min?: number; readonly usd_per_million_tokens_max?: number;
  }
  export interface StudioRecord { readonly id: string; readonly kind: StudioKind; readonly status: string; readonly model: string; readonly prompt: string; readonly used: StudioUsed; readonly adjustments: readonly string[]; readonly cost_usd: number | null; readonly asset_id?: string; readonly error?: { readonly code: string; readonly message: string }; readonly created_at: string; readonly completed_at?: string }
  export interface StudioImageRef { readonly id: string; readonly file_name: string; readonly mime_type: string }
  export class StudioError extends Error { readonly status: number; readonly code: string }
  export function fetchStudioModels(kind: StudioKind, signal?: AbortSignal): Promise<{ readonly default_model: string; readonly models: readonly StudioModel[] }>
  export function listStudioHistory(kind: StudioKind | undefined, before: string | undefined, signal?: AbortSignal): Promise<readonly StudioRecord[]>
  export function createStudioVideo(body: StudioVideoBody): Promise<StudioRecord>
  export function createStudioImage(body: StudioImageBody): Promise<StudioRecord>
  export function listStudioLibrary(signal?: AbortSignal): Promise<readonly StudioImageRef[]>
  export function finalizeStudioUpload(id: string): Promise<StudioImageRef>
  // studioForm.ts
  export interface StudioOptions { readonly resolution: string; readonly duration: number | undefined; readonly aspectRatio: string; readonly audio: boolean; readonly seed: number | undefined }
  export interface StudioDraft { readonly kind: StudioKind; readonly model: string; readonly prompt: string; readonly options: StudioOptions; readonly images: readonly StudioImageRef[]; readonly endFrame: StudioImageRef | undefined }
  export function cheapestOptions(model: StudioModel): StudioOptions
  export function reconcileDraft(draft: StudioDraft, model: StudioModel): StudioDraft
  export function estimateCost(model: StudioModel, options: StudioOptions): number | undefined
  export function requestBody(draft: StudioDraft, model: StudioModel): StudioVideoBody | StudioImageBody
  export function maxImages(model: StudioModel, kind: StudioKind): number
  export function isActive(record: Pick<StudioRecord, 'status'>): boolean
  export function draftFromRecord(record: StudioRecord, images: readonly StudioImageRef[]): StudioDraft
  ```

- [x] **Step 1: Add the registry components.** From `web/`:
  `npx shadcn@latest add toggle-group switch slider dropdown-menu kbd`, answering "no" to
  overwriting. Read each generated file: it imports from `radix-ui` and `@/lib/utils`, and adds
  no dependency. Then `npm run format && npm run lint && npm run typecheck`.

- [x] **Step 2: Write the failing form tests** in `__tests__/studioForm.test.ts`, over two
  fixtures — `veoLite` (durations `[8,4,6]`, resolutions `['1080p','720p']`, ratios
  `['16:9','9:16']`, both frames, audio, seed, the four price rows) and `seedream` (an image
  model with ratios `['1:1','3:2']`, `reference_max: 4`, `usd_per_image_min/max` 0.05):
  - `cheapestOptions` picks 720p, 4 s, `16:9`, audio off, no seed; for an image model it picks
    the first declared ratio and leaves resolution, duration, audio and seed unset;
  - `estimateCost` gives 0.12 for 720p/4 s/no audio, 0.64 for 1080p/8 s/audio, 0.05 for the
    image model, and `undefined` when the price or the duration is missing — never 0;
  - `reconcileDraft` keeps declared values, falls back to the cheapest otherwise, drops an end
    frame when the model has no `last_frame` or no start frame, drops a seed a model does not
    declare, and truncates the images to `maxImages`;
  - `requestBody` sends the video body with audio and seed only when declared and the frames by
    id, and the image body with the reference ids;
  - `draftFromRecord` rebuilds a draft from a record for Reuse, resolving the image ids against
    the library;
  - `isActive` is true only for `pending` and `in_progress`.

- [x] **Step 3: Implement `studioApi.ts`** — the types above, a `StudioError` carrying the
  server's code and sentence, `studioJSON` reading `{error, message}` from a failure (a
  non-JSON body leaves the bare status), `readInit` with `credentials: 'same-origin'`, the six
  calls against the spec's routes (history with `limit=24`, `before` and `kind` only when
  given), and `assetDownloadUrl` / `assetStreamUrl` helpers. Its test stubs `fetch` and pins
  the URLs, the POST bodies, and both failure shapes.

- [x] **Step 4: Implement `studioForm.ts`** — the pure model. `resolutionHeight` mirrors
  `mediagen`'s measure (`<n>p`, or 1K/2K/4K); `cheapestOptions` sorts by that height, takes
  `Math.min` of the durations, prefers `16:9` when declared, audio off and no seed;
  `reconcileDraft` keeps only declared values and enforces `maxImages`;
  `estimateCost` multiplies the matching price row by the duration for video and reads
  `usd_per_image_min` for image, returning `undefined` when either is missing; `requestBody`
  builds the two bodies; `draftFromRecord` reads a record's `used`.

- [x] **Step 5: Implement `frameUpload.ts`** (presign with `thread_id: ''`, `putWithProgress`,
  then `finalizeStudioUpload`) and `modelChoice.ts` (`aura.studio.model.<kind>` in
  `localStorage`, `initialModel(listed, deploymentDefault)`), each with the tests named in
  their Interfaces.

- [x] **Step 6: Implement `useStudio.ts`** — `useStudioModels(kind)`,
  `useStudioHistory(kind)` as an infinite query polling the first page every 5 s while a listed
  record is active, `useCreateStudioVideo()` and `useCreateStudioImage()` invalidating the
  history on success, and `useStudioLibrary(enabled)`.

- [x] **Step 7: Register the mode and the strings.** `MODES` becomes
  `['chat', 'studio', 'graph', 'governance', 'documents', 'settings']`; `ModeTabBar` and
  `MobileAppSidebar` map `studio` to lucide's `Clapperboard`; `resources.ts` gains
  `shell.modes.studio` / `shell.modesCompact.studio` in both locales and spreads
  `studioEn` / `studioIt`; `resources.studio.ts` holds every string of the page (title,
  placeholders per mode, the tiles, the two popovers with their Reset, the model pill, Generate
  with its cost and unknown-cost wording, History with its search and empty state, the centre
  states, and the error sentences for `no_key`, `no_credit`, `outcome_unknown`, `local_route`
  and the generic fallback), in `en` and `it`. `AppShell.tsx` lazy-loads
  `studio/StudioWorkspace` and renders it for `surface === 'studio'`, with `studio.loading` in
  the fallback; if those lines push the file past 600 LOC, extract the surface switch into
  `web/src/shell/SurfaceWorkspace.tsx` in this step. `StudioWorkspace.tsx` renders only the
  title for now.

- [x] **Step 8: Verify.** From `web/`:
  `npm run typecheck && npm run lint && npx vitest run src/studio src/shell src/i18n && npm run dup`.
  `npm run deadcode` may flag the hooks until Task 6; if only those, fold this commit into
  Task 6's.

- [x] **Step 9: Commit.**
  `git commit -m "feat(cockpit): add the Studio mode and its data layer"`.

---

### Task 6: The Studio page

**Files:**
- Modify: `web/src/studio/StudioWorkspace.tsx`, `web/src/chat/generation/GenerationFrame.tsx`
  (an optional `startedAt`)
- Create: `web/src/studio/StudioBar.tsx`, `ImageTile.tsx`, `RatioTiles.tsx`,
  `OptionsPopover.tsx`, `AdvancedPopover.tsx`, `StudioStage.tsx`, `StudioHistory.tsx`
- Modify: `web/src/styles/` (the `.studio-pill`, `.studio-title` and card-reveal rules)
- Test: `web/src/studio/__tests__/{StudioWorkspace,StudioBar,ImageTile,StudioHistory}.test.tsx`,
  `web/src/chat/generation/__tests__/GenerationFrame.test.tsx`

- [ ] **Step 1: `GenerationFrame` takes a start time.** Failing test: with
  `startedAt = Date.now() - 65_000` under fake timers the timer reads the clock's 65-second
  form. Implementation: an optional `readonly startedAt?: number` threaded into
  `useElapsedClock`, defaulting to mount, documented as "a Studio card passes the job's
  creation time, so a reload does not restart the clock".

- [ ] **Step 2: Write the failing page tests** (`StudioWorkspace.test.tsx`), rendering inside a
  fresh `QueryClientProvider` with the i18n setup the other workspace tests use and `fetch`
  stubbed per URL:
  - **Empty page:** the gradient title, the prompt placeholder for video, and Generate reading
    the veo-lite estimate `≈ $0.12`.
  - **Mode switch:** choosing Image keeps the typed prompt, loads the image models, swaps the
    placeholder, hides the duration and Advanced pills, and re-estimates with the image price.
  - **Submit video:** Ctrl+Enter posts once to `/api/studio/videos` with
    `{model, prompt, duration: 4, resolution: '720p', aspect_ratio: '16:9', audio: false}`,
    disables Generate until it resolves, and refetches the history.
  - **Submit image:** the same flow posts to `/api/studio/images` with the reference ids.
  - **Refusal:** a 409 `no_key` shows the localized sentence in an `alert`.
  - **Empty prompt:** Generate is disabled and nothing is posted.
  - **Model memory:** choosing another model writes `aura.studio.model.video`, a remount keeps
    it, and a throwing `localStorage` does not break the choice.
  - **Catalog refusal:** a 409 `local_route` shows its sentence and no bar.
- [ ] **Step 3: Write the failing component tests.**
  - `StudioBar.test.tsx`: the options pill summarizes `16:9 · 720p · 4s`; the popover is headed
    "Video settings" and its Reset returns the cheapest; choosing 1080p and turning on Sound in
    Advanced updates the pill and the estimate; a model without audio or seed shows neither
    control; an image model shows only the ratio tiles; the end tile appears only with a start
    frame on a model that declares `last_frame`.
  - `ImageTile.test.tsx`: the menu offers upload and library; upload calls `uploadStudioFrame`
    and reports the frame; a failed upload shows the reason; the library lists
    `/api/studio/library` and selecting one reports it; a disabled tile carries
    `aria-disabled` and its reason; a filled tile shows the thumbnail and removes on click.
  - `StudioHistory.test.tsx`: cards render per status, the search filters by prompt, selecting
    a card calls `onSelect`, "Load more" appears only with a next page, and the toggle
    collapses the panel and remembers it.

- [ ] **Step 4: Implement the pieces.**
  - **`RatioTiles.tsx`** — a `ToggleGroup` of tiles, each drawing a rectangle at its ratio
    (`style={{ aspectRatio: ratio.replace(':', ' / ') }}` inside a fixed box) with the label
    under it.
  - **`OptionsPopover.tsx`** — the pill (summary) plus a popover headed by the mode's title with
    Reset; the ratio tiles; resolution tiles (video); a `Slider` over the sorted declared
    durations with the value beside it (video). Every control appears only when the model
    declares its values, and `optionsSummary` is exported for the pill and its test.
  - **`AdvancedPopover.tsx`** (video only) — seed (a number `Input`, empty for none) and the
    audio `Switch`, each shown only when declared, with Reset.
  - **`ImageTile.tsx`** — the rotated `+` tile with a `DropdownMenu` (upload, library), a hidden
    file input, a `Popover` listing `/api/studio/library` thumbnails, the filled state with a
    remove control, and the disabled state carrying its reason.
  - **`StudioBar.tsx`** — the tiles row plus the prompt textarea (Ctrl+Enter submits), then the
    pills: mode (`ToggleGroup`), model (`model-selector` rows showing name, description and
    price), options, advanced, and the Generate button with its estimate and `Kbd`.
  - **`StudioStage.tsx`** — the centre: the gradient title when nothing is selected, else the
    running frame, the image or the clip through `PreviewByKind`, with model, cost, Download
    and Reuse, and the error card for a failed record.
  - **`StudioHistory.tsx`** — the right panel: toggle, prompt search, cards (thumbnail from the
    asset for an image, the video's first frame via `preload="metadata"` for a clip), "Load
    more", the empty state, and the drawer behaviour below `md`.
  - **`StudioWorkspace.tsx`** — the composition: models and history queries, the draft state,
    mode and model choice with `reconcileDraft`, submit through the right mutation, selection
    of the shown record (the newest by default), Reuse, and the failure alert.
  - **Styles** — `.studio-pill` (rounded-full, border, surface background, `text-xs`, `h-8`),
    `.studio-title` (a gradient from the accent tokens with `background-clip: text`, and a
    plain colour under `forced-colors`), and a staggered card reveal disabled under
    `prefers-reduced-motion`. Aura's tokens only.

- [ ] **Step 5: Run the tests.** `npx vitest run src/studio src/chat/generation`.

- [ ] **Step 6: Verify the gates.** From `web/`:
  `npm run typecheck && npm run lint && npm run test && npm run dup && npm run deadcode && npm run format:check && npm run contrast`,
  then `npm run build` (which regenerates `internal/webui/dist`).

- [ ] **Step 7: Look at it.** `npm run dev`, sign in, open Studio at 1280 px and 390 px, light
  and dark: the empty page, both modes, every popover, the tile menu, the History panel and the
  drawer. Do not press Generate — that is Task 7's paid run.

- [ ] **Step 8: Commit** the web files and `internal/webui/dist`:
  `git commit -m "feat(cockpit): build the Studio page"`.

---

### Task 7: Gates, live acceptance and evidence

**Files:**
- Create: `web/e2e/studio-live.spec.ts`, `docs/testing/studio.md`
- Modify: `scripts/critical_mutation_gate.py` (the new frontend files in the media scope),
  `scripts/coverage_package_policy.json` (only if a measured ratio moved), this plan's boxes

- [ ] **Step 1: Write the paid spec**, gated by `AURA_E2E_STUDIO=1` and skipped otherwise (so
  CI never runs it), reusing `gotoAuthenticated` and `sameOriginFetch` from
  `media-generation-live.helpers.ts`. It:
  1. opens the Studio with `aura.shell.surface = 'studio'` preset;
  2. generates an image with the cheapest declared image model, asserting the estimate on the
     button and then a completed record with an `asset_id`;
  3. switches to Video, presets `google/veo-3.1-lite`, asserts `≈ $0.12`, generates, and waits
     for the clip;
  4. reloads and finds both in History;
  5. asserts `GET /api/conversations` is unchanged in length;
  6. attaches both records as annotations.

- [ ] **Step 2: Run the full local gates.** In WSL, `make quality`; then
  `bash scripts/coverage_docker.sh` (only `aura_cov`); then the web gate list.

- [ ] **Step 3: Build and deploy the acceptance image.**
  `docker build -f docker/aura/Dockerfile -t aura:acceptance --build-arg VCS_REF=$(git rev-parse HEAD) .`,
  then in `/opt/aura` recreate `aura-migrate` first (migration 0129) and then `aura` with
  `AURA_IMAGE=aura:acceptance AURA_PULL_POLICY=never`. Confirm health and that the migration
  head is 129.

- [ ] **Step 4: Ask for the go, then run the paid spec.** State the expected spend (≈ $0.05 for
  the image, ≈ $0.12 for the clip). With the go:

  ```bash
  AURA_E2E_STUDIO=1 bash <scratchpad>/e2e_run.sh e2e/studio-live.spec.ts --project=chrome --reporter=line
  ```

  Record both records' ids, asset ids and costs, and check by hand that Download works and the
  History survives a reload.

- [ ] **Step 5: Write the evidence** in `docs/testing/studio.md`: the gates, the migration head,
  the paid run's ids and costs against the estimates, and what the run does not prove (one
  model per kind, the SKU naming checked on veo-lite only, seed determinism untested).

- [ ] **Step 6: Commit, tick this plan, report.**
  `git commit -m "test(studio): record the live Studio acceptance"`, then tell the operator the
  commits are local, push only when asked, watch CI to green, and move `/opt/aura` back to
  `:edge` once the published image carries the Studio.
