//nolint:revive // Internal filesystem store types are exported for composition roots.
package objectstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type FilesystemStore struct {
	root string
}

func NewFilesystem(root string) *FilesystemStore {
	return &FilesystemStore{root: root}
}

func (s *FilesystemStore) PresignPut(ctx context.Context, req PresignPutRequest) (PresignedPut, error) {
	if err := ctx.Err(); err != nil {
		return PresignedPut{}, err
	}
	if _, err := s.objectPath(req.Ref); err != nil {
		return PresignedPut{}, err
	}
	expiresIn := req.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	base := req.PublicBase
	if base == "" {
		base = "file://" + filepath.ToSlash(s.root)
	}
	u, err := url.Parse(strings.TrimRight(base, "/") + "/" + req.Ref.Bucket + "/" + req.Ref.Key)
	if err != nil {
		return PresignedPut{}, err
	}
	// The headers are declared, but this store keeps only bytes on disk: it has nowhere to
	// put user metadata without inventing a sidecar file per object. So a filename does not
	// survive a Put here, and the ingest sweep falls back to the key-derived name — which is
	// exactly what every backend did before this channel existed. Said plainly rather than
	// left to be discovered: this is the local development backend, and the deployment runs
	// on S3/Garage, where the metadata does survive (measured 2026-08-13).
	return PresignedPut{
		URL:             u.String(),
		Method:          "PUT",
		RequiredHeaders: presignRequiredHeaders(req.MIMEType, req.Metadata),
		ExpiresAt:       time.Now().Add(expiresIn),
	}, nil
}

func (s *FilesystemStore) Put(ctx context.Context, ref ObjectRef, body io.Reader, opts PutOptions) (Attrs, error) {
	if err := ctx.Err(); err != nil {
		return Attrs{}, err
	}
	p, err := s.objectPath(ref)
	if err != nil {
		return Attrs{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return Attrs{}, err
	}
	f, err := os.Create(p) //nolint:gosec // objectPath rejects traversal, absolute paths, and root escapes.
	if err != nil {
		return Attrs{}, err
	}
	h := newHashWriter(f)
	size, copyErr := io.Copy(h, body)
	closeErr := f.Close()
	if copyErr != nil {
		return Attrs{}, copyErr
	}
	if closeErr != nil {
		return Attrs{}, closeErr
	}
	attrs := Attrs{
		SizeBytes: size,
		ETag:      h.ETag(),
		MIMEType:  opts.MIMEType,
	}
	if err := writeMetadata(p, attrs); err != nil {
		return Attrs{}, err
	}
	return attrs, nil
}

func (s *FilesystemStore) Head(ctx context.Context, ref ObjectRef) (Attrs, error) {
	if err := ctx.Err(); err != nil {
		return Attrs{}, err
	}
	p, err := s.objectPath(ref)
	if err != nil {
		return Attrs{}, err
	}
	return readAttrs(p)
}

func (s *FilesystemStore) Get(ctx context.Context, ref ObjectRef) (io.ReadCloser, Attrs, error) {
	if err := ctx.Err(); err != nil {
		return nil, Attrs{}, err
	}
	p, err := s.objectPath(ref)
	if err != nil {
		return nil, Attrs{}, err
	}
	attrs, err := readAttrs(p)
	if err != nil {
		return nil, Attrs{}, err
	}
	f, err := os.Open(p) //nolint:gosec // objectPath rejects traversal, absolute paths, and root escapes.
	if err != nil {
		return nil, Attrs{}, err
	}
	return f, attrs, nil
}

func (s *FilesystemStore) List(ctx context.Context, req ListRequest) ([]ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePathPart(req.Bucket, false); err != nil {
		return nil, fmt.Errorf("objectstore filesystem bucket: %w", err)
	}
	if req.Limit < 0 {
		return nil, fmt.Errorf("objectstore filesystem limit must not be negative")
	}
	if req.Prefix != "" {
		prefixForValidation := strings.TrimSuffix(req.Prefix, "/")
		if prefixForValidation != "" {
			if err := validatePathPart(prefixForValidation, true); err != nil {
				return nil, fmt.Errorf("objectstore filesystem prefix: %w", err)
			}
		}
		if strings.HasPrefix(req.Prefix, "/") || strings.Contains(req.Prefix, "\\") {
			return nil, fmt.Errorf("objectstore filesystem prefix: unsafe prefix")
		}
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return nil, err
	}
	bucketRoot := filepath.Join(root, filepath.FromSlash(req.Bucket))
	var out []ObjectInfo
	groups := make(map[string]struct{})
	err = filepath.WalkDir(bucketRoot, func(p string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".meta.json") {
			return nil
		}
		key, err := filepath.Rel(bucketRoot, p)
		if err != nil {
			return err
		}
		key = filepath.ToSlash(key)
		if !strings.HasPrefix(key, req.Prefix) {
			return nil
		}
		if req.Delimiter != "" {
			rest := strings.TrimPrefix(key, req.Prefix)
			if index := strings.Index(rest, req.Delimiter); index >= 0 {
				groups[req.Prefix+rest[:index+len(req.Delimiter)]] = struct{}{}
				return nil
			}
		}
		attrs, err := readAttrs(p)
		if err != nil {
			return err
		}
		// The sidecar file holds the metadata; the object file holds the mtime, and that is
		// the one a person sorts "recent" by.
		if info, statErr := entry.Info(); statErr == nil {
			attrs.ModifiedAt = info.ModTime()
		}
		out = append(out, ObjectInfo{Ref: ObjectRef{Bucket: req.Bucket, Key: key}, Attrs: attrs})
		return nil
	})
	for prefix := range groups {
		out = append(out, ObjectInfo{Ref: ObjectRef{Bucket: req.Bucket, Key: prefix}})
	}
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b ObjectInfo) int {
		return strings.Compare(a.Ref.Key, b.Ref.Key)
	})
	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

