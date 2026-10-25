package skills

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"

	"github.com/chetto1983/aura/internal/scoring"
)

// ValidateDir checks a fetched skill directory against the write boundary without writing
// anything: its SKILL.md parses, the declared name equals the directory's (D-30), and
// ValidateForWrite passes with the blocklist enforced. It returns the frontmatter and body.
func ValidateDir(dir string, blocklist []string, bodyCapBytes int) (Frontmatter, string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md")) // #nosec G304 -- dir is a tree the caller fetched into its own work dir
	if err != nil {
		return Frontmatter{}, "", fmt.Errorf("read staged SKILL.md: %w", err)
	}
	fm, body, err := parseFrontmatter(raw)
	if err != nil {
		return Frontmatter{}, "", fmt.Errorf("parse staged SKILL.md: %w", err)
	}
	if err := ValidateNameAgainstDir(fm, filepath.Base(dir)); err != nil {
		return Frontmatter{}, "", err
	}
	if err := ValidateForWrite(fm, body, blocklist, bodyCapBytes, false); err != nil {
		return Frontmatter{}, "", err
	}
	return fm, body, nil
}

// InstallDir installs the skill already fetched into dir, which it may rewrite: it validates
// the tree, drops `always`, hashes the result and hands it to Writer.WriteInstall, which lands
// it active, materialized and audited. source is what the audit and the result name.
//
// An installed skill never arrives always-on. always:true prepends the body to every one of
// the owner's turns, and a third-party author must not be able to say that for the operator
// (spec 2026-10-25-pack-install §6b). The flag is removed from the file, not only from the
// catalog row, because the always-on block is read from the materialized SKILL.md.
func (i *Installer) InstallDir(ctx context.Context, dir, source string, actor AuditActor) (InstallInfo, error) {
	if i.writer == nil {
		return InstallInfo{}, fmt.Errorf("install %q: writer not configured", source)
	}
	fm, body, err := ValidateDir(dir, i.blocklist, i.bodyCapBytes)
	if err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: %w", source, err)
	}
	if fm.Always {
		if err := stripAlways(filepath.Join(dir, "SKILL.md")); err != nil {
			return InstallInfo{}, fmt.Errorf("install %q: drop always: %w", source, err)
		}
		fm.Always = false
	}
	hash, err := HashSkillDir(dir)
	if err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: hash staged tree: %w", source, err)
	}
	status, err := i.writer.WriteInstall(ctx, fm, dir, hash, actor)
	if err != nil {
		return InstallInfo{}, fmt.Errorf("install %q: %w", source, err)
	}
	return InstallInfo{
		Name:        fm.Name,
		Source:      source,
		ContentHash: hash,
		Preview:     previewBody(body),
		Destination: filepath.Join(i.writer.activeDir, fm.Name),
		RiskTier:    string(scoring.ComputeSkillTier(scoring.SkillInstall, body)),
		Status:      status,
		Checklist:   installChecklist(),
	}, nil
}

// LandingHash is the content hash InstallDir records for the skill in dir: the tree's hash with
// `always` dropped from its SKILL.md. It writes nothing, so a plugin review can record the hash
// a removal later compares the active copy against.
func LandingHash(dir string) (string, error) {
	files := make(map[string][]byte)
	if err := collectFilesNoSymlinks(dir, dir, files); err != nil {
		return "", err
	}
	if raw, ok := files["SKILL.md"]; ok {
		fm, _, err := parseFrontmatter(raw)
		if err != nil {
			return "", fmt.Errorf("parse SKILL.md: %w", err)
		}
		if fm.Always {
			if files["SKILL.md"], err = withoutAlways(raw); err != nil {
				return "", err
			}
		}
	}
	return HashSkillFiles(files), nil
}

// stripAlways rewrites a SKILL.md without its `always` key.
func stripAlways(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the SKILL.md of the tree InstallDir was handed
	if err != nil {
		return err
	}
	out, err := withoutAlways(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, info.Mode().Perm()) // #nosec G703 -- the SKILL.md read above, of the tree InstallDir was handed
}

// withoutAlways renders a SKILL.md without its `always` key. The frontmatter goes through an
// ordered YAML round trip, so every other key, nested ones included, keeps its value and place.
func withoutAlways(raw []byte) ([]byte, error) {
	block, body, err := SplitFrontmatter(raw)
	if err != nil {
		return nil, err
	}
	var header yaml.MapSlice
	if err := yaml.UnmarshalWithOptions(block, &header, yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("parse frontmatter yaml: %w", err)
	}
	kept := header[:0]
	for _, item := range header {
		if item.Key != "always" {
			kept = append(kept, item)
		}
	}
	out, err := yaml.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("render frontmatter yaml: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(out)
	b.WriteString("---\n")
	b.WriteString(body)
	return b.Bytes(), nil
}
