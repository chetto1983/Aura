package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	idPrefix     = "src_"
	idHexLen     = 16
	metadataFile = "source.json"
)

// idPattern is what a valid source ID looks like on disk. External callers
// may pass IDs (via tools, URLs); we never trust them for path joins until
// they match this regex.
var idPattern = regexp.MustCompile(`^src_[a-f0-9]{16}$`)

// Store persists immutable sources under <wiki>/raw/.
//
// Atomic-write + per-key mutex pattern is borrowed from internal/wiki/store.go;
// the regex-based ID validation is borrowed from picobot's memory store
// (D:\tmp\picobot\internal\agent\memory\store.go isValidMemoryFile).
type Store struct {
	rawDir string
	mu     sync.Map // id -> *sync.Mutex
	logger *slog.Logger
	now    func() time.Time
}

// NewStore creates a source store rooted at <wikiDir>/raw/. The directory is
// created if missing.
func NewStore(wikiDir string, logger *slog.Logger) (*Store, error) {
	rawDir := filepath.Join(wikiDir, "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		return nil, fmt.Errorf("source: create raw dir: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{
		rawDir: rawDir,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Store) idMutex(id string) *sync.Mutex {
	mu, _ := s.mu.LoadOrStore(id, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// PutInput is the payload for a new source.
type PutInput struct {
	Kind     Kind
	Filename string
	MimeType string
	Bytes    []byte
}

// Put stores a new source. If a source with the same content (sha256) already
// exists it returns the existing record and dup=true; the original.* file is
// not rewritten in that case. Otherwise it creates raw/<id>/, writes
// original.<ext> and source.json atomically, and returns dup=false.
func (s *Store) Put(ctx context.Context, in PutInput) (src *Source, dup bool, err error) {
	if err := validatePutInput(in); err != nil {
		return nil, false, err
	}

	sum := sha256.Sum256(in.Bytes)
	sha := hex.EncodeToString(sum[:])
	id := idPrefix + sha[:idHexLen]

	mu := s.idMutex(id)
	mu.Lock()
	defer mu.Unlock()

	if existing, err := s.getLocked(id); err == nil {
		if existing.SHA256 == sha {
			return existing, true, nil
		}
		return nil, false, fmt.Errorf("source: sha256 prefix collision for id %s", id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	sourceDir := filepath.Join(s.rawDir, id)
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("source: create source dir: %w", err)
	}

	originalName := "original" + extForKind(in.Kind)
	if err := writeFileAtomic(filepath.Join(sourceDir, originalName), in.Bytes); err != nil {
		return nil, false, fmt.Errorf("source: write original: %w", err)
	}

	rec := &Source{
		ID:        id,
		Kind:      in.Kind,
		Filename:  in.Filename,
		MimeType:  in.MimeType,
		SHA256:    sha,
		SizeBytes: int64(len(in.Bytes)),
		CreatedAt: s.now(),
		Status:    StatusStored,
	}
	if err := s.writeMetadataLocked(sourceDir, rec); err != nil {
		return nil, false, err
	}
	return rec, false, nil
}

// Get returns the source metadata for id.
func (s *Store) Get(id string) (*Source, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("source: invalid id %q", id)
	}
	mu := s.idMutex(id)
	mu.Lock()
	defer mu.Unlock()
	return s.getLocked(id)
}

func (s *Store) getLocked(id string) (*Source, error) {
	path := filepath.Join(s.rawDir, id, metadataFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec Source
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, fmt.Errorf("source: parse metadata for %s: %w", id, err)
	}
	return &rec, nil
}

// ListFilter narrows the set returned by List. Empty fields match anything.
type ListFilter struct {
	Kind   Kind
	Status Status
}

// List returns sources matching filter, sorted by CreatedAt descending. Unreadable
// or malformed source dirs are skipped with a warning, never fatal.
func (s *Store) List(filter ListFilter) ([]*Source, error) {
	entries, err := os.ReadDir(s.rawDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*Source, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !idPattern.MatchString(id) {
			continue
		}
		rec, err := s.Get(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			s.logger.Warn("source: skip unreadable source", "id", id, "err", err)
			continue
		}
		if filter.Kind != "" && rec.Kind != filter.Kind {
			continue
		}
		if filter.Status != "" && rec.Status != filter.Status {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Update applies mutator to the source under id and persists the result. The
// mutator runs while the per-id mutex is held, so two Updates on the same
// source are serialized.
func (s *Store) Update(id string, mutator func(*Source) error) (*Source, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("source: invalid id %q", id)
	}
	mu := s.idMutex(id)
	mu.Lock()
	defer mu.Unlock()
	rec, err := s.getLocked(id)
	if err != nil {
		return nil, err
	}
	if err := mutator(rec); err != nil {
		return nil, err
	}
	if err := s.writeMetadataLocked(filepath.Join(s.rawDir, id), rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Path returns the absolute path of a file inside the source dir, or "" if
// the id or name would escape the source dir. Use this when other packages
// (ocr, ingest) need to write ocr.md / ocr.json next to the source.
func (s *Store) Path(id, name string) string {
	if !idPattern.MatchString(id) {
		return ""
	}
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return ""
	}
	return filepath.Join(s.rawDir, id, name)
}

// RawDir returns the absolute path of the raw directory; useful for tests and
// for the ingest pipeline to discover source dirs.
func (s *Store) RawDir() string { return s.rawDir }

func (s *Store) writeMetadataLocked(sourceDir string, rec *Source) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("source: marshal metadata: %w", err)
	}
	return writeFileAtomic(filepath.Join(sourceDir, metadataFile), b)
}

// writeFileAtomic writes data to path via temp file + rename in the same dir,
// matching internal/wiki/store.go's WritePage pattern so partial writes never
// leak as a half-written source.json.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func extForKind(k Kind) string {
	switch k {
	case KindPDF:
		return ".pdf"
	case KindText:
		return ".txt"
	case KindURL:
		return ".url"
	case KindXLSX:
		return ".xlsx"
	}
	return ".bin"
}

func validatePutInput(in PutInput) error {
	switch in.Kind {
	case KindPDF, KindText, KindURL, KindXLSX:
	default:
		return fmt.Errorf("source: invalid kind %q", in.Kind)
	}
	if len(in.Bytes) == 0 {
		return errors.New("source: empty content")
	}
	if in.Filename == "" {
		return errors.New("source: filename required")
	}
	return nil
}