// Copy reads and rewrites. A filesystem has no server-side copy to delegate to, and this
// store backs tests and single-node deployments where that cost is not the constraint.
func (s *FilesystemStore) Copy(ctx context.Context, src, dst ObjectRef) error {
	body, attrs, err := s.Get(ctx, src)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	_, err = s.Put(ctx, dst, body, PutOptions{MIMEType: attrs.MIMEType, Size: attrs.SizeBytes})
	return err
}

func (s *FilesystemStore) Delete(ctx context.Context, ref ObjectRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := s.objectPath(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(metadataPath(p)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *FilesystemStore) objectPath(ref ObjectRef) (string, error) {
	if err := validatePathPart(ref.Bucket, false); err != nil {
		return "", fmt.Errorf("objectstore filesystem bucket: %w", err)
	}
	if err := validatePathPart(ref.Key, true); err != nil {
		return "", fmt.Errorf("objectstore filesystem key: %w", err)
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, filepath.FromSlash(ref.Bucket), filepath.FromSlash(ref.Key))
	if !strings.HasPrefix(p, root+string(os.PathSeparator)) && p != root {
		return "", fmt.Errorf("unsafe path escapes root")
	}
	return p, nil
}

func validatePathPart(value string, allowSlash bool) error {
	if value == "" {
		return fmt.Errorf("empty")
	}
	if strings.Contains(value, "..") {
		return fmt.Errorf("unsafe substring %q", "..")
	}
	if strings.Contains(value, `\`) {
		return fmt.Errorf("backslash not allowed")
	}
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" || path.IsAbs(value) {
		return fmt.Errorf("absolute path not allowed")
	}
	if !allowSlash && strings.Contains(value, "/") {
		return fmt.Errorf("slash not allowed")
	}
	for part := range strings.SplitSeq(value, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("unsafe segment %q", part)
		}
	}
	return nil
}

func readAttrs(p string) (Attrs, error) {
	b, err := os.ReadFile(metadataPath(p))
	if err == nil {
		var attrs Attrs
		if err := json.Unmarshal(b, &attrs); err != nil {
			return Attrs{}, err
		}
		return attrs, nil
	}
	if !os.IsNotExist(err) {
		return Attrs{}, err
	}
	data, err := os.ReadFile(p) //nolint:gosec // p is produced by objectPath before readAttrs is called.
	if err != nil {
		return Attrs{}, err
	}
	return Attrs{SizeBytes: int64(len(data)), ETag: etag(data)}, nil
}

func writeMetadata(p string, attrs Attrs) error {
	b, err := json.Marshal(attrs)
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath(p), b, 0o600)
}

func metadataPath(p string) string {
	return p + ".meta.json"
}

type hashWriter struct {
	w    io.Writer
	data []byte
}

func newHashWriter(w io.Writer) *hashWriter {
	return &hashWriter{w: w}
}

func (w *hashWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.data = append(w.data, p[:n]...)
	return n, err
}

func (w *hashWriter) ETag() string {
	return etag(w.data)
}
