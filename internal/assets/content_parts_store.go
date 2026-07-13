package assets

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type storedContentPart struct {
	part     ContentPart
	migrated bool
	links    map[string]ContentPartLink
}

// MemoryContentPartStore provides the content-part contract for pure tests and ephemeral runtimes.
type MemoryContentPartStore struct {
	mu    sync.RWMutex
	parts map[string]*storedContentPart
	next  int
}

// NewMemoryContentPartStore creates an empty store.
func NewMemoryContentPartStore() *MemoryContentPartStore {
	return &MemoryContentPartStore{parts: map[string]*storedContentPart{}}
}

// Put records immutable bytes and their digest.
func (s *MemoryContentPartStore) Put(_ context.Context, req PutContentPart) (ContentPart, error) {
	if req.TenantID == "" || req.OwnerID == "" || req.MIMEType == "" {
		return ContentPart{}, fmt.Errorf("content part identity and MIME are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := fmt.Sprintf("part-%d", s.next)
	b := append([]byte(nil), req.Bytes...)
	p := ContentPart{ID: id, StorageID: id, TenantID: req.TenantID, OwnerID: req.OwnerID, MIMEType: req.MIMEType, Digest: digestBytes(b), ByteLength: int64(len(b)), Bytes: b, Retention: req.Retention, EncryptionClass: req.EncryptionClass, ProviderRequirements: append([]string(nil), req.ProviderRequirements...), FallbackText: req.FallbackText, ExpiresAt: req.ExpiresAt}
	s.parts[id] = &storedContentPart{part: p, links: map[string]ContentPartLink{}}
	return p, nil
}

// Reload reauthorizes and verifies bytes before returning them.
func (s *MemoryContentPartStore) Reload(_ context.Context, principal PartPrincipal, id string) (ContentPart, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.parts[id]
	if !ok || rec.part.TenantID != principal.TenantID || rec.part.OwnerID != principal.OwnerID {
		return ContentPart{}, ErrContentPartUnavailable
	}
	if rec.migrated {
		return ContentPart{}, ErrContentPartMigrated
	}
	if int64(len(rec.part.Bytes)) != rec.part.ByteLength || digestBytes(rec.part.Bytes) != rec.part.Digest {
		return ContentPart{}, ErrContentPartCorrupt
	}
	p := rec.part
	p.Bytes = append([]byte(nil), p.Bytes...)
	return p, nil
}

// Link adds an immutable reachability edge.
func (s *MemoryContentPartStore) Link(_ context.Context, link ContentPartLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.parts[link.PartID]
	if rec == nil {
		return ErrContentPartUnavailable
	}
	key := string(link.Kind) + link.TurnID + link.CheckpointID + link.MemoryID
	if old, ok := rec.links[key]; ok && old != link {
		return errorsNewImmutable()
	}
	rec.links[key] = link
	return nil
}
func errorsNewImmutable() error { return fmt.Errorf("content part links are immutable") }

// GC removes only expired parts without reachability edges.
func (s *MemoryContentPartStore) GC(_ context.Context, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, rec := range s.parts {
		if len(rec.links) == 0 && !rec.part.ExpiresAt.IsZero() && !rec.part.ExpiresAt.After(now) {
			delete(s.parts, id)
			n++
		}
	}
	return n
}

// MarkMigrated quarantines the old storage record after migration.
func (s *MemoryContentPartStore) MarkMigrated(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parts[id] == nil {
		return ErrContentPartUnavailable
	}
	s.parts[id].migrated = true
	return nil
}

// CorruptForTest replaces bytes without their digest to exercise fail-closed reloads.
func (s *MemoryContentPartStore) CorruptForTest(id string, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parts[id] != nil {
		s.parts[id].part.Bytes = append([]byte(nil), b...)
	}
}
