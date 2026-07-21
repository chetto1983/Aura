package agui

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ManifestVersion is the owner-export manifest wire version.
const ManifestVersion = 1

const defaultOwnerExportLimit int64 = 1 << 30

const defaultOwnerExportRetention = 30 * 24 * time.Hour

// ErrOwnerExportNotFound hides absent and foreign ownership identically.
var ErrOwnerExportNotFound = errors.New("owner export not found")

// ExportArtifact is one owner-scoped referenced asset descriptor.
type ExportArtifact struct {
	ID       string
	Filename string
	MIMEType string
	Size     int64
}

// ExportSnapshot is the source's consistent owner-scoped snapshot.
type ExportSnapshot struct {
	IdentitySnapshotID     string
	ConversationSnapshotID string
	ConversationJSON       []byte
	Artifacts              []ExportArtifact
	Omissions              []string
	ConversationVersion    time.Time
	Release                func()
}

// ManifestEntry records one archive member's verified digest.
type ManifestEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// ExportManifest is the versioned integrity contract inside each archive.
type ExportManifest struct {
	ManifestVersion        int             `json:"manifest_version"`
	SchemaVersion          int             `json:"schema_version"`
	AuraVersion            string          `json:"aura_version"`
	PolicyVersion          string          `json:"policy_version"`
	IdentitySnapshotID     string          `json:"identity_snapshot_id"`
	ConversationSnapshotID string          `json:"conversation_snapshot_id"`
	CreatedAt              time.Time       `json:"created_at"`
	Entries                []ManifestEntry `json:"entries"`
	Omissions              []string        `json:"omissions"`
	Errors                 []string        `json:"errors"`
}

// ExportResult identifies a verified published archive.
type ExportResult struct {
	ExportID  string
	Size      int64
	SHA256    string
	ExpiresAt time.Time
	Manifest  ExportManifest
}

// OwnerExportSource provides a consistent snapshot and owner-scoped asset streams.
type OwnerExportSource interface {
	Snapshot(context.Context, string, string) (ExportSnapshot, error)
	OpenArtifact(context.Context, string, string) (io.ReadCloser, error)
}

// ExportDestination atomically publishes and reopens an archive for verification.
type ExportDestination interface {
	Publish(context.Context, string, string, io.Reader, int64, string, time.Time) error
	Open(context.Context, string, string, time.Time) (io.ReadCloser, error)
}

// OwnerDeleteLifecycle is the canonical ordered teardown seam.
type OwnerDeleteLifecycle interface {
	DeleteConversationLifecycle(context.Context, string, string) (int64, error)
}

type ownerConditionalDeleteLifecycle interface {
	DeleteConversationLifecycleIfVersion(context.Context, string, string, time.Time) (int64, error)
}

// OwnerExporter builds, publishes, verifies, and optionally tears down owner data.
type OwnerExporter struct {
	Source          OwnerExportSource
	Destination     ExportDestination
	Deleter         OwnerDeleteLifecycle
	AuraVersion     string
	PolicyVersion   string
	MaxArchiveBytes int64
	Retention       time.Duration
	Now             func() time.Time
}

// Export builds a bounded archive, publishes it atomically, then rereads and verifies it.
func (e *OwnerExporter) Export(ctx context.Context, ownerID, conversationID string) (result ExportResult, err error) {
	snapshot, err := e.acquireSnapshot(ctx, ownerID, conversationID)
	if err != nil {
		return result, err
	}
	if snapshot.Release != nil {
		defer snapshot.Release()
	}
	return e.exportSnapshot(ctx, ownerID, snapshot)
}

func (e *OwnerExporter) acquireSnapshot(ctx context.Context, ownerID, conversationID string) (ExportSnapshot, error) {
	if e == nil || e.Source == nil || e.Destination == nil || e.PolicyVersion == "" {
		return ExportSnapshot{}, errors.New("owner export is not configured")
	}
	if ownerID == "" || conversationID == "" {
		return ExportSnapshot{}, ErrOwnerExportNotFound
	}
	return e.Source.Snapshot(ctx, ownerID, conversationID)
}

