package packs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/chetto1983/aura/internal/skills"
)

// Fetcher materializes source into dir, which already exists and is empty.
// It is the one seam that touches the network, so a test drives the whole
// resolver against a fixture tree without one.
type Fetcher func(ctx context.Context, source, dir string) error

// Resolver reads packs out of a repository.
type Resolver struct {
	// Fetch defaults to a shallow git clone (see fetch.go).
	Fetch Fetcher
}

// Resolve materializes the reference's repository once and returns its packs:
// every one when the Ref names no directory, exactly one when it does.
//
// A repository with no manifest anywhere is an error naming that fact, not an
// empty slice — the caller asked to install something, and "nothing here" is an
// answer they need rather than a quiet success with no packs in it.
func (r *Resolver) Resolve(ctx context.Context, ref Ref) ([]Pack, error) {
	fetch := r.Fetch
	if fetch == nil {
		fetch = GitFetcher
	}
	dir, err := os.MkdirTemp("", "aura-pack-*")
	if err != nil {
		return nil, fmt.Errorf("pack workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := fetch(ctx, ref.Source, dir); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", ref.Source, err)
	}
	return ReadTree(dir, ref)
}

// ReadTree resolves packs from an ALREADY-materialized repository at root. It is
// separate from Resolve so the reading half is exercisable on a fixture without
// a fetcher at all, and so a caller who already has a checkout does not clone a
// second copy of it.
func ReadTree(root string, ref Ref) ([]Pack, error) {
	if ref.Directory != "" {
		pack, err := readPack(root, ref.Source, ref.Directory)
		if err != nil {
			return nil, err
		}
		return []Pack{pack}, nil
	}

	dirs, err := packDirs(root)
	if err != nil {
		return nil, err
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("%s: no plugin manifest found (looked for .claude-plugin/plugin.json at the root and one level down)", ref.Source)
	}
	packs := make([]Pack, 0, len(dirs))
	for _, d := range dirs {
		pack, err := readPack(root, ref.Source, d)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

// packDirs lists the plugin directories in root: the root itself when it carries
// a manifest (a single-plugin repository), otherwise every immediate child that
// does. It does not recurse further — Anthropic's layout is one level, and a
// deeper walk would start finding manifests in vendored copies.
func packDirs(root string) ([]string, error) {
	if hasManifest(root, "") {
		return []string{""}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read repository: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if hasManifest(root, e.Name()) {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func hasManifest(root, dir string) bool {
	info, err := os.Stat(filepath.Join(root, dir, ".claude-plugin", "plugin.json"))
	return err == nil && !info.IsDir()
}

func readPack(root, source, dir string) (Pack, error) {
	if dir != "" {
		if err := validSegment(dir); err != nil {
			return Pack{}, err
		}
	}
	base := filepath.Join(root, dir)
	man, err := readManifest(base)
	if err != nil {
		return Pack{}, fmt.Errorf("%s/%s: %w", source, dir, err)
	}
	pack := Pack{
		Source:      source,
		Directory:   dir,
		Name:        man.Name,
		Version:     man.Version,
		Description: man.Description,
		Author:      man.author(),
	}
	if pack.Name == "" {
		// The manifest is what makes a directory a pack; one without a name
		// cannot be addressed, installed or shown, so this is fatal rather than
		// something to paper over with the folder name.
		return Pack{}, fmt.Errorf("%s/%s: plugin.json has no name", source, dir)
	}
	if pack.Skills, err = readSkillNames(base); err != nil {
		return Pack{}, err
	}
	if pack.Servers, err = readServers(base); err != nil {
		return Pack{}, err
	}
	if pack.Commands, err = readCommands(base); err != nil {
		return Pack{}, err
	}
	return pack, nil
}

// manifest mirrors `.claude-plugin/plugin.json`. Author is raw because the
// format uses an object (`{"name": "Anthropic"}`) while plenty of hand-written
// manifests use a bare string, and refusing one of the two would reject packs
// over a field that is only ever displayed.
type manifest struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Author      json.RawMessage `json:"author"`
}

func (m manifest) author() string {
	if len(m.Author) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Author, &s); err == nil {
		return s
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(m.Author, &obj); err == nil {
		return obj.Name
	}
	return ""
}

func readManifest(base string) (manifest, error) {
	// #nosec G304 -- base is a directory the fetcher just created under
	// os.MkdirTemp, and the only caller-supplied segment in it passed
	// validSegment, which rejects "..", separators and path characters.
	raw, err := os.ReadFile(filepath.Join(base, ".claude-plugin", "plugin.json"))
	if err != nil {
		return manifest{}, fmt.Errorf("read plugin.json: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return manifest{}, fmt.Errorf("parse plugin.json: %w", err)
	}
	return m, nil
}

// readSkillNames lists the directories under skills/ that actually hold a
// SKILL.md. A directory without one is skipped rather than reported: the format
// leaves room for shared assets beside the skills, and a pack must not fail to
// resolve because someone dropped a README folder in there.
func readSkillNames(base string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(base, "skills"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := os.Stat(filepath.Join(base, "skills", e.Name(), "SKILL.md")); err == nil && !info.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// mcpFile mirrors `.mcp.json`.
type mcpFile struct {
	MCPServers map[string]struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		OAuth   json.RawMessage   `json:"oauth"`
	} `json:"mcpServers"`
}

func readServers(base string) ([]Server, error) {
	// #nosec G304 -- base is a directory the fetcher just created under
	// os.MkdirTemp, and the only caller-supplied segment in it passed
	// validSegment, which rejects "..", separators and path characters.
	raw, err := os.ReadFile(filepath.Join(base, ".mcp.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read .mcp.json: %w", err)
	}
	var f mcpFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse .mcp.json: %w", err)
	}
	servers := make([]Server, 0, len(f.MCPServers))
	for name, s := range f.MCPServers {
		servers = append(servers, Server{
			Name:    name,
			Type:    s.Type,
			URL:     s.URL,
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			OAuth:   len(s.OAuth) > 0 && string(s.OAuth) != "null",
		})
	}
	// A JSON object has no order, so without this the same pack renders its
	// connectors differently on every read and no test of it can be stable.
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return servers, nil
}

// commandFrontmatter is the header of a `commands/<name>.md`. It is the SAME
// fence a SKILL.md opens with, which is why the split comes from internal/skills
// rather than from a second implementation here.
type commandFrontmatter struct {
	Description  string `yaml:"description"`
	ArgumentHint string `yaml:"argument-hint"`
}

func readCommands(base string) ([]Command, error) {
	entries, err := os.ReadDir(filepath.Join(base, "commands"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read commands: %w", err)
	}
	var cmds []Command
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		// #nosec G304 -- see readManifest; e.Name() comes from ReadDir of a
		// directory inside that same tree, never from a caller.
		raw, err := os.ReadFile(filepath.Join(base, "commands", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read command %s: %w", e.Name(), err)
		}
		cmd := Command{Name: strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))}
		block, body, err := skills.SplitFrontmatter(raw)
		if err != nil {
			// A command without a fence is still a command: its file IS the
			// instruction, and refusing the whole pack over a missing optional
			// header would be the format being stricter than its own authors.
			cmd.Body = strings.TrimSpace(string(raw))
		} else {
			var fm commandFrontmatter
			if err := yaml.Unmarshal(block, &fm); err != nil {
				return nil, fmt.Errorf("parse command %s frontmatter: %w", e.Name(), err)
			}
			cmd.Description, cmd.ArgumentHint = fm.Description, fm.ArgumentHint
			cmd.Body = strings.TrimSpace(body)
		}
		cmds = append(cmds, cmd)
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds, nil
}
