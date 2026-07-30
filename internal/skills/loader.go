package skills

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// defaultCacheTTL is the snapshot freshness window. A read within this window
// returns the cached scan; past it the next read re-scans lazily (NO background
// goroutine, so goleak stays clean — the cache is a mutex-guarded snapshot).
const defaultCacheTTL = time.Second

// defaultBodyCapBytes is the per-skill body size cap when the loader is built
// with 0 (D-34 default). An oversized body bloats the manifest/context (T-11-02-D1).
const defaultBodyCapBytes = 32768

// Skill is one loaded, structurally-valid skill: its frontmatter plus the markdown
// body and the absolute directory it was scanned from. The body is what the `use`
// action wraps in the authority frame; Name/Description feed the manifest.
type Skill struct {
	Name        string
	Description string
	Always      bool
	Type        string
	Language    string // snippet code language (D-20); empty for instruction skills
	Body        string
	Dir         string
}

// Config configures a Loader. Roots are scanned in order with later-root-wins
// precedence on a name collision (an operator override placed in a later root
// shadows a builtin in an earlier one). CacheTTL/BodyCapBytes fall back to the
// package defaults when zero. Blocklist is the load-time NFKC+literal injection
// scan applied to every body AND frontmatter description entering the manifest/
// always-block (amendment #51 / D-40): self-installed skills land directly on
// disk via the sandbox CLI without passing the Writer, so the blocklist must
// also run at load. An empty Blocklist makes the scan a no-op.
type Config struct {
	Roots        []string
	CacheTTL     time.Duration
	BodyCapBytes int
	Blocklist    []string
}

// Loader scans the configured roots for SKILL.md files and caches the parsed,
// structurally-valid skills behind a short TTL. It is safe for concurrent use.
// Validation is parse + name-regex + body-cap, plus — when a blocklist is
// configured — a load-time NFKC+literal injection scan of every body AND
// frontmatter description (amendment #51 / D-40): a self-installed skill lands on
// disk via the sandbox CLI without passing the Writer, so the blocklist must also
// run here. An empty blocklist preserves the original D-28 parse-only behavior
// (disk is operator-trusted), keeping every loader built without a blocklist green.
type Loader struct {
	roots     []string
	ttl       time.Duration
	bodyCap   int
	blocklist []string

	mu        sync.Mutex
	snapshot  map[string]Skill
	order     []string // sorted skill names, snapshot-consistent
	scannedAt time.Time
	scanned   bool
}

// NewLoader builds a Loader from cfg, applying defaults for unset fields.
func NewLoader(cfg Config) *Loader {
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	cap := cfg.BodyCapBytes
	if cap <= 0 {
		cap = defaultBodyCapBytes
	}
	roots := make([]string, len(cfg.Roots))
	copy(roots, cfg.Roots)
	blocklist := make([]string, len(cfg.Blocklist))
	copy(blocklist, cfg.Blocklist)
	return &Loader{roots: roots, ttl: ttl, bodyCap: cap, blocklist: blocklist}
}

// List returns the loaded skills in stable (name-sorted) order, re-scanning if the
// cache is stale. The returned slice is a fresh copy the caller may retain.
func (l *Loader) List() []Skill {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refreshLocked()
	out := make([]Skill, 0, len(l.order))
	for _, name := range l.order {
		out = append(out, l.snapshot[name])
	}
	return out
}

// Get returns the named skill, re-scanning if the cache is stale.
func (l *Loader) Get(name string) (Skill, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refreshLocked()
	s, ok := l.snapshot[name]
	return s, ok
}

// Invalidate expires the cached snapshot immediately. The next List/Get rescans
// synchronously, which makes a completed Writer mutation visible in the same
// agent turn instead of waiting for the normal one-second cache TTL.
func (l *Loader) Invalidate() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.scanned = false
	l.scannedAt = time.Time{}
}

// refreshLocked re-scans the roots when the snapshot is absent or older than the
// TTL. Caller holds l.mu. The lazy-on-read design keeps the loader goroutine-free.
func (l *Loader) refreshLocked() {
	if l.scanned && time.Since(l.scannedAt) < l.ttl {
		return
	}
	snap := l.scan()
	names := make([]string, 0, len(snap))
	for name := range snap {
		names = append(names, name)
	}
	sort.Strings(names)
	l.snapshot = snap
	l.order = names
	l.scannedAt = time.Now()
	l.scanned = true
}

// scan walks each root in order and merges the structurally-valid skills, with
// later roots overriding earlier ones on a name collision. Invalid skills are
// skip-logged (slog.Warn — auditable, T-11-02-R1) and excluded, never fatal.
func (l *Loader) scan() map[string]Skill {
	out := make(map[string]Skill)
	for _, root := range l.roots {
		l.scanRoot(root, out)
	}
	return out
}

