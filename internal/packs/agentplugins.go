package packs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The canonical identifiers of Agent Plugins 1.0 (spec/1.0.0.md §5.2 and §7.2.1, read from
// agentplugins/agent-plugins-spec on 2026-10-25). A schema is never fetched: the identifier only
// selects the rules compiled in here.
const (
	AgentPluginsManifestSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	AgentPluginsMCPSchema      = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
	agentPluginsSchemaPrefix   = "https://agent-plugins.org/schemas/"
)

// agentManifestFields is the closed top-level field set of §5.2.
var agentManifestFields = map[string]bool{
	"$schema": true, "name": true, "version": true, "description": true, "author": true,
	"homepage": true, "repository": true, "license": true, "keywords": true, "extensions": true,
}

// claimsAgentPlugins reports whether dir holds a root plugin.json that declares an Agent
// Plugins $schema. A plugin.json that is a symlink claims it unconditionally: reading its target
// could mean reading outside the plugin, and readAgentPack rejects it with that reason instead.
func claimsAgentPlugins(dir string) bool {
	path := filepath.Join(dir, "plugin.json")
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return true
	}
	if !info.Mode().IsRegular() {
		return false
	}
	// #nosec G304 -- dir is a checkout directory under the resolver's own temp root, and the
	// only caller-supplied segment in it passed validSegment.
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var head struct {
		Schema string `json:"$schema"`
	}
	return json.Unmarshal(raw, &head) == nil && strings.HasPrefix(head.Schema, agentPluginsSchemaPrefix)
}

// schemaVersion returns the version segment of an Agent Plugins identifier, for the refusal
// that names it.
func schemaVersion(id string) string {
	rest := strings.TrimPrefix(id, agentPluginsSchemaPrefix)
	if v, _, ok := strings.Cut(rest, "/"); ok {
		return v
	}
	return rest
}

// readAgentPack reads an Agent Plugins plugin. Only a manifest violation is fatal (§5.2,
// §11.3): every other failure stays inside the component it belongs to.
func readAgentPack(base, source, dir string) (Pack, error) {
	label := strings.TrimSuffix(source+"/"+dir, "/")
	if _, err := withinRoot(base, "plugin.json"); err != nil {
		return Pack{}, fmt.Errorf("%s: plugin.json %w", label, err)
	}
	pack, err := readAgentManifest(filepath.Join(base, "plugin.json"))
	if err != nil {
		return Pack{}, fmt.Errorf("%s: %w", label, err)
	}
	pack.Source, pack.Directory, pack.Format = source, dir, FormatAgentPlugins
	pack.Skills, pack.SkippedSkills = readContainedSkills(base)
	pack.Servers, pack.SkippedServers, pack.MCPDisabled = readAgentServers(base)
	return pack, nil
}

func readAgentManifest(path string) (Pack, error) {
	// #nosec G304 -- path was just checked by withinRoot against the checkout root.
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pack{}, fmt.Errorf("read plugin.json: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Pack{}, errors.New("plugin.json: want a JSON object")
	}
	var pack Pack
	schema, err := requiredString(fields, "$schema")
	if err != nil {
		return Pack{}, err
	}
	if schema != AgentPluginsManifestSchema {
		return Pack{}, fmt.Errorf("plugin.json: unsupported Agent Plugins version %s (Aura reads 1.0.0)", schemaVersion(schema))
	}
	if pack.Name, err = requiredString(fields, "name"); err != nil {
		return Pack{}, err
	}
	if err := ValidPluginName(pack.Name); err != nil {
		return Pack{}, fmt.Errorf("plugin.json: name: %w", err)
	}
	for _, f := range []struct {
		key string
		dst *string
	}{{"version", &pack.Version}, {"description", &pack.Description}, {"homepage", nil}, {"repository", nil}, {"license", nil}} {
		value, err := optionalString(fields, f.key)
		if err != nil {
			return Pack{}, err
		}
		if f.dst != nil {
			*f.dst = value
		}
	}
	if pack.Author, err = agentAuthor(fields["author"]); err != nil {
		return Pack{}, err
	}
	if raw, ok := fields["keywords"]; ok {
		var keywords []string
		if err := strictUnmarshal(raw, &keywords); err != nil {
			return Pack{}, errors.New("plugin.json: keywords: want an array of strings")
		}
	}
	pack.Notes = manifestNotes(fields)
	return pack, nil
}

// manifestNotes reports what §5.2 and §8.1 say to report and ignore: an unknown top-level
// field, and an extensions field that is not an object. Sorted, because the notes are part of
// the review the install digest covers.
func manifestNotes(fields map[string]json.RawMessage) []string {
	var notes []string
	for key := range fields {
		if !agentManifestFields[key] {
			notes = append(notes, fmt.Sprintf("unknown plugin.json field %q ignored", key))
		}
	}
	if raw, ok := fields["extensions"]; ok && !isJSONObject(raw) {
		notes = append(notes, `plugin.json field "extensions" is not an object; ignored`)
	}
	sort.Strings(notes)
	return notes
}

func agentAuthor(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "", nil
	}
	var author struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		URL   string `json:"url"`
	}
	if err := strictUnmarshal(raw, &author); err != nil {
		return "", errors.New("plugin.json: author: want an object with only name, email and url strings")
	}
	return author.Name, nil
}

func requiredString(fields map[string]json.RawMessage, key string) (string, error) {
	value, err := optionalString(fields, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("plugin.json: %s is required", key)
	}
	return value, nil
}

func optionalString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("plugin.json: %s: want a string", key)
	}
	return value, nil
}

// strictUnmarshal refuses fields the target does not declare, which is how the closed object
// types of the specification (author, an MCP server variant) are enforced.
func strictUnmarshal(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func isJSONObject(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil && obj != nil
}

// readContainedSkills is the skills reader with §4.1's containment and §6.2's kind check: a
// skills location that leaves the root or is not a directory disables skills, and a SKILL.md
// that leaves the root skips that one skill.
func readContainedSkills(base string) ([]string, []Skipped) {
	info, err := statContained(base, "skills")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case errors.Is(err, errOutsideRoot):
		return nil, []Skipped{{Name: "skills", Reason: "skills " + errOutsideRoot.Error()}}
	case err != nil:
		return nil, []Skipped{{Name: "skills", Reason: "skills: " + err.Error()}}
	case !info.IsDir():
		return nil, []Skipped{{Name: "skills", Reason: "skills is not a directory"}}
	}
	entries, err := os.ReadDir(filepath.Join(base, "skills"))
	if err != nil {
		return nil, []Skipped{{Name: "skills", Reason: "skills: " + err.Error()}}
	}
	var names []string
	var skipped []Skipped
	for _, e := range entries {
		rel := "skills/" + e.Name() + "/SKILL.md"
		info, err := statContained(base, rel)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			skipped = append(skipped, Skipped{Name: e.Name(), Reason: "SKILL.md " + err.Error()})
		case info.Mode().IsRegular():
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	sortSkipped(skipped)
	return names, skipped
}

func sortSkipped(s []Skipped) {
	sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name })
}
