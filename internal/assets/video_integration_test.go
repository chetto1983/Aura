//go:build db_integration

// The real-database half of the video modality: migration 0127's widened
// assets_modality_check must actually admit a 'video' row through the same store every
// other modality goes through (Store.Create -> aura.assets), not merely parse in isolation.
//
//	go test -tags=db_integration ./internal/assets -run TestVideo -count=1
package assets

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

// mp4FixtureBytes is a genuine, if minimal, MP4 header -- a size-32 "ftyp" box declaring the
// "isom" brand -- rather than an arbitrary byte string standing in for a video. The pipeline
// never sniffs file magic (hashAndSniff trusts the caller's declared MIME type; see
// service.go), so this buys nothing functionally, but a fixture worth calling "real" should
// still parse as the format it claims to be.
var mp4FixtureBytes = []byte{
	0x00, 0x00, 0x00, 0x20, 0x66, 0x74, 0x79, 0x70, // size=32, box type "ftyp"
	0x69, 0x73, 0x6f, 0x6d, 0x00, 0x00, 0x02, 0x00, // major brand "isom", minor version
	0x69, 0x73, 0x6f, 0x6d, 0x69, 0x73, 0x6f, 0x32, // compatible brands: isom, iso2
	0x61, 0x76, 0x63, 0x31, 0x6d, 0x70, 0x34, 0x31, // compatible brands: avc1, mp41
}

// webmFixtureBytes is a genuine WebM/EBML header (the standard EBML magic number followed
// by the DocType "webm" element), for the second allowed format.
var webmFixtureBytes = []byte{
	0x1a, 0x45, 0xdf, 0xa3, // EBML magic number
	0x9f, 0x42, 0x86, 0x81, 0x01, // EBMLVersion = 1
	0x42, 0xf7, 0x81, 0x01, // EBMLReadVersion = 1
	0x42, 0x82, 0x84, 0x77, 0x65, 0x62, 0x6d, // DocType = "webm"
}

// TestVideoAssetPersistsThroughTheRealModalityConstraint proves migration 0127 against the
// live schema: IngestAgentFile (the D-04 limit-bypassing agent path, per the task brief step
// 4) stores an MP4 and a WebM asset row with modality 'video' through the SAME Store every
// other modality uses, and GetForIdentity reads back status/modality/MIME/bytes/ownership.
// A pre-0127 schema would refuse the INSERT with SQLSTATE 23514 (check_violation) on the
// very first Create below.
func TestVideoAssetPersistsThroughTheRealModalityConstraint(t *testing.T) {
	pool := migratedAssetPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	owner := uuid.Must(uuid.NewV7()).String()
	seedAssetRLSIdentity(t, pool, owner, "video-owner-"+owner[:8])

	svc := &Service{
		Store:      NewStore(pool),
		Objects:    objectstore.NewFake(),
		Limits:     Limits{MaxVideoBytes: 50 << 20},
		Bucket:     "asset-video-test",
		PresignTTL: time.Minute,
	}

	for _, sample := range []struct {
		name, mime string
		body       []byte
	}{
		{"clip.mp4", "video/mp4", mp4FixtureBytes},
		{"clip.webm", "video/webm", webmFixtureBytes},
	} {
		t.Run(sample.name, func(t *testing.T) {
			sourceRef := "video-fixture:" + uuid.Must(uuid.NewV7()).String()
			asset, err := svc.IngestAgentFile(ctx, AgentIngestRequest{
				IdentityID: owner,
				ThreadID:   "thread-video-1",
				SourceRef:  sourceRef,
				FileName:   sample.name,
				MIMEType:   sample.mime,
				Modality:   ModalityVideo,
				SizeBytes:  int64(len(sample.body)),
				Reader:     strings.NewReader(string(sample.body)),
			})
			if err != nil {
				t.Fatalf("IngestAgentFile(%s): %v", sample.name, err)
			}
			if asset.Status != StatusAccepted {
				t.Fatalf("asset.Status = %q, want %q", asset.Status, StatusAccepted)
			}
			if asset.Modality != ModalityVideo {
				t.Fatalf("asset.Modality = %q, want %q (migration 0127 constraint)", asset.Modality, ModalityVideo)
			}

			got, err := svc.Store.GetForIdentity(ctx, asset.ID, owner)
			if err != nil {
				t.Fatalf("GetForIdentity: %v", err)
			}
			if got.Modality != ModalityVideo {
				t.Fatalf("read-back modality = %q, want %q", got.Modality, ModalityVideo)
			}
			if got.MIMEType != sample.mime {
				t.Fatalf("read-back MIME = %q, want %q", got.MIMEType, sample.mime)
			}
			if got.SizeBytes != int64(len(sample.body)) {
				t.Fatalf("read-back size = %d, want %d", got.SizeBytes, len(sample.body))
			}
			if got.IdentityID != owner {
				t.Fatalf("read-back owner = %q, want %q", got.IdentityID, owner)
			}

			// Cross-owner access is denied: the fail-closed RLS floor (migration 0090,
			// pinned in rls_integration_test.go) returns the row to nobody but its owner, so
			// a stranger reading the same asset id gets the store's not-found path, not the
			// video bytes.
			stranger := uuid.Must(uuid.NewV7()).String()
			if _, err := svc.Store.GetForIdentity(ctx, asset.ID, stranger); err == nil {
				t.Fatalf("a stranger read owner %s's video asset, want a not-found refusal", owner)
			}
		})
	}
}
