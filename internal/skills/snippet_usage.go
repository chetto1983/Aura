package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// --- usage sidecar (D-19): the ONE live-state source, atomic-write ---

// UsageSidecar is the per-skill usage/archive state recorded next to the active skill
// (D-19): the live-state source the TTL sweep scans (small N). It is written
// atomically (temp+rename). snippet_runs DB forensics are intentionally NOT shipped
// here (optional/discretion, D-19/A4) — the sidecar is sufficient for v1 + the sweep.
type UsageSidecar struct {
	Status     string    `json:"status"`       // active | archived
	LastUsedAt time.Time `json:"last_used_at"` // zero until first use
	UseCount   int       `json:"use_count"`
}

// usageSidecarPath is the per-skill usage JSON path: <activeRoot>/<name>/.usage.json.
// It lives inside the skill dir so it travels with promote/archive moves and the TTL
// sweep finds it by scanning the active root (D-19). The dotfile name keeps it out of
// the materialized export tree's interpreter target.
func (w *Writer) usageSidecarPath(name string) string {
	return filepath.Join(w.activeDir, name, ".usage.json")
}

// ReadUsage loads a skill's usage sidecar. A missing sidecar is NOT an error — it
// returns a zero-value UsageSidecar (status active, never used), so the TTL sweep
// treats a never-used snippet by its on-disk mtime path (the caller's policy).
func (w *Writer) ReadUsage(name string) (UsageSidecar, error) {
	data, err := os.ReadFile(w.usageSidecarPath(name)) // #nosec G304 -- path = activeRoot/<name>/.usage.json
	if err != nil {
		if os.IsNotExist(err) {
			return UsageSidecar{Status: "active"}, nil
		}
		return UsageSidecar{}, fmt.Errorf("read usage %q: %w", name, err)
	}
	var u UsageSidecar
	if jerr := json.Unmarshal(data, &u); jerr != nil {
		return UsageSidecar{}, fmt.Errorf("read usage %q: %w", name, jerr)
	}
	if u.Status == "" {
		u.Status = "active"
	}
	return u, nil
}

// writeUsageAtomic writes the usage sidecar atomically (temp file in the same dir +
// rename), so a crash mid-write never leaves a half-written sidecar the sweep reads.
func (w *Writer) writeUsageAtomic(name string, u UsageSidecar) error {
	dir := filepath.Join(w.activeDir, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("usage %q: mkdir: %w", name, err)
	}
	data, err := json.Marshal(u)
	if err != nil {
		return fmt.Errorf("usage %q: marshal: %w", name, err)
	}
	tmp, err := os.CreateTemp(dir, ".usage-tmp-*")
	if err != nil {
		return fmt.Errorf("usage %q: temp: %w", name, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if _, werr := tmp.Write(data); werr != nil {
		_ = tmp.Close()
		return fmt.Errorf("usage %q: write temp: %w", name, werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return fmt.Errorf("usage %q: close temp: %w", name, cerr)
	}
	if rerr := os.Rename(tmpName, w.usageSidecarPath(name)); rerr != nil {
		return fmt.Errorf("usage %q: rename: %w", name, rerr)
	}
	committed = true
	return nil
}

// StampUsage bumps use_count + last_used_at on a skill's usage sidecar atomically
// (D-19). It is the deterministic stamp the CLI `aura skills snippet exec` path calls
// after a successful by-path run; a model-run stamp via a sandbox_exec /skills-prefix
// argv hook is a documented fast-follow (D-04 discretion). now is injectable for tests.
func (w *Writer) StampUsage(name string, now time.Time) error {
	u, err := w.ReadUsage(name)
	if err != nil {
		return err
	}
	u.UseCount++
	u.LastUsedAt = now.UTC()
	if u.Status == "" {
		u.Status = "active"
	}
	return w.writeUsageAtomic(name, u)
}

// SetUsageStatus rewrites only the status field (active|archived), preserving the
// use counters. The TTL sweep calls it (archived) when it de-materializes a snippet
// past the TTL so a later scan does not re-process an already-archived skill.
func (w *Writer) SetUsageStatus(name, status string) error {
	u, err := w.ReadUsage(name)
	if err != nil {
		return err
	}
	u.Status = status
	return w.writeUsageAtomic(name, u)
}

// SweepResult summarizes one TTL sweep pass (D-16): the snippets it archived and the
// ones it kept (still fresh), so the cron handler returns a count summary.
type SweepResult struct {
	Archived []string
	Kept     []string
}

// SweepExpiredSnippets archives every ACTIVE snippet whose last use is older than the
// TTL (D-16). For each stale snippet it calls Archive(ApprovalAuto) — de-materialize
// from the /skills mount + move active->archived + the D-29 `auto` audit row
// (gate_recommended=false, gate_taken=true) — then marks the usage sidecar archived so
// a later pass skips it. Staleness is the usage sidecar's last_used_at; a never-used
// snippet falls back to its SKILL.md mtime (its install time), so a snippet sitting
// unused past the TTL is still swept. It is best-effort per skill (log-and-skip on a
// per-skill error so one bad snippet never aborts the sweep). ttl<=0 disables the
// sweep (returns empty). now is injectable for tests.
func (w *Writer) SweepExpiredSnippets(ctx context.Context, ttl time.Duration, now time.Time, actor AuditActor) (SweepResult, error) {
	var res SweepResult
	if ttl <= 0 || w.activeDir == "" {
		return res, nil
	}
	cutoff := now.Add(-ttl)
	entries, err := os.ReadDir(w.activeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, fmt.Errorf("sweep snippets: read active root: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || name == "pending" || name == "archived" {
			continue
		}
		if err := SanitizeName(name, name); err != nil {
			continue // not a valid skill dir name; skip
		}
		stale, ok := w.snippetIsStale(name, cutoff)
		if !ok {
			continue // not a snippet, or already archived — skip
		}
		if !stale {
			res.Kept = append(res.Kept, name)
			continue
		}
		if aerr := w.Archive(ctx, name, ApprovalAuto, actor); aerr != nil {
			slog.Warn("snippet TTL sweep: archive failed", "skill", name, "err", aerr)
			continue
		}
		_ = w.SetUsageStatus(name, "archived") // sidecar moved with the dir; mark archived
		res.Archived = append(res.Archived, name)
	}
	return res, nil
}

// snippetIsStale reports whether the named ACTIVE skill is a snippet past its TTL
// cutoff. ok=false when the skill is not a snippet or its usage is already archived
// (the sweep skips it). last_used_at drives staleness; a never-used snippet falls back
// to the SKILL.md mtime so it is still eligible.
func (w *Writer) snippetIsStale(name string, cutoff time.Time) (stale, ok bool) {
	skillMd := filepath.Join(w.activeDir, name, "SKILL.md")
	raw, err := os.ReadFile(skillMd) // #nosec G304 -- activeDir/<SanitizeName-validated name>/SKILL.md
	if err != nil {
		return false, false
	}
	fm, _, perr := parseFrontmatter(raw)
	if perr != nil || fm.Type != TypeSnippet {
		return false, false
	}
	u, uerr := w.ReadUsage(name)
	if uerr == nil && u.Status == "archived" {
		return false, false // already swept
	}
	last := time.Time{}
	if uerr == nil {
		last = u.LastUsedAt
	}
	if last.IsZero() {
		if info, serr := os.Stat(skillMd); serr == nil {
			last = info.ModTime()
		}
	}
	if last.IsZero() {
		return false, true // unknown age, keep (conservative)
	}
	return last.Before(cutoff), true
}