func (e *OwnerExporter) exportSnapshot(ctx context.Context, ownerID string, snapshot ExportSnapshot) (result ExportResult, err error) {
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now().UTC()
	}
	retention := e.Retention
	if retention <= 0 {
		retention = defaultOwnerExportRetention
	}
	expiresAt := now.Add(retention)
	manifest := ExportManifest{
		ManifestVersion: ManifestVersion, SchemaVersion: 1, AuraVersion: e.AuraVersion,
		PolicyVersion: e.PolicyVersion, IdentitySnapshotID: snapshot.IdentitySnapshotID,
		ConversationSnapshotID: snapshot.ConversationSnapshotID, CreatedAt: now,
		Omissions: slices.Clone(snapshot.Omissions), Errors: []string{},
	}
	slices.Sort(manifest.Omissions)

	temp, err := os.CreateTemp("", "aura-owner-export-*.zip")
	if err != nil {
		return result, fmt.Errorf("create owner export: %w", err)
	}
	name := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(name)
	}()

	writer := zip.NewWriter(temp)
	entry, err := writeExportEntry(writer, "conversation.json", bytes.NewReader(snapshot.ConversationJSON), int64(len(snapshot.ConversationJSON)), now)
	if err != nil {
		return result, err
	}
	manifest.Entries = append(manifest.Entries, entry)
	artifacts := slices.Clone(snapshot.Artifacts)
	slices.SortFunc(artifacts, func(a, b ExportArtifact) int {
		return strings.Compare(a.ID+"\x00"+a.Filename, b.ID+"\x00"+b.Filename)
	})
	for _, artifact := range artifacts {
		path, pathErr := exportArtifactPath(artifact)
		if pathErr != nil {
			return result, pathErr
		}
		body, openErr := e.Source.OpenArtifact(ctx, ownerID, artifact.ID)
		if openErr != nil {
			return result, fmt.Errorf("open owner export artifact: %w", openErr)
		}
		entry, writeErr := writeExportEntry(writer, path, body, artifact.Size, now)
		closeErr := body.Close()
		if writeErr != nil {
			return result, writeErr
		}
		if closeErr != nil {
			return result, fmt.Errorf("close owner export artifact: %w", closeErr)
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	slices.SortFunc(manifest.Entries, func(a, b ManifestEntry) int { return strings.Compare(a.Path, b.Path) })
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return result, fmt.Errorf("marshal owner export manifest: %w", err)
	}
	if _, err = writeExportEntry(writer, "manifest.json", bytes.NewReader(manifestBytes), int64(len(manifestBytes)), now); err != nil {
		return result, err
	}
	if err = writer.Close(); err != nil {
		return result, fmt.Errorf("close owner export archive: %w", err)
	}
	if err = temp.Sync(); err != nil {
		return result, fmt.Errorf("sync owner export archive: %w", err)
	}
	stat, err := temp.Stat()
	if err != nil {
		return result, fmt.Errorf("stat owner export archive: %w", err)
	}
	limit := e.MaxArchiveBytes
	if limit <= 0 {
		limit = defaultOwnerExportLimit
	}
	if stat.Size() > limit {
		return result, errors.New("owner export exceeds archive limit")
	}
	if err = verifyExportArchive(name, manifest); err != nil {
		return result, err
	}
	archiveHash, err := hashFile(temp)
	if err != nil {
		return result, err
	}
	exportID := uuid.NewString()
	if _, err = temp.Seek(0, io.SeekStart); err != nil {
		return result, fmt.Errorf("rewind owner export archive: %w", err)
	}
	if err = e.Destination.Publish(ctx, ownerID, exportID, temp, stat.Size(), archiveHash, expiresAt); err != nil {
		return result, fmt.Errorf("publish owner export: %w", err)
	}
	if err = e.verifyPublished(ctx, ownerID, exportID, stat.Size(), archiveHash, manifest, expiresAt); err != nil {
		return result, err
	}
	return ExportResult{ExportID: exportID, Size: stat.Size(), SHA256: archiveHash, ExpiresAt: expiresAt, Manifest: manifest}, nil
}

// ExportDelete invokes canonical teardown only after Export has been reread and verified.
func (e *OwnerExporter) ExportDelete(ctx context.Context, ownerID, conversationID string) (ExportResult, error) {
	if e == nil || e.Deleter == nil {
		return ExportResult{}, errors.New("owner export-delete is not configured")
	}
	snapshot, err := e.acquireSnapshot(ctx, ownerID, conversationID)
	if err != nil {
		return ExportResult{}, err
	}
	if snapshot.Release != nil {
		defer snapshot.Release()
	}
	result, err := e.exportSnapshot(ctx, ownerID, snapshot)
	if err != nil {
		return ExportResult{}, err
	}
	var affected int64
	if conditional, ok := e.Deleter.(ownerConditionalDeleteLifecycle); ok {
		if snapshot.ConversationVersion.IsZero() {
			return result, errors.New("owner export-delete snapshot version is unavailable")
		}
		affected, err = conditional.DeleteConversationLifecycleIfVersion(ctx, ownerID, conversationID, snapshot.ConversationVersion)
	} else {
		affected, err = e.Deleter.DeleteConversationLifecycle(ctx, ownerID, conversationID)
	}
	if err != nil {
		return result, fmt.Errorf("owner export-delete lifecycle: %w", err)
	}
	if affected != 1 {
		return result, ErrOwnerExportNotFound
	}
	return result, nil
}

func (e *OwnerExporter) verifyPublished(ctx context.Context, ownerID, exportID string, size int64, archiveHash string, manifest ExportManifest, expiresAt time.Time) error {
	body, err := e.Destination.Open(ctx, ownerID, exportID, expiresAt)
	if err != nil {
		return fmt.Errorf("reopen owner export: %w", err)
	}
	defer func() { _ = body.Close() }()
	temp, err := os.CreateTemp("", "aura-owner-export-verify-*.zip")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = temp.Close(); _ = os.Remove(name) }()
	h := sha256.New()
	n, err := io.Copy(temp, io.TeeReader(io.LimitReader(body, size+1), h))
	if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != archiveHash {
		return errors.New("published owner export failed size or checksum verification")
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return verifyExportArchive(name, manifest)
}

