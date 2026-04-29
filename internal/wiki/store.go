package wiki

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Store manages wiki page storage with atomic writes and git tracking.
type Store struct {
	dir     string
	mu      sync.Map   // per-file mutex: slug -> *sync.Mutex
	gitMu   sync.Mutex // serializes git operations
	indexMu sync.Mutex // serializes index.md updates
	logMu   sync.Mutex // serializes log.md updates
	logger  *slog.Logger
}

// NewStore creates a wiki store rooted at the given directory.
// It initializes the git repo if one doesn't exist.
func NewStore(dir string, logger *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating wiki dir: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
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

// WritePage atomically writes a wiki page to disk as .md and commits it to git.
// It validates the page against the schema before writing.
func (s *Store) WritePage(ctx context.Context, page *Page) error {
	if err := Validate(page); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	slug := Slug(page.Title)
	filename := slug + ".md"
	path := filepath.Join(s.dir, filename)

	mu := s.fileMutex(slug)
	mu.Lock()
	defer mu.Unlock()

	// Remove legacy .yaml if it exists
	yamlPath := filepath.Join(s.dir, slug+".yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		os.Remove(yamlPath)
		s.gitCommit(ctx, slug+".yaml", "delete")
	}

	// Serialize as markdown with YAML frontmatter
	data, err := MarshalMD(page)
	if err != nil {
		return fmt.Errorf("marshaling markdown: %w", err)
	}

	// Atomic write: temp file + rename
	tmp, err := os.CreateTemp(s.dir, slug+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

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

	// Update graph files
	s.updateIndex(ctx)
	s.appendLog(ctx, "update", slug)

	// Commit to git
	if err := s.gitCommit(ctx, filename, "update"); err != nil {
		s.logger.Error("git commit failed for wiki page", "slug", slug, "error", err)
	}

	return nil
}

// ReadPage reads a wiki page by slug.
// Tries .md first, falls back to legacy .yaml format.
func (s *Store) ReadPage(slug string) (*Page, error) {
	// Try .md first
	mdPath := filepath.Join(s.dir, slug+".md")
	if data, err := os.ReadFile(mdPath); err == nil {
		return ParseMD(data)
	}

	// Fall back to legacy .yaml
	yamlPath := filepath.Join(s.dir, slug+".yaml")
	data, err := os.ReadFile(yamlPath)
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

	// Try .md first, then .yaml
	var removed bool
	var filename string
	for _, ext := range []string{".md", ".yaml"} {
		path := filepath.Join(s.dir, slug+ext)
		if err := os.Remove(path); err == nil {
			removed = true
			filename = slug + ext
			break
		}
	}

	if !removed {
		return fmt.Errorf("deleting wiki page %s: file not found", slug)
	}

	s.logger.Info("wiki page deleted", "slug", slug)
	s.updateIndex(ctx)
	s.appendLog(ctx, "delete", slug)

	if err := s.gitCommit(ctx, filename, "delete"); err != nil {
		s.logger.Error("git commit failed for wiki page deletion", "slug", slug, "error", err)
	}

	return nil
}

// ListPages returns slugs for all wiki pages.
// Scans for both .md and .yaml files.
func (s *Store) ListPages() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("listing wiki dir: %w", err)
	}
	seen := make(map[string]bool)
	var slugs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		var slug string
		if strings.HasSuffix(name, ".md") {
			slug = strings.TrimSuffix(name, ".md")
		} else if strings.HasSuffix(name, ".yaml") {
			slug = strings.TrimSuffix(name, ".yaml")
		} else {
			continue
		}
		// Skip special files
		if slug == "index" || slug == "log" {
			continue
		}
		if !seen[slug] {
			seen[slug] = true
			slugs = append(slugs, slug)
		}
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

	slug := filename
	if idx := strings.LastIndex(slug, "."); idx != -1 {
		slug = slug[:idx]
	}
	msg := fmt.Sprintf("wiki: %s %s", action, slug)
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

// updateIndex regenerates index.md grouped by category.
func (s *Store) updateIndex(ctx context.Context) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	slugs, err := s.ListPages()
	if err != nil {
		s.logger.Warn("failed to list pages for index", "error", err)
		return
	}

	byCategory := make(map[string][]indexEntry)
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil {
			continue
		}
		cat := page.Category
		if cat == "" {
			cat = "uncategorized"
		}
		byCategory[cat] = append(byCategory[cat], indexEntry{
			Slug:  slug,
			Title: page.Title,
		})
	}

	var sb strings.Builder
	sb.WriteString("# Wiki Index\n\n")
	sb.WriteString("Auto-generated catalog of all wiki pages.\n\n")

	for _, cat := range sortedCategoryKeys(byCategory) {
		entries := byCategory[cat]
		sb.WriteString(fmt.Sprintf("## %s\n\n", cat))
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("- [[%s]] %s\n", e.Slug, e.Title))
		}
		sb.WriteString("\n")
	}

	indexPath := filepath.Join(s.dir, "index.md")
	if err := os.WriteFile(indexPath, []byte(sb.String()), 0644); err != nil {
		s.logger.Warn("failed to write index.md", "error", err)
	}

	if err := s.gitCommit(ctx, "index.md", "update"); err != nil {
		s.logger.Warn("git commit failed for index.md", "error", err)
	}
}

