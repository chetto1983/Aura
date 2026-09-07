package skills

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

// builtinFS holds the skills Aura ships in-binary. Three builtins are embedded:
// skill-creator (D-31, the spec-compliant authoring meta-skill), find-skills-aura
// (amendment #51 / D-40, the always:true self-extension skill) and memory-aura.
// BuiltinNames reads this tree, so the count in this sentence is prose and the set is code.
//
// find-skills-aura now
// teaches DISCOVERY through the CLI (npx skills find only prints) and administrator-only
// INSTALL through skill_manage action=install: it used to teach the CLI install too, which lands the tree
// outside every loader root — the model followed the instruction, read "Installation
// complete", and ended up with a skill nothing could load.
//
//go:embed embed
var builtinFS embed.FS

// builtinRoot is the embed prefix the builtin tree lives under.
const builtinRoot = "embed"

// MaterializeBuiltins writes the embedded builtin skills into dir (the active
// AURA_SKILLS_DIR root) on first boot, so they appear in the loader's scan exactly
// like an operator-installed skill. It is fingerprint-idempotent: a file is written
// only when absent or its on-disk content differs from the embedded bytes, so a
// boot that changed nothing performs no writes and an operator edit is not clobbered
// unless the embedded bytes changed (codex-style materialization, D-31).
func MaterializeBuiltins(dir string) error {
	if dir == "" {
		return nil
	}
	return fs.WalkDir(builtinFS, builtinRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(builtinRoot, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, rerr := builtinFS.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return writeIfChanged(target, data)
	})
}

// BuiltinNames is the sorted set of skills Aura ships in-binary, read from the embedded tree
// rather than from a list somebody has to remember to update: adding a directory under
// internal/skills/embed is the whole registration.
func BuiltinNames() []string {
	entries, err := builtinFS.ReadDir(builtinRoot)
	if err != nil {
		// Unreachable with a compiled-in embed.FS; an empty set is the safe answer because it
		// only ever costs a builtin its protection, never protects something that is not one.
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// IsBuiltin reports whether this name is one of Aura's own skills.
//
// It is the answer every lifecycle surface needs: a builtin is product code, not house policy,
// and MaterializeBuiltins rewrites it at the next boot whenever the on-disk bytes differ from
// the embedded ones. Archiving or deleting one is therefore a change that undoes itself one
// restart later — so the cockpit must not offer the verb and the write path must not perform
// it. The match is exact: a name is Aura's own or it is somebody's.
func IsBuiltin(name string) bool {
	return slices.Contains(BuiltinNames(), name)
}

// writeIfChanged writes data to target only when target is absent or its content
// differs (compared by SHA-256). The parent dir is created lazily. target is built
// by MaterializeBuiltins from the operator-controlled skills dir joined with the
// embedded (compile-time-fixed) relative path — never a network/model input.
func writeIfChanged(target string, data []byte) error {
	if existing, err := os.ReadFile(target); err == nil { // #nosec G304 -- target derived from operator skills dir + embedded path
		if sha256.Sum256(existing) == sha256.Sum256(data) {
			return nil // unchanged — leave the operator's copy in place
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("materialize builtin %q: %w", target, err)
	}
	return nil
}
