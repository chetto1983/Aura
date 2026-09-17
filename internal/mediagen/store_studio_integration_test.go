//go:build db_integration

// The live-Postgres half of the cockpit Studio's rows: the surface/kind CHECKs migration
// 0129 adds, InsertImage's finished-generation guard, the delivered-on-completion rule for a
// Studio video, and ListStudio's paging. Shares its pool, identity and asset helpers with
// store_integration_test.go.
//
//	go test -tags db_integration ./internal/mediagen -run 'TestStore(Studio|RejectsChatJob|CompleteMarksStudio|InsertImage|ListStudio)' -count=1
package mediagen

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// seedStudioJob writes a Studio row directly, so history paging can be tested without real
// provider timing. A video row is left pending, active but never polled by these tests; an
// image row is written already completed and delivered, matching InsertImage's own write.
func seedStudioJob(t *testing.T, pool *pgxpool.Pool, owner string, kind Kind, assetID string, createdAgo time.Duration) Job {
	t.Helper()
	status := "pending"
	if kind == KindImage {
		status = "completed"
	}
	var id string
	err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
INSERT INTO aura.media_job (identity_id, conversation_id, tool_call_id, provider_job_id, model, request,
                            status, asset_id, surface, kind, created_at, updated_at, completed_at, delivered_at)
VALUES ($1, '', '', $2, 'minimax/hailuo-3-max', '{"model":"minimax/hailuo-3-max"}',
        $3, NULLIF($4, '')::uuid, 'studio', $5, now() - $6::interval, now() - $6::interval,
        CASE WHEN $3 = 'completed' THEN now() - $6::interval END,
        CASE WHEN $3 = 'completed' THEN now() - $6::interval END)
RETURNING id`,
			owner, "studio_"+uuid.NewString(), status, assetID, string(kind), createdAgo.String()).Scan(&id)
	})
	if err != nil {
		t.Fatalf("seed studio %s job: %v", kind, err)
	}
	job, err := NewStore(pool).Get(context.Background(), owner, id)
	if err != nil {
		t.Fatalf("read seeded studio job: %v", err)
	}
	return job
}

func newStudioVideoJob(owner string) Job {
	return Job{
		IdentityID: owner, Surface: SurfaceStudio, Kind: KindVideo,
		ProviderJobID: "studio_" + uuid.NewString(), Model: "minimax/hailuo-3-max",
		Request: []byte(`{"model":"minimax/hailuo-3-max","_aura":{"origin":"https://openrouter.ai/api/v1"}}`),
		Status:  StatusPending,
	}
}

func newStudioImage(owner, assetID string) Job {
	return Job{
		IdentityID: owner, Surface: SurfaceStudio, Kind: KindImage,
		ProviderJobID: "image-" + uuid.NewString(), Model: "google/gemini-2.5-flash-image",
		Request: []byte(`{"model":"google/gemini-2.5-flash-image"}`), AssetID: assetID,
	}
}

func TestStoreStudioJobRoundTrip(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	owner := seedMediaIdentity(t, pool)

	inserted, err := store.Insert(ctx, newStudioVideoJob(owner))
	if err != nil {
		t.Fatal(err)
	}
	if inserted.Surface != SurfaceStudio || inserted.Kind != KindVideo ||
		inserted.ConversationID != "" || inserted.ToolCallID != "" {
		t.Fatalf("inserted Studio job = %#v", inserted)
	}
	got, err := store.Get(ctx, owner, inserted.ID)
	if err != nil || got.Surface != SurfaceStudio || got.Kind != KindVideo {
		t.Fatalf("Get = %#v (%v), want the Studio surface and kind read back", got, err)
	}
}

func TestStoreRejectsChatJobWithoutConversation(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	owner := seedMediaIdentity(t, pool)
	ownerID, err := db.ParseUUID("owner id", owner)
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		_, err := sqlc.New(tx).InsertMediaJob(ctx, sqlc.InsertMediaJobParams{
			IdentityID: ownerID, ConversationID: "", ToolCallID: "call-1",
			ProviderJobID: "vid_" + uuid.NewString(), Model: "m", Request: []byte("{}"),
			Status: string(StatusPending), Surface: string(SurfaceChat), Kind: string(KindVideo),
		})
		return err
	})
	if sqlState(err) != "23514" {
		t.Fatalf("raw InsertMediaJob with surface=chat and no conversation err = %v, want SQLSTATE 23514", err)
	}
}

func TestStoreCompleteMarksStudioDelivered(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	owner := seedMediaIdentity(t, pool)

	studioAsset := seedAsset(t, pool, owner, seededAsset{modality: "video", status: "accepted"})
	studioJob, err := store.Insert(ctx, newStudioVideoJob(owner))
	if err != nil {
		t.Fatal(err)
	}
	studioDone, err := store.Complete(ctx, owner, studioJob.ID, studioAsset, nil)
	if err != nil || studioDone.DeliveredAt == nil {
		t.Fatalf("Complete on a Studio job = %#v (%v), want it delivered in the same statement", studioDone, err)
	}
	jobs, err := store.Recoverable(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.ID == studioJob.ID {
			t.Fatalf("Recoverable still lists the delivered Studio job %s", job.ID)
		}
	}

	chatAsset := seedAsset(t, pool, owner, acceptedVideo)
	chatJob, err := store.Insert(ctx, newJob(owner))
	if err != nil {
		t.Fatal(err)
	}
	chatDone, err := store.Complete(ctx, owner, chatJob.ID, chatAsset, nil)
	if err != nil || chatDone.DeliveredAt != nil {
		t.Fatalf("Complete on a chat job = %#v (%v), want delivered_at left for its own delivery claim", chatDone, err)
	}
}

func TestStoreInsertImageRecordsAFinishedGeneration(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	owner, stranger := seedMediaIdentity(t, pool), seedMediaIdentity(t, pool)

	image := seedAsset(t, pool, owner, seededAsset{modality: "image", status: "accepted"})
	inserted, err := store.InsertImage(ctx, newStudioImage(owner, image))
	if err != nil {
		t.Fatal(err)
	}
	if inserted.Status != StatusCompleted || inserted.Kind != KindImage || inserted.Surface != SurfaceStudio ||
		inserted.AssetID != image || inserted.CompletedAt == nil || inserted.DeliveredAt == nil {
		t.Fatalf("InsertImage = %#v, want a completed, delivered image row", inserted)
	}

	refused := map[string]string{
		"video asset":         seedAsset(t, pool, owner, seededAsset{modality: "video", status: "accepted"}),
		"another identity's":  seedAsset(t, pool, stranger, seededAsset{modality: "image", status: "accepted"}),
		"thread-scoped asset": seedAsset(t, pool, owner, seededAsset{thread: "thread-a", modality: "image", status: "accepted"}),
	}
	for name, assetID := range refused {
		if _, err := store.InsertImage(ctx, newStudioImage(owner, assetID)); ErrorCode(err) != "asset_not_found" {
			t.Errorf("InsertImage with %s err = %v, want asset_not_found", name, err)
		}
	}

	jobs, err := store.Recoverable(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.ID == inserted.ID {
			t.Fatalf("Recoverable lists the finished image job %s: the watcher must never poll it", job.ID)
		}
	}
}

func TestStoreListStudioPages(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	owner, stranger := seedMediaIdentity(t, pool), seedMediaIdentity(t, pool)

	image := seedAsset(t, pool, owner, seededAsset{modality: "image", status: "accepted"})
	newest := seedStudioJob(t, pool, owner, KindVideo, "", 5*time.Minute)
	middle := seedStudioJob(t, pool, owner, KindVideo, "", 10*time.Minute)
	oldest := seedStudioJob(t, pool, owner, KindVideo, "", 15*time.Minute)
	imageJob := seedStudioJob(t, pool, owner, KindImage, image, 1*time.Minute)
	seedJob(t, pool, owner, "pending", "", 0, false)               // chat job: must never appear
	seedStudioJob(t, pool, stranger, KindVideo, "", 2*time.Minute) // another identity: must never appear

	first, err := store.ListStudio(ctx, owner, "", "", 2)
	if err != nil || len(first) != 2 || first[0].ID != imageJob.ID || first[1].ID != newest.ID {
		t.Fatalf("first page = %#v (%v), want the newest two Studio rows, newest first", first, err)
	}
	second, err := store.ListStudio(ctx, owner, first[1].ID, "", 2)
	if err != nil || len(second) != 2 || second[0].ID != middle.ID || second[1].ID != oldest.ID {
		t.Fatalf("second page = %#v (%v), want the remaining two, oldest last", second, err)
	}
	third, err := store.ListStudio(ctx, owner, second[1].ID, "", 2)
	if err != nil || len(third) != 0 {
		t.Fatalf("third page = %#v (%v), want an empty page past the last row", third, err)
	}

	onlyImages, err := store.ListStudio(ctx, owner, "", KindImage, StudioPageMax)
	if err != nil || len(onlyImages) != 1 || onlyImages[0].ID != imageJob.ID {
		t.Fatalf("kind=image page = %#v (%v), want only the image row", onlyImages, err)
	}

	malformed, err := store.ListStudio(ctx, owner, "not-a-uuid", "", StudioPageMax)
	if err != nil || len(malformed) != 0 {
		t.Fatalf("malformed before = %#v (%v), want an empty page, not an error", malformed, err)
	}
}