type indexEntry struct {
	Slug  string
	Title string
}

// appendLog appends an entry to the log.md audit trail.
func (s *Store) appendLog(ctx context.Context, action, slug string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()

	logPath := filepath.Join(s.dir, "log.md")

	var existing string
	if data, err := os.ReadFile(logPath); err == nil {
		existing = string(data)
	}

	// Ensure header exists
	if !strings.Contains(existing, "# Wiki Log") {
		existing = "# Wiki Log\n\n| timestamp | action | page |\n|---|---|---|\n"
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	row := fmt.Sprintf("| %s | %s | [[%s]] |\n", timestamp, action, slug)

	// Append row before trailing newline
	existing = strings.TrimRight(existing, "\n") + "\n" + row

	if err := os.WriteFile(logPath, []byte(existing), 0644); err != nil {
		s.logger.Warn("failed to write log.md", "error", err)
	}

	if err := s.gitCommit(ctx, "log.md", "update"); err != nil {
		s.logger.Warn("git commit failed for log.md", "error", err)
	}
}

// MigrateYAMLToMD performs a one-time migration of all .yaml wiki pages to .md format.
// Returns the number of pages migrated.
func (s *Store) MigrateYAMLToMD(ctx context.Context) (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, fmt.Errorf("reading wiki dir for migration: %w", err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		slug := strings.TrimSuffix(entry.Name(), ".yaml")
		yamlPath := filepath.Join(s.dir, entry.Name())
		mdPath := filepath.Join(s.dir, slug+".md")

		// Skip if .md already exists
		if _, err := os.Stat(mdPath); err == nil {
			continue
		}

		data, err := os.ReadFile(yamlPath)
		if err != nil {
			s.logger.Warn("failed to read yaml for migration", "slug", slug, "error", err)
			continue
		}

		page, err := ParseYAML(data)
		if err != nil {
			s.logger.Warn("failed to parse yaml for migration", "slug", slug, "error", err)
			continue
		}

		// Force current schema version
		page.SchemaVersion = CurrentSchemaVersion

		mdData, err := MarshalMD(page)
		if err != nil {
			s.logger.Warn("failed to marshal md for migration", "slug", slug, "error", err)
			continue
		}

		if err := os.WriteFile(mdPath, mdData, 0644); err != nil {
			s.logger.Warn("failed to write md during migration", "slug", slug, "error", err)
			continue
		}

		os.Remove(yamlPath)
		count++

		s.logger.Info("migrated wiki page", "slug", slug, "from", "yaml", "to", "md")
	}

	if count > 0 {
		s.updateIndex(ctx)
		s.appendLog(ctx, "migrate", "batch")
		s.logger.Info("wiki migration complete", "pages_migrated", count)
	}

	return count, nil
}

// LintIssue represents a problem found by Lint.
type LintIssue struct {
	Slug    string
	Message string
}

// Lint checks the wiki for broken links, orphans, and missing categories.
func (s *Store) Lint(ctx context.Context) ([]LintIssue, error) {
	slugs, err := s.ListPages()
	if err != nil {
		return nil, err
	}

	slugSet := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		slugSet[s] = true
	}

	var issues []LintIssue
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil {
			issues = append(issues, LintIssue{Slug: slug, Message: "failed to read page"})
			continue
		}

		if page.Category == "" {
			issues = append(issues, LintIssue{Slug: slug, Message: "missing category"})
		}

		for _, link := range ExtractWikiLinks(page.Body) {
			if !slugSet[link] {
				issues = append(issues, LintIssue{
					Slug:    slug,
					Message: fmt.Sprintf("broken link: [[%s]]", link),
				})
			}
		}

		for _, rel := range page.Related {
			if !slugSet[rel] {
				issues = append(issues, LintIssue{
					Slug:    slug,
					Message: fmt.Sprintf("broken related ref: %s", rel),
				})
			}
		}
	}

	// Sort for deterministic output
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Slug != issues[j].Slug {
			return issues[i].Slug < issues[j].Slug
		}
		return issues[i].Message < issues[j].Message
	})

	return issues, nil
}

func sortedCategoryKeys(m map[string][]indexEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
