// Package plugins installs and removes a plugin as one unit: the kept tree, its skills, its
// connectors, and the record that ties them together (spec 2026-10-25-pack-install). It reads
// both formats internal/packs understands, Agent Plugins 1.0 and Claude's.
package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/packs"
)

// The phases an install record moves through. An installing record is mirrored into the
// boxes already, so a connector verified during the install finds its tree there.
const (
	PhaseInstalling = "installing"
	PhaseInstalled  = "installed"
	PhaseUpgrading  = "upgrading"
	PhaseRemoving   = "removing"
)

const (
	recordFile    = "record.json"
	treeDir       = "tree"
	backupDir     = "tree.previous"
	stagingPrefix = ".staging-"
)

// SkillEntry is one skill a plugin installed, with the content hash it landed with: removal
// deletes a skill only while its content is still what the plugin wrote.
type SkillEntry struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// Record is the only account of what a plugin installed. No skill or connector carries its
// plugin's name, so removal reads it from here.
type Record struct {
	Name        string       `json:"name"`
	Ref         string       `json:"ref"`
	Commit      string       `json:"commit"`
	Digest      string       `json:"digest"`
	Version     string       `json:"version"`
	Format      string       `json:"format"`
	InstalledBy string       `json:"installed_by"`
	InstalledAt string       `json:"installed_at"`
	Skills      []SkillEntry `json:"skills"`
	Connectors  []string     `json:"connectors"`
	Phase       string       `json:"phase"`
}

// Store keeps each installed plugin under Root/<name>: the record, and beside it the tree, so
// the record never reaches a box with the tree it describes.
type Store struct {
	Root string
}

func (s Store) dir(name string) (string, error) {
	if err := packs.ValidPluginName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.Root, name), nil
}

// TreeDir is where a plugin's kept tree lives. The name is not checked here: it comes from a
// record or a review, both of which hold only names that passed packs.ValidPluginName.
func (s Store) TreeDir(name string) string {
	return filepath.Join(s.Root, name, treeDir)
}

// Claim keeps a copy of the plugin tree at src and its first record, in installing phase, under
// rec.Name. Both are built in a staging directory that one rename puts in place, so a second
// claim of the name fails as ErrAlreadyInstalled and nothing half-written is ever visible.
func (s Store) Claim(rec Record, src string) error {
	dir, err := s.dir(rec.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Root, 0o750); err != nil {
		return fmt.Errorf("plugins dir: %w", err)
	}
	staging, err := os.MkdirTemp(s.Root, stagingPrefix+"*")
	if err != nil {
		return fmt.Errorf("plugin staging: %w", err)
	}
	placed := false
	defer func() {
		if !placed {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := copyTree(src, filepath.Join(staging, treeDir)); err != nil {
		return fmt.Errorf("keep plugin %s: %w", rec.Name, err)
	}
	rec.Phase = PhaseInstalling
	if err := writeJSON(filepath.Join(staging, recordFile), rec); err != nil {
		return err
	}
	if err := os.Rename(staging, dir); err != nil {
		if _, statErr := os.Lstat(dir); statErr == nil {
			return fmt.Errorf("%w: %s", ErrAlreadyInstalled, rec.Name)
		}
		return fmt.Errorf("place plugin %s: %w", rec.Name, err)
	}
	placed = true
	return nil
}

// Swap replaces an installed plugin's kept tree with a copy of src and keeps the old tree as a
// backup until Restore or DropBackup, as Codex's activate_marketplace_root swaps a marketplace
// root (codex-rs/core-plugins/src/marketplace_upgrade/activation.rs). A backup left by an
// upgrade that never finished is dropped first. A failed swap puts the old tree back itself.
func (s Store) Swap(name, src string) error {
	dir, err := s.dir(name)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(dir, stagingPrefix+"*")
	if err != nil {
		return fmt.Errorf("plugin staging: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := copyTree(src, filepath.Join(staging, treeDir)); err != nil {
		return fmt.Errorf("keep plugin %s: %w", name, err)
	}
	tree, backup := filepath.Join(dir, treeDir), filepath.Join(dir, backupDir)
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("drop the stale backup of %s: %w", name, err)
	}
	if err := os.Rename(tree, backup); err != nil {
		return fmt.Errorf("move the tree of %s aside: %w", name, err)
	}
	if err := os.Rename(filepath.Join(staging, treeDir), tree); err != nil {
		return errors.Join(fmt.Errorf("place the new tree of %s: %w", name, err), os.Rename(backup, tree))
	}
	return nil
}

// Restore puts the backup Swap kept back in place. Without a backup there is nothing to undo.
func (s Store) Restore(name string) error {
	dir, err := s.dir(name)
	if err != nil {
		return err
	}
	backup := filepath.Join(dir, backupDir)
	if _, err := os.Lstat(backup); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	tree := filepath.Join(dir, treeDir)
	if err := os.RemoveAll(tree); err != nil {
		return fmt.Errorf("restore plugin %s: %w", name, err)
	}
	if err := os.Rename(backup, tree); err != nil {
		return fmt.Errorf("restore plugin %s: %w", name, err)
	}
	return nil
}

// DropBackup deletes the backup Swap kept, once the upgrade it guarded has finished.
func (s Store) DropBackup(name string) error {
	dir, err := s.dir(name)
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(dir, backupDir))
}

// WriteRecord replaces an installed plugin's record.
func (s Store) WriteRecord(rec Record) error {
	dir, err := s.dir(rec.Name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("%w: %s", ErrNotInstalled, rec.Name)
	}
	return writeJSON(filepath.Join(dir, recordFile), rec)
}

// Get reads a plugin's record, reporting false when the plugin is not installed.
func (s Store) Get(name string) (Record, bool, error) {
	dir, err := s.dir(name)
	if err != nil {
		return Record{}, false, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, recordFile)) // #nosec G304 -- dir is Root joined with a name packs.ValidPluginName accepted
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read plugin record %s: %w", name, err)
	}
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Record{}, false, fmt.Errorf("parse plugin record %s: %w", name, err)
	}
	return rec, true, nil
}

