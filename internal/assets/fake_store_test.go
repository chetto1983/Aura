package assets

import (
	"context"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeAssetStore struct {
	mu      sync.Mutex
	next    int
	created []CreateRequest
	assets  map[string]Asset
	// duplicateKey makes the next Create fail the way aura.assets does when its
	// (identity_id, object_key) UNIQUE index is already taken.
	duplicateKey bool
	// createErr fails Create with something that is NOT a duplicate key.
	createErr error
	// lastRecentLimit, lastRecentModalities and lastRecentBefore are what ListRecent was called
	// with, so the service's clamp is observable where the real store would only pass it to SQL.
	lastRecentLimit      int
	lastRecentModalities []Modality
	lastRecentBefore     string
}

func newFakeAssetStore() *fakeAssetStore {
	return &fakeAssetStore{assets: make(map[string]Asset)}
}

// The fake answers like the real store: a row with deleted_at is invisible to every read
// and every update, and a miss is pgx.ErrNoRows.
func (s *fakeAssetStore) ByObjectKey(_ context.Context, identityID, objectKey string) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, asset := range s.assets {
		if asset.IdentityID == identityID && asset.ObjectKey == objectKey && asset.DeletedAt.IsZero() {
			return asset, nil
		}
	}
	return Asset{}, pgx.ErrNoRows
}

func (s *fakeAssetStore) Create(_ context.Context, req CreateRequest) (Asset, error) {
	s.mu.Lock()
	if s.createErr != nil {
		s.mu.Unlock()
		return Asset{}, s.createErr
	}
	if s.duplicateKey {
		s.duplicateKey = false
		s.mu.Unlock()
		return Asset{}, objectKeyViolation()
	}
	defer s.mu.Unlock()
	if req.SourceKind == SourceAgent && req.SourceRef != "" {
		for _, asset := range s.assets {
			if asset.IdentityID == req.IdentityID && asset.SourceKind == req.SourceKind && asset.SourceRef == req.SourceRef {
				return asset, nil
			}
		}
	}
	if s.holdsObjectKey(req.IdentityID, req.ObjectKey) {
		return Asset{}, objectKeyViolation()
	}
	s.next++
	id := "asset-test-" + string(rune('0'+s.next))
	asset := Asset{
		ID:                id,
		IdentityID:        req.IdentityID,
		SourceKind:        req.SourceKind,
		SourceRef:         req.SourceRef,
		ThreadID:          req.ThreadID,
		Scope:             req.Scope,
		Modality:          req.Modality,
		Status:            StatusPresigned,
		FileName:          req.FileName,
		MIMEType:          req.MIMEType,
		DeclaredSizeBytes: req.DeclaredSizeBytes,
		ObjectBucket:      req.ObjectBucket,
		ObjectKey:         req.ObjectKey,
		Metadata:          cloneMetadata(req.Metadata),
	}
	s.created = append(s.created, req)
	s.assets[id] = asset
	return asset, nil
}

func (s *fakeAssetStore) GetForIdentity(_ context.Context, id, identityID string) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live(id, identityID)
}

func (s *fakeAssetStore) live(id, identityID string) (Asset, error) {
	asset, ok := s.assets[id]
	if !ok || asset.IdentityID != identityID || !asset.DeletedAt.IsZero() {
		return Asset{}, pgx.ErrNoRows
	}
	return asset, nil
}

func (s *fakeAssetStore) ListForThread(_ context.Context, identityID, threadID string) ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Asset
	for _, asset := range s.assets {
		if asset.IdentityID == identityID && asset.ThreadID == threadID && asset.DeletedAt.IsZero() {
			out = append(out, asset)
		}
	}
	return out, nil
}

