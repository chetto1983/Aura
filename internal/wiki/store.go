package wiki

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"
)

// Store manages wiki page storage with atomic writes and git tracking.
type Store struct {
	dir    string
	mu     sync.Map   // per-file mutex: slug -> *sync.Mutex
	gitMu  sync.Mutex // serializes git operations
	logger *slog.Logger
}

// NewStore creates a wiki store rooted at the given directory.
// It initializes the git repo if one doesn't exist.
func NewStore(dir string, logger *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating wiki dir: %w", err)
	}

	s := &Store{dir: dir, logger: logger}

	if err := s.initGit(); err != nil {
		return nil, fmt.Errorf("initializing git repo: %w", err)
	}

	return s, nil
}

func (s *Store) initGit() error {
	_, err := git.PlainOpen(s.dir)
	if err == nil {
		return nil // repo already exists
	}
	if err != git.ErrRepositoryNotExists {
		return fmt.Errorf("opening git repo: %w", err)
	}

	_, err = git.PlainInit(s.dir, false)
	if err != nil {
		return fmt.Errorf("initializing git repo: %w", err)
	}

	s.logger.Info("initialized git repo in wiki directory", "dir", s.dir)
	return nil
}

func (s *Store) fileMutex(slug string) *sync.Mutex {
	mu, _ := s.mu.LoadOrStore(slug, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// WritePage atomically writes a wiki page to disk and commits it to git.
// It validates the page against the schema before writing.
func (s *Store) WritePage(ctx context.Context, page *Page) error {
	if err := Validate(page); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	slug := Slug(page.Title)
	filename := slug + ".yaml"
	path := filepath.Join(s.dir, filename)

	mu := s.fileMutex(slug)
	mu.Lock()
	defer mu.Unlock()

	// Atomic write: temp file + rename
	tmp, err := os.CreateTemp(s.dir, slug+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	data, err := yaml.Marshal(page)
	if err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("marshaling yaml: %w", err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	tmp.Close()

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	s.logger.Info("wiki page written", "slug", slug, "path", path)

	// Commit to git
	if err := s.gitCommit(ctx, filename, "update"); err != nil {
		s.logger.Error("git commit failed for wiki page", "slug", slug, "error", err)
		// Don't fail the write — git tracking is best-effort
	}

	return nil
}

// ReadPage reads a wiki page by slug.
func (s *Store) ReadPage(slug string) (*Page, error) {
	path := filepath.Join(s.dir, slug+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading wiki page %s: %w", slug, err)
	}
	return ParseYAML(data)
}

// DeletePage removes a wiki page by slug and commits the deletion to git.
func (s *Store) DeletePage(ctx context.Context, slug string) error {
	mu := s.fileMutex(slug)
	mu.Lock()
	defer mu.Unlock()

	path := filepath.Join(s.dir, slug+".yaml")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting wiki page %s: %w", slug, err)
	}

	s.logger.Info("wiki page deleted", "slug", slug)

	filename := slug + ".yaml"
	if err := s.gitCommit(ctx, filename, "delete"); err != nil {
		s.logger.Error("git commit failed for wiki page deletion", "slug", slug, "error", err)
	}

	return nil
}

// ListPages returns slugs for all wiki pages.
func (s *Store) ListPages() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("listing wiki dir: %w", err)
	}
	var slugs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		slugs = append(slugs, name)
	}
	return slugs, nil
}

func (s *Store) gitCommit(ctx context.Context, filename, action string) error {
	s.gitMu.Lock()
	defer s.gitMu.Unlock()

	repo, err := git.PlainOpen(s.dir)
	if err != nil {
		return fmt.Errorf("opening repo for commit: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	if _, err := wt.Add(filename); err != nil {
		return fmt.Errorf("staging %s: %w", filename, err)
	}

	msg := fmt.Sprintf("wiki: %s %s", action, strings.TrimSuffix(filename, ".yaml"))
	if _, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Aura",
			Email: "aura@local",
		},
	}); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") {
			return nil
		}
		return fmt.Errorf("committing %s: %w", filename, err)
	}

	s.logger.Info("wiki page committed to git", "file", filename, "action", action)
	return nil
}
