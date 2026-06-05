package skills

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
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
	Body        string
	Dir         string
}

// Config configures a Loader. Roots are scanned in order with later-root-wins
// precedence on a name collision (an operator override placed in a later root
// shadows a builtin in an earlier one). CacheTTL/BodyCapBytes fall back to the
// package defaults when zero.
type Config struct {
	Roots        []string
	CacheTTL     time.Duration
	BodyCapBytes int
}

// Loader scans the configured roots for SKILL.md files and caches the parsed,
// structurally-valid skills behind a short TTL. It is safe for concurrent use.
// Validation is parse + name-regex + body-cap ONLY — NO blocklist (D-28: disk is
// operator-trusted; blocklist enforcement lives at the write boundary, plan 11-04).
type Loader struct {
	roots   []string
	ttl     time.Duration
	bodyCap int

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
	return &Loader{roots: roots, ttl: ttl, bodyCap: cap}
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

// loadSkillDir reads <dir>/SKILL.md, parses it, and validates STRUCTURE only
// (D-28): name regex + name==dirName + body cap. A failure at any step is
// skip-logged and the skill is excluded (ok=false). NO blocklist is consulted.
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
	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Always:      fm.Always,
		Type:        fm.Type,
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
	if fm.Description == "" {
		return fmt.Errorf("missing description")
	}
	if fm.Type != TypeInstruction && fm.Type != TypeSnippet {
		return fmt.Errorf("invalid type %q", fm.Type)
	}
	if len(body) > bodyCap {
		return fmt.Errorf("body %d bytes exceeds cap %d", len(body), bodyCap)
	}
	return nil
}
