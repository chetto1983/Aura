package assets

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContentPartAuthorizationDigestAndLifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryContentPartStore()
	p, err := s.Put(ctx, PutContentPart{TenantID: "tenant-a", OwnerID: "owner-a", MIMEType: "image/png", Bytes: []byte("png"), Retention: RetentionDurable, FallbackText: "image attachment"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Reload(ctx, PartPrincipal{TenantID: "tenant-a", OwnerID: "owner-a"}, p.ID)
	if err != nil || string(got.Bytes) != "png" {
		t.Fatalf("reload=%+v err=%v", got, err)
	}
	if _, err := s.Reload(ctx, PartPrincipal{TenantID: "tenant-a", OwnerID: "other"}, p.ID); !errors.Is(err, ErrContentPartUnavailable) {
		t.Fatalf("foreign error leaks state: %v", err)
	}
	s.CorruptForTest(p.ID, []byte("tampered"))
	if _, err := s.Reload(ctx, PartPrincipal{TenantID: "tenant-a", OwnerID: "owner-a"}, p.ID); !errors.Is(err, ErrContentPartCorrupt) {
		t.Fatalf("corrupt err=%v", err)
	}
}

func TestContentPartReachabilityGCAndMigration(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryContentPartStore()
	now := time.Now()
	reachable, _ := s.Put(ctx, PutContentPart{TenantID: "t", OwnerID: "o", MIMEType: "audio/wav", Bytes: []byte("a"), ExpiresAt: now.Add(-time.Hour)})
	unreachable, _ := s.Put(ctx, PutContentPart{TenantID: "t", OwnerID: "o", MIMEType: "audio/wav", Bytes: []byte("b"), ExpiresAt: now.Add(-time.Hour)})
	if err := s.Link(ctx, ContentPartLink{PartID: reachable.ID, TurnID: "turn-1", Kind: ReachabilityTurn}); err != nil {
		t.Fatal(err)
	}
	if n := s.GC(ctx, now); n != 1 {
		t.Fatalf("deleted=%d", n)
	}
	if _, err := s.Reload(ctx, PartPrincipal{TenantID: "t", OwnerID: "o"}, unreachable.ID); !errors.Is(err, ErrContentPartUnavailable) {
		t.Fatalf("unreachable err=%v", err)
	}
	if err := s.MarkMigrated(ctx, reachable.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reload(ctx, PartPrincipal{TenantID: "t", OwnerID: "o"}, reachable.ID); !errors.Is(err, ErrContentPartMigrated) {
		t.Fatalf("migrated err=%v", err)
	}
}