// List returns every installed plugin's record, by name. A staging directory, or a directory
// with no record, is not an installed plugin.
func (s Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read plugins dir: %w", err)
	}
	var recs []Record
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || packs.ValidPluginName(e.Name()) != nil {
			continue
		}
		rec, ok, err := s.Get(e.Name())
		if err != nil {
			return nil, err
		}
		if ok {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}

// Delete removes a plugin's tree and record.
func (s Store) Delete(name string) error {
	dir, err := s.dir(name)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// writeJSON writes v through a temp file and a rename, so a reader sees the old record or the
// new one, never half of one.
func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".record-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// walkTree visits every entry under src, as the kept copy will hold it. A symlink is followed
// only to a regular file inside src; a directory symlink, a link that leaves src, and any
// other non-regular file refuse the tree. The box mirror refuses symlinks too, so the kept
// copy holds none.
func walkTree(src string, visit func(rel, from string, info fs.FileInfo) error) error {
	root, err := filepath.EvalSymlinks(src)
	if err != nil {
		return fmt.Errorf("plugin root: %w", err)
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		from := path
		if d.Type()&fs.ModeSymlink != 0 {
			if from, err = filepath.EvalSymlinks(path); err != nil {
				return fmt.Errorf("symlink %s: %w", rel, err)
			}
			if inside, _ := filepath.Rel(root, from); inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
				return fmt.Errorf("symlink %s leaves the plugin", rel)
			}
		}
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir() && from != path:
			return fmt.Errorf("symlink %s points at a directory", rel)
		case !info.IsDir() && !info.Mode().IsRegular():
			return fmt.Errorf("%s is not a regular file", rel)
		}
		return visit(rel, from, info)
	})
}

// checkTree reports whether src can be kept, without copying it.
func checkTree(src string) error {
	return walkTree(src, func(string, string, fs.FileInfo) error { return nil })
}

// copyTree copies src to dst, keeping each file's mode.
func copyTree(src, dst string) error {
	return walkTree(src, func(rel, from string, info fs.FileInfo) error {
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		return copyFile(from, target, info.Mode().Perm())
	})
}

func copyFile(from, to string, perm fs.FileMode) error {
	in, err := os.Open(from) // #nosec G304 -- from is a regular file walkTree confirmed inside the plugin root
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) // #nosec G304 -- to is under a staging dir this store created
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// OpenFile's mode passes through the umask; the kept copy must have the mode the plugin shipped.
	return os.Chmod(to, perm)
}
