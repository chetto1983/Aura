package skills

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
)

// ContentHash is Aura's OWN canonical skill-content hash (D-15 / D-23): a
// SHA-256 over the byte-sorted (relPath, bytes) pairs of a skill's files. It is
// the recovery path recorded on every audit row and the install TOFU pin. It is
// deliberately NOT interoperable with upstream skills-lock.json `computedHash`,
// which is locale/platform-sensitive (spike 004b). The writer (single-file skill)
// and the installer (cloned tree) share this one helper so a create-then-reinstall
// of identical content produces an identical hash.
//
// The encoding is collision-resistant across path/content boundaries: for each
// file we feed the path length (uint32 LE), the path bytes, the content length
// (uint64 LE), then the content bytes — so neither a path nor a content boundary
// can be ambiguously reinterpreted.

// HashSkillFiles hashes an explicit map of relPath -> content. Used by the writer
// for a single-file (SKILL.md-only) skill and by tests; the relPaths are sorted
// so iteration order never affects the result.
func HashSkillFiles(files map[string][]byte) string {
	// Re-key on slash-normalized paths so a Windows backslash and a slash form of
	// the same relpath hash identically, then sort for order-independence.
	norm := make(map[string][]byte, len(files))
	paths := make([]string, 0, len(files))
	for p, content := range files {
		key := filepath.ToSlash(p)
		norm[key] = content
		paths = append(paths, key)
	}
	sort.Strings(paths)

	h := sha256.New()
	var lenBuf [8]byte
	for _, p := range paths {
		content := norm[p]
		binary.LittleEndian.PutUint32(lenBuf[:4], uint32(len(p)))
		_, _ = h.Write(lenBuf[:4])
		_, _ = h.Write([]byte(p))
		binary.LittleEndian.PutUint64(lenBuf[:8], uint64(len(content)))
		_, _ = h.Write(lenBuf[:8])
		_, _ = h.Write(content)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// HashSkillDir walks dir and hashes every regular file under it (paths relative to
// dir, slash-normalized), NOT following symlinks (Lstat-no-follow — a symlinked
// file is skipped, never traversed, mirroring the materialize symlink-strip). It is
// the installer's content pin over a cloned tree. The walk is a manual os.ReadDir +
// Lstat recursion (the loader/materialize idiom, NOT filepath.WalkDir) so symlinked
// subdirectories are never descended.
func HashSkillDir(dir string) (string, error) {
	files := make(map[string][]byte)
	if err := collectFilesNoSymlinks(dir, dir, files); err != nil {
		return "", err
	}
	return HashSkillFiles(files), nil
}

// collectFilesNoSymlinks recursively reads regular files under cur into out, keyed
// by their path relative to base (slash-normalized), skipping any symlinked entry.
func collectFilesNoSymlinks(base, cur string, out map[string][]byte) error {
	entries, err := os.ReadDir(cur)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(cur, e.Name())
		info, lerr := os.Lstat(path)
		if lerr != nil {
			return lerr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue // symlink-strip: never hash a link target (Pitfall 4)
		}
		switch {
		case info.IsDir():
			if err := collectFilesNoSymlinks(base, path, out); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			rel, rerr := filepath.Rel(base, path)
			if rerr != nil {
				return rerr
			}
			data, rerr := os.ReadFile(path) // #nosec G304 -- path is an Lstat-confirmed regular file under the operator/installer-controlled skill dir (symlink-stripped)
			if rerr != nil {
				return rerr
			}
			out[filepath.ToSlash(rel)] = data
		}
	}
	return nil
}
