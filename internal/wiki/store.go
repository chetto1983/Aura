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

	"github.com/aura/aura/internal/reindex"
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

	// gitCommitFunc is an optional override for the gitCommit function.
	// Production code leaves this nil. Set via SetGitCommitFuncForTest
	// to drive failure/success paths from external test packages.
	gitCommitFunc func(ctx context.Context, filename, action string) error

	reindexSubmitter reindex.Submitter // optional; set via SetReindexSubmitter (Phase 2 INDEX-01)
}

// PageCatalog is the read side for enumerating wiki pages.
type PageCatalog interface {
	ListPages() ([]string, error)
}

// PageReader is the read side for wiki pages and page catalogs.
type PageReader interface {
	ReadPage(slug string) (*Page, error)
	PageCatalog
}

// SlugResolver maps user/model supplied slugs or aliases to canonical pages.
type SlugResolver interface {
	ResolveSlug(input string) (resolved string, candidates []string, err error)
}

// PageWriter is the mutation side for wiki pages.
// The variadic expectedUpdatedAt enables optimistic concurrency (WIKI-02, D-02):
//   - no variadic argument:        trust-caller (legacy callers preserved, D-05)
//   - expectedUpdatedAt[0] == "":  create-only-if-absent sentinel
//   - expectedUpdatedAt[0] != "":  update-if-on-disk-updated_at-matches
type PageWriter interface {
	WritePage(ctx context.Context, page *Page, expectedUpdatedAt ...string) error
	DeletePage(ctx context.Context, slug string) error
}

// Directory exposes the wiki root for file-backed auxiliary stores.
type Directory interface {
	Dir() string
}

// Linter is the wiki health check surface.
type Linter interface {
	Lint(ctx context.Context) ([]LintIssue, error)
}

// MemoryCleaner is the automated memory hygiene surface.
type MemoryCleaner interface {
	CleanMemory(ctx context.Context, opts MemoryHygieneOptions) (*MemoryHygieneReport, error)
}

// LinkRepairer repairs broken wiki links.
type LinkRepairer interface {
	RepairLink(ctx context.Context, brokenSlug, fixedSlug string) error
}

// Maintainer is the nightly/wiki-health maintenance boundary.
type Maintainer interface {
	PageCatalog
	Linter
	MemoryCleaner
	LinkRepairer
}

// Journal is the operational index/log surface.
type Journal interface {
	RebuildIndex(ctx context.Context)
	AppendLog(ctx context.Context, action, slug string)
}

// Repository is the full wiki store boundary used by runtime wiring.
type Repository interface {
	PageReader
	SlugResolver
	PageWriter
	Directory
	Maintainer
	Journal
}

const (
	memoryDecayMediumAge = 90 * 24 * time.Hour
	memoryDecayHighAge   = 180 * 24 * time.Hour
)

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

func normalizeSlugInput(slug string) string {
	slug = strings.TrimSpace(slug)
	slug = strings.TrimSuffix(slug, ".md")
	slug = strings.TrimSuffix(slug, ".yaml")
	return Slug(slug)
}

func (s *Store) pageFileExists(slug string) bool {
	for _, ext := range []string{".md", ".yaml"} {
		if _, err := os.Stat(filepath.Join(s.dir, slug+ext)); err == nil {
			return true
		}
	}
	return false
}

// ReadPage reads a wiki page by slug.
// Tries .md first, falls back to legacy .yaml format.
func (s *Store) ReadPage(slug string) (*Page, error) {
	slug = normalizeSlugInput(slug)
	if slug == "" {
		return nil, fmt.Errorf("reading wiki page: empty slug")
	}

	// Try .md first
	mdPath := filepath.Join(s.dir, slug+".md")
	if data, err := os.ReadFile(mdPath); err == nil {
		return ParseMD(data)
	}

	// Fall back to legacy .yaml
	yamlPath := filepath.Join(s.dir, slug+".yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("reading wiki page [[%s]]: %w", slug, os.ErrNotExist)
	}
	return ParseYAML(data)
}

