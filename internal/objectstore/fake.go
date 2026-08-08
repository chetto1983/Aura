//nolint:revive // Internal objectstore fakes are exported for tests and wiring.
package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

type FakeStore struct {
	mu      sync.RWMutex
	objects map[ObjectRef]fakeObject
}

type fakeObject struct {
	data  []byte
	attrs Attrs
}

func NewFake() *FakeStore {
	return &FakeStore{objects: make(map[ObjectRef]fakeObject)}
}

func (s *FakeStore) PresignPut(ctx context.Context, req PresignPutRequest) (PresignedPut, error) {
	if err := ctx.Err(); err != nil {
		return PresignedPut{}, err
	}
	expiresIn := req.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	u := url.URL{
		Scheme: "fake",
		Host:   req.Ref.Bucket,
		Path:   "/" + req.Ref.Key,
	}
	return PresignedPut{
		URL:    u.String(),
		Method: "PUT",
		RequiredHeaders: map[string]string{
			"Content-Type": req.MIMEType,
		},
		ExpiresAt: time.Now().Add(expiresIn),
	}, nil
}

func (s *FakeStore) Put(ctx context.Context, ref ObjectRef, body io.Reader, opts PutOptions) (Attrs, error) {
	if err := ctx.Err(); err != nil {
		return Attrs{}, err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return Attrs{}, err
	}
	attrs := Attrs{
		SizeBytes: int64(len(data)),
		ETag:      etag(data),
		MIMEType:  opts.MIMEType,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[ref] = fakeObject{data: bytes.Clone(data), attrs: attrs}
	return attrs, nil
}

func (s *FakeStore) Head(ctx context.Context, ref ObjectRef) (Attrs, error) {
	if err := ctx.Err(); err != nil {
		return Attrs{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.objects[ref]
	if !ok {
		return Attrs{}, fmt.Errorf("objectstore fake: %s/%s: %w", ref.Bucket, ref.Key, fs.ErrNotExist)
	}
	return obj.attrs, nil
}

func (s *FakeStore) Get(ctx context.Context, ref ObjectRef) (io.ReadCloser, Attrs, error) {
	if err := ctx.Err(); err != nil {
		return nil, Attrs{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.objects[ref]
	if !ok {
		return nil, Attrs{}, fmt.Errorf("objectstore fake: %s/%s: %w", ref.Bucket, ref.Key, fs.ErrNotExist)
	}
	data := bytes.Clone(obj.data)
	return io.NopCloser(bytes.NewReader(data)), obj.attrs, nil
}

func (s *FakeStore) List(ctx context.Context, req ListRequest) ([]ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Bucket == "" {
		return nil, fmt.Errorf("objectstore fake: bucket is required")
	}
	if req.Limit < 0 {
		return nil, fmt.Errorf("objectstore fake: limit must not be negative")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ObjectInfo, 0)
	// Groups are collapsed exactly as S3 collapses them: everything sharing a prefix up to
	// the first delimiter AFTER req.Prefix becomes one zero-sized entry whose key ends with
	// the delimiter. The fake has to agree with the real store here, or a browser that
	// works in tests shows a flat list of every key in production.
	groups := make(map[string]struct{})
	for ref, obj := range s.objects {
		if ref.Bucket != req.Bucket || !strings.HasPrefix(ref.Key, req.Prefix) {
			continue
		}
		if req.Delimiter != "" {
			rest := strings.TrimPrefix(ref.Key, req.Prefix)
			if index := strings.Index(rest, req.Delimiter); index >= 0 {
				groups[req.Prefix+rest[:index+len(req.Delimiter)]] = struct{}{}
				continue
			}
		}
		out = append(out, ObjectInfo{Ref: ref, Attrs: obj.attrs})
	}
	for prefix := range groups {
		out = append(out, ObjectInfo{Ref: ObjectRef{Bucket: req.Bucket, Key: prefix}})
	}
	slices.SortFunc(out, func(a, b ObjectInfo) int {
		return strings.Compare(a.Ref.Key, b.Ref.Key)
	})
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

func (s *FakeStore) Delete(ctx context.Context, ref ObjectRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, ref)
	return nil
}

func (s *FakeStore) Copy(ctx context.Context, src, dst ObjectRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[src]
	if !ok {
		return fs.ErrNotExist
	}
	s.objects[dst] = object
	return nil
}

func etag(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
