package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// Materialize copies an ACTIVE skill's files from skillDir into the export dir
// (AURA_SKILL_EXPORT_DIR — the source of the read-only `/skills` bind mount the
// sandbox sees, D-17). It STRIPS symlinks via Lstat-no-follow (Pitfall 4 /
// T-11-04-T1): a symlinked file or directory inside the skill is skipped, never
// followed, so a malicious skill cannot project a host path into the sandbox mount.
//
// Only ACTIVE skills are ever materialized (D-17): pending and archived skills are
// NOT in the export dir, so the mount tracks loader state in lockstep. Dematerialize
// removes a skill from the export dir on archive/delete.
//
// The walk is a manual os.ReadDir + Lstat recursion (the loader/orphan_scan idiom,
// NOT filepath.WalkDir) so symlinked subdirectories are never descended and the
// destination is rebuilt clean each time.
func Materialize(name, skillDir, exportDir string) error {
	if name == "" || skillDir == "" || exportDir == "" {
		return fmt.Errorf("materialize: name, skillDir, and exportDir are all required")
	}
	dst := filepath.Join(exportDir, name)
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("materialize %q: clear export subtree: %w", name, err)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return fmt.Errorf("materialize %q: mkdir export: %w", name, err)
	}
	if err := copyTreeNoSymlinks(skillDir, dst); err != nil {
		return fmt.Errorf("materialize %q: %w", name, err)
	}
	return nil
}

// copyTreeNoSymlinks recursively copies regular files + dirs from src to dst,
// SKIPPING any symlinked entry (Lstat-no-follow). It never descends a symlinked
// directory. src is the operator/active-trusted skill dir; dst is exportDir/<name>.
func copyTreeNoSymlinks(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read %q: %w", src, err)
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		info, lerr := os.Lstat(srcPath)
		if lerr != nil {
			return fmt.Errorf("lstat %q: %w", srcPath, lerr)
		}
		// Symlink strip (Pitfall 4 / T-11-04-T1): skip the entry entirely — never
		// follow it, never descend it.
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		switch {
		case info.IsDir():
			if err := os.MkdirAll(dstPath, 0o750); err != nil {
				return err
			}
			if err := copyTreeNoSymlinks(srcPath, dstPath); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyRegularFile(srcPath, dstPath); err != nil {
				return err
			}
		default:
			// devices/sockets/fifos: skip.
		}
	}
	return nil
}

// copyRegularFile copies one regular file's bytes from src to dst (0o600). src was
// Lstat-confirmed a non-symlink regular file by the caller; dst is under the
// operator-controlled export dir joined with a validated skill name.
func copyRegularFile(src, dst string) error {
	data, err := os.ReadFile(src) // #nosec G304 -- src is an Lstat-confirmed regular file under the operator/active skill dir (symlink-stripped)
	if err != nil {
		return fmt.Errorf("read %q: %w", src, err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil { // #nosec G703 -- dst = exportDir/<validated-name>/<relpath>, operator-controlled root
		return fmt.Errorf("write %q: %w", dst, err)
	}
	return nil
}

// Dematerialize removes a skill's subtree from the export dir (the /skills mount
// source) on archive/delete, so the sandbox no longer sees it (D-17: mount tracks
// loader state in lockstep). A missing subtree is a no-op.
func Dematerialize(name, exportDir string) error {
	if name == "" || exportDir == "" {
		return nil
	}
	dst := filepath.Join(exportDir, name)
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("dematerialize %q: %w", name, err)
	}
	return nil
}