// ResolveSlug maps a user/model supplied slug or short alias to a canonical
// page slug. Exact pages win. Prefix aliases are accepted only when unique so
// a short guess like "golem" can recover to the real page without silently
// choosing between multiple candidates.
func (s *Store) ResolveSlug(input string) (resolved string, candidates []string, err error) {
	query := normalizeSlugInput(input)
	if query == "" {
		return "", nil, fmt.Errorf("resolving wiki slug: empty slug")
	}
	if s.pageFileExists(query) {
		return query, nil, nil
	}

	slugs, err := s.ListPages()
	if err != nil {
		return "", nil, err
	}
	for _, slug := range slugs {
		if strings.HasPrefix(slug, query+"-") {
			candidates = append(candidates, slug)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 1 {
		return candidates[0], candidates, nil
	}
	return "", candidates, nil
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
		if rest, ok := strings.CutSuffix(name, ".md"); ok {
			slug = rest
		} else if rest, ok := strings.CutSuffix(name, ".yaml"); ok {
			slug = rest
		} else {
			continue
		}
		if IsOperationalSlug(slug) {
			continue
		}
		if !seen[slug] {
			seen[slug] = true
			slugs = append(slugs, slug)
		}
	}
	return slugs, nil
}

// IsOperationalSlug reports whether a markdown file is wiki infrastructure
// rather than durable user/project memory.
func IsOperationalSlug(slug string) bool {
	switch strings.ToLower(strings.TrimSpace(slug)) {
	case "index", "log", "schema":
		return true
	default:
		return false
	}
}

// SetGitCommitFuncForTest installs a custom gitCommit implementation for
// deterministic testing of the Unversioned set/clear paths (D-17, D-18).
// Production code MUST NOT call this. It is exported (rather than a
// lowercase field) so cross-package tests in internal/api/ — which run
// under `package api` — can drive the failure path that makes the wiki
// page surface Unversioned: true through /api/wiki (Plan 07).
//
// Pass nil to remove the override and restore the production gitCommit.
func (s *Store) SetGitCommitFuncForTest(fn func(ctx context.Context, filename, action string) error) {
	s.gitCommitFunc = fn
}

// SetReindexSubmitter wires an optional reindex.Submitter. Production setup
// calls this AFTER both wiki.Store and reindex.Worker exist (avoids ctor cycle).
// Passing nil is allowed (clears the wiring; production code never does this).
// Phase 2 INDEX-01.
func (s *Store) SetReindexSubmitter(sub reindex.Submitter) {
	s.reindexSubmitter = sub
}

// gitCommit ignores ctx because go-git's Worktree API is synchronous; we
// keep the parameter so callers don't need a special case in the wiki
// write path.
func (s *Store) gitCommit(ctx context.Context, filename, action string) error {
	if s.gitCommitFunc != nil {
		return s.gitCommitFunc(ctx, filename, action)
	}
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
		if isCleanWorktreeCommitError(err) {
			return nil
		}
		return fmt.Errorf("committing %s: %w", filename, err)
	}

	s.logger.Info("wiki page committed to git", "file", filename, "action", action)
	return nil
}

func isCleanWorktreeCommitError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "nothing to commit") ||
		strings.Contains(msg, "clean working tree")
}

// Dir returns the absolute directory the store reads from. Useful for
// callers (currently internal/api) that need to walk the wiki tree for
// metadata like file mtimes without going through ReadPage.
func (s *Store) Dir() string { return s.dir }

// updateIndex regenerates index.md grouped by category.
// RebuildIndex regenerates index.md from the current set of wiki pages
// and commits the result to git. Safe to call after manual edits or
// disk-level changes that bypassed WritePage / DeletePage.
func (s *Store) RebuildIndex(ctx context.Context) {
	s.updateIndex(ctx)
}

// RebuildGraph refreshes the materialized graph files from the current pages.
func (s *Store) RebuildGraph(ctx context.Context) {
	s.updateGraph(ctx)
}

