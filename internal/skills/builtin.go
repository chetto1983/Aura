package skills

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// builtinFS holds the skills Aura ships in-binary. Two builtins are embedded:
// skill-creator (D-31, the spec-compliant authoring meta-skill) and find-skills-aura
// (amendment #51 / D-40, the always:true self-extension skill). find-skills-aura now
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