// scanRoot reads the immediate child directories of root; each child that holds a
// SKILL.md is parsed into a Skill. A symlinked entry is stripped (Lstat-no-follow,
// Pitfall 4 / T-11-02-T1) so a malicious skill symlink cannot redirect the scan
// outside root.
func (l *Loader) scanRoot(root string, out map[string]Skill) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("skills loader: read root failed", "root", root, "err", err)
		}
		return
	}
	for _, e := range entries {
		full := filepath.Join(root, e.Name())

		// Symlink strip (Pitfall 4): Lstat does NOT follow — a symlinked entry is
		// skipped, never traversed to an external target.
		info, lerr := os.Lstat(full)
		if lerr != nil {
			slog.Warn("skills loader: lstat failed", "path", full, "err", lerr)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			slog.Warn("skills loader: skipping symlinked skill entry", "path", full)
			continue
		}
		if !info.IsDir() {
			continue
		}
		skill, ok := l.loadSkillDir(full, e.Name())
		if ok {
			out[skill.Name] = skill // later root wins (scan iterates roots in order)
		}
	}
}

// loadSkillDir reads <dir>/SKILL.md, parses it, and validates STRUCTURE (name
// regex + name==dirName + body cap) plus, when a blocklist is configured, the
// load-time NFKC+literal injection scan on body AND frontmatter description
// (amendment #51 / D-40). A failure at any step is skip-logged and the skill is
// excluded (ok=false), never fatal.
func (l *Loader) loadSkillDir(dir, dirName string) (Skill, bool) {
	path := filepath.Join(dir, "SKILL.md")
	info, lerr := os.Lstat(path)
	if lerr != nil {
		// No SKILL.md (or a symlinked one) → not a skill dir; silent skip (a dir
		// without SKILL.md is normal, not an error).
		return Skill{}, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		slog.Warn("skills loader: skipping symlinked SKILL.md", "path", path)
		return Skill{}, false
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is <operator-trusted root>/<dir>/SKILL.md, symlink-stripped above
	if err != nil {
		slog.Warn("skills loader: read SKILL.md failed", "path", path, "err", err)
		return Skill{}, false
	}
	fm, body, err := parseFrontmatter(raw)
	if err != nil {
		slog.Warn("skills loader: skip malformed skill", "path", path, "err", err)
		return Skill{}, false
	}
	if err := validateStructure(fm, dirName, body, l.bodyCap); err != nil {
		slog.Warn("skills loader: skip invalid skill", "path", path, "err", err)
		return Skill{}, false
	}
	// Load-time injection scan (amendment #51 / D-40): a self-installed skill body —
	// and its frontmatter description, which renders into the model-visible manifest —
	// crosses into context WITHOUT passing the Writer's blocklist. Scan both here; a
	// hit skips the skill (never loads the body) and is logged, never fatal. An empty
	// l.blocklist is a no-op (the scan finds nothing), so operator-trusted disk
	// (no blocklist configured) keeps the original parse-only behavior.
	if matched, pos, ok := violatesBlocklist(body, l.blocklist); ok {
		slog.Warn("skills loader: skipping skill with blocklisted body", "path", path, "matched", matched, "pos", pos)
		return Skill{}, false
	}
	if matched, pos, ok := violatesBlocklist(fm.Description, l.blocklist); ok {
		slog.Warn("skills loader: skipping skill with blocklisted description", "path", path, "matched", matched, "pos", pos)
		return Skill{}, false
	}
	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Always:      fm.Always,
		Type:        fm.Type,
		Language:    fm.Language,
		Body:        body,
		Dir:         dir,
	}, true
}

// validateStructure enforces the D-30/D-28 structural rules: name present and
// matching the grammar, name == directory name, description present, type in the
// enum, and body within the cap. It is deliberately blocklist-free (D-28).
func validateStructure(fm Frontmatter, dirName, body string, bodyCap int) error {
	if fm.Name == "" {
		return fmt.Errorf("missing name")
	}
	// SanitizeName is the SINGLE name chokepoint (grammar + name==dir, D-30);
	// the loader reuses it so there is exactly one name validator in the package.
	if err := SanitizeName(fm.Name, dirName); err != nil {
		return err
	}
	// Trimmed, so the rule matches ValidateForWrite's exactly: a whitespace-only
	// description is as useless in the manifest as an absent one, and two gates that
	// disagree about the same field are how an unusable skill gets written, approved
	// and then silently skipped.
	if strings.TrimSpace(fm.Description) == "" {
		return fmt.Errorf("missing description")
	}
	if fm.Type != TypeInstruction && fm.Type != TypeSnippet {
		return fmt.Errorf("invalid type %q", fm.Type)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("empty body")
	}
	if len(body) > bodyCap {
		return fmt.Errorf("body %d bytes exceeds cap %d", len(body), bodyCap)
	}
	return nil
}