func writeExportEntry(writer *zip.Writer, path string, body io.Reader, expectedSize int64, modified time.Time) (ManifestEntry, error) {
	if expectedSize < 0 {
		return ManifestEntry{}, errors.New("owner export entry has invalid size")
	}
	header := &zip.FileHeader{Name: path, Method: zip.Deflate, Modified: modified}
	destination, err := writer.CreateHeader(header)
	if err != nil {
		return ManifestEntry{}, fmt.Errorf("create owner export entry: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(destination, h), io.LimitReader(body, expectedSize+1))
	if err != nil {
		return ManifestEntry{}, fmt.Errorf("write owner export entry: %w", err)
	}
	if n != expectedSize {
		return ManifestEntry{}, errors.New("owner export entry size changed")
	}
	return ManifestEntry{Path: path, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func exportArtifactPath(artifact ExportArtifact) (string, error) {
	if artifact.ID == "" || strings.ContainsAny(artifact.ID, `/\\`) || artifact.Filename == "" ||
		strings.ContainsAny(artifact.Filename, `/\\`) || filepath.Base(artifact.Filename) != artifact.Filename ||
		!utf8.ValidString(artifact.Filename) || strings.IndexFunc(artifact.Filename, unicode.IsControl) >= 0 {
		return "", errors.New("owner export artifact identifier is unsafe")
	}
	return "assets/" + artifact.ID + "/" + artifact.Filename, nil
}

func verifyExportArchive(path string, expected ExportManifest) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open owner export archive: %w", err)
	}
	defer func() { _ = reader.Close() }()
	want := make(map[string]ManifestEntry, len(expected.Entries))
	for _, entry := range expected.Entries {
		want[entry.Path] = entry
	}
	manifestSeen := false
	for _, file := range reader.File {
		if file.Name == "manifest.json" {
			manifestSeen = true
			rc, openErr := file.Open()
			if openErr != nil {
				return openErr
			}
			var got ExportManifest
			decodeErr := json.NewDecoder(rc).Decode(&got)
			_ = rc.Close()
			if decodeErr != nil || !manifestsEqual(got, expected) {
				return errors.New("owner export manifest verification failed")
			}
			continue
		}
		entry, ok := want[file.Name]
		if !ok {
			return errors.New("owner export contains an unlisted entry")
		}
		rc, openErr := file.Open()
		if openErr != nil {
			return openErr
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, io.LimitReader(rc, entry.Size+1))
		_ = rc.Close()
		if copyErr != nil || n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
			return errors.New("owner export entry verification failed")
		}
		delete(want, file.Name)
	}
	if !manifestSeen || len(want) != 0 {
		return errors.New("owner export is incomplete")
	}
	return nil
}

func manifestsEqual(left, right ExportManifest) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func hashFile(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// MemoryExportDestination is a bounded in-memory HTTP delivery adapter.
type MemoryExportDestination struct {
	mu      sync.Mutex
	max     int64
	items   map[string][]byte
	failPut error
}

// NewMemoryExportDestination constructs a bounded destination.
func NewMemoryExportDestination(maxBytes int64) *MemoryExportDestination {
	return &MemoryExportDestination{max: maxBytes, items: map[string][]byte{}}
}

// Publish verifies bytes before atomically making the archive visible.
func (d *MemoryExportDestination) Publish(_ context.Context, ownerID, id string, body io.Reader, size int64, checksum string, expiresAt time.Time) error {
	if d == nil || ownerID == "" || id == "" || expiresAt.IsZero() || size < 0 || size > d.max {
		return errors.New("owner export destination limit exceeded")
	}
	if d.failPut != nil {
		return d.failPut
	}
	data, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil || int64(len(data)) != size {
		return errors.New("owner export destination size mismatch")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != checksum {
		return errors.New("owner export destination checksum mismatch")
	}
	d.mu.Lock()
	d.items[ownerExportMemoryKey(ownerID, id, expiresAt)] = slices.Clone(data)
	d.mu.Unlock()
	return nil
}

// Open returns a private copy of the published archive.
func (d *MemoryExportDestination) Open(_ context.Context, ownerID, id string, expiresAt time.Time) (io.ReadCloser, error) {
	d.mu.Lock()
	data, ok := d.items[ownerExportMemoryKey(ownerID, id, expiresAt)]
	copyData := slices.Clone(data)
	d.mu.Unlock()
	if !ok {
		return nil, ErrOwnerExportNotFound
	}
	return io.NopCloser(bytes.NewReader(copyData)), nil
}

func ownerExportMemoryKey(ownerID, id string, expiresAt time.Time) string {
	return ownerID + ":" + id + ":" + fmt.Sprint(expiresAt.Unix())
}

// Count reports the number of published archives.
func (d *MemoryExportDestination) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.items)
}