func (s *fakeAssetStore) ListForLibrary(_ context.Context, identityID string, limit int) ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Asset
	for _, asset := range s.assets {
		if asset.IdentityID == identityID && asset.Scope == ScopeLibrary && asset.DeletedAt.IsZero() {
			out = append(out, asset)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *fakeAssetStore) ListRecent(
	_ context.Context, identityID, beforeID string, modalities []Modality, limit int,
) ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRecentLimit = limit
	s.lastRecentModalities = modalities
	s.lastRecentBefore = beforeID
	// A before_id the owner does not hold compares as NULL in the query, so the page is empty.
	before, paged := s.assets[beforeID]
	if beforeID != "" && (!paged || before.IdentityID != identityID) {
		return nil, nil
	}
	var out []Asset
	for _, asset := range s.assets {
		if asset.IdentityID != identityID || !slices.Contains(modalities, asset.Modality) ||
			!usableAssetStatuses[asset.Status] || !asset.DeletedAt.IsZero() ||
			(paged && !newerFirst(before, asset)) {
			continue
		}
		out = append(out, asset)
	}
	// The query orders by (created_at, id) DESC and only then applies the LIMIT; a map's
	// iteration order would otherwise make this fake answer a different question than the store.
	sort.Slice(out, func(i, j int) bool { return newerFirst(out[i], out[j]) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// newerFirst is the query's (created_at, id) DESC order: whether a comes before b.
func newerFirst(a, b Asset) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}

// The statuses ListRecentImageAssets accepts, kept beside the fake that has to agree with it.
var usableAssetStatuses = map[Status]bool{
	StatusAccepted: true, StatusProcessing: true, StatusComplete: true,
}

func (s *fakeAssetStore) MarkUploaded(_ context.Context, id, identityID string, size int64, etag string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = StatusUploaded
		asset.SizeBytes = size
		asset.ObjectETag = etag
	})
}

func (s *fakeAssetStore) MarkAccepted(_ context.Context, id, identityID string, size int64, hash, mimeType string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = StatusAccepted
		asset.SizeBytes = size
		asset.ContentHash = hash
		asset.MIMEType = mimeType
	})
}

func (s *fakeAssetStore) SetStatus(_ context.Context, id, identityID string, status Status, code, message string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = status
		asset.ErrorCode = code
		asset.ErrorMessage = message
	})
}

// ResetForRetry mirrors ResetAssetForIngestionRetry's guard; the statement itself is proven in
// store_retry_integration_test.go.
func (s *fakeAssetStore) ResetForRetry(_ context.Context, id, identityID string) (Asset, error) {
	s.mu.Lock()
	current, ok := s.assets[id]
	s.mu.Unlock()
	if !ok || current.IdentityID != identityID ||
		(current.Status != StatusFailed && current.Status != StatusRefused && current.Status != StatusCanceled) {
		return Asset{}, ErrNotRetryable
	}
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = StatusAccepted
		asset.ErrorCode = ""
		asset.ErrorMessage = ""
		asset.ProcessedAt = time.Time{}
		asset.CompletedAt = time.Time{}
	})
}

func (s *fakeAssetStore) SetResult(_ context.Context, id, identityID string, result Result) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = result.Status
		asset.DocumentID = result.DocumentID
		asset.Summary = result.Summary
		asset.Metadata = cloneMetadata(result.Metadata)
		asset.ErrorCode = ""
		asset.ErrorMessage = ""
	})
}

// Adoption is a no-op here: the fake has no unclaimed-thread notion, and every test
// that exercises the real claim drives the store.
func (s *fakeAssetStore) AdoptIntoThread(_ context.Context, id, identityID, threadID string) (Asset, error) {
	return s.update(id, identityID, func(a *Asset) {
		if a.ThreadID == "" {
			a.ThreadID = threadID
		}
	})
}

func (s *fakeAssetStore) Promote(_ context.Context, id, identityID string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Scope = ScopeLibrary
	})
}

func (s *fakeAssetStore) Delete(_ context.Context, id, identityID string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = StatusDeleting
		asset.DeletedAt = time.Now()
	})
}

// Finalize drops a deleting row, as the real store does for a row no media_job points at;
// the tombstone branch needs aura.media_job and is proven against Postgres.
func (s *fakeAssetStore) Finalize(_ context.Context, id, identityID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if asset, ok := s.assets[id]; ok && asset.IdentityID == identityID && asset.Status == StatusDeleting {
		delete(s.assets, id)
	}
	return nil
}

func (s *fakeAssetStore) ListDeleting(_ context.Context, identityID string, limit int) ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Asset
	for _, asset := range s.assets {
		if asset.IdentityID == identityID && asset.Status == StatusDeleting {
			out = append(out, asset)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *fakeAssetStore) update(id, identityID string, apply func(*Asset)) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	asset, err := s.live(id, identityID)
	if err != nil {
		return Asset{}, err
	}
	apply(&asset)
	s.assets[id] = asset
	return asset, nil
}

func cloneMetadata(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	maps.Copy(out, in)
	return out
}