// AppendLog appends a single chronological entry (timestamp, action,
// slug) to log.md and commits it to git. Use slug="" for actions that
// don't pertain to a specific page (e.g. "lint", "query").
func (s *Store) AppendLog(ctx context.Context, action, slug string) {
	s.appendLog(ctx, action, slug)
}

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
		fmt.Fprintf(&sb, "## %s\n\n", cat)
		for _, e := range entries {
			fmt.Fprintf(&sb, "- [[%s]] %s\n", e.Slug, e.Title)
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

	s.updateGraph(ctx)
}

type indexEntry struct {
	Slug  string
	Title string
}

func (s *Store) updateGraph(ctx context.Context) {
	graph, err := BuildGraphFromReader(s)
	if err != nil {
		s.logger.Warn("failed to build wiki graph", "error", err)
		return
	}
	if err := WriteGraphFiles(s.dir, graph); err != nil {
		s.logger.Warn("failed to write wiki graph files", "error", err)
		return
	}
	if err := s.gitCommit(ctx, "graph/graph.json", "update"); err != nil {
		s.logger.Warn("git commit failed for graph.json", "error", err)
	}
	if err := s.gitCommit(ctx, "graph/context.md", "update"); err != nil {
		s.logger.Warn("git commit failed for graph context", "error", err)
	}
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
	pageCell := ""
	if slug != "" {
		pageCell = "[[" + slug + "]]"
	}
	row := fmt.Sprintf("| %s | %s | %s |\n", timestamp, action, pageCell)

	// Append row before trailing newline
	existing = strings.TrimRight(existing, "\n") + "\n" + row

	if err := os.WriteFile(logPath, []byte(existing), 0644); err != nil {
		s.logger.Warn("failed to write log.md", "error", err)
	}

	if err := s.gitCommit(ctx, "log.md", "update"); err != nil {
		s.logger.Warn("git commit failed for log.md", "error", err)
	}
}

// LintIssue represents a problem found by Lint.
type LintIssue struct {
	Slug     string
	Message  string
	Kind     string
	Severity string
}

// Lint checks the wiki for broken links, missing categories, and memory decay.
func (s *Store) Lint(ctx context.Context) ([]LintIssue, error) {
	return s.lintAt(ctx, time.Now().UTC())
}

func (s *Store) lintAt(ctx context.Context, now time.Time) ([]LintIssue, error) {
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
			issues = append(issues, LintIssue{Slug: slug, Message: "failed to read page", Kind: "read_error", Severity: "medium"})
			continue
		}

		if page.Category == "" {
			issues = append(issues, LintIssue{Slug: slug, Message: "missing category", Kind: "missing_category", Severity: "low"})
		}

		if issue, ok := memoryDecayIssue(slug, page.UpdatedAt, now); ok {
			issues = append(issues, issue)
		}

		for _, link := range ExtractWikiLinks(page.Body) {
			if !slugSet[link] {
				issues = append(issues, LintIssue{
					Slug:    slug,
					Message: fmt.Sprintf("broken link: [[%s]]", link),
					Kind:    "broken_link", Severity: "high",
				})
			}
		}

		for _, rel := range page.Related {
			if !slugSet[rel] {
				issues = append(issues, LintIssue{
					Slug:    slug,
					Message: fmt.Sprintf("broken related ref: %s", rel),
					Kind:    "broken_link", Severity: "high",
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

func memoryDecayIssue(slug, updatedAt string, now time.Time) (LintIssue, bool) {
	updated, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return LintIssue{
			Slug: slug, Message: "invalid updated_at for decay check",
			Kind: "invalid_metadata", Severity: "medium",
		}, true
	}
	age := now.Sub(updated)
	if age < memoryDecayMediumAge {
		return LintIssue{}, false
	}
	days := int(age.Hours() / 24)
	severity := "medium"
	if age >= memoryDecayHighAge {
		severity = "high"
	}
	decay := age.Hours() / memoryDecayHighAge.Hours()
	if decay > 1 {
		decay = 1
	}
	return LintIssue{
		Slug:     slug,
		Message:  fmt.Sprintf("memory decay: updated_at %s is %d days old (decay=%.2f)", updated.UTC().Format(time.RFC3339), days, decay),
		Kind:     "memory_decay",
		Severity: severity,
	}, true
}

func sortedCategoryKeys(m map[string][]indexEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
