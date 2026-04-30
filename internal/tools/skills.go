package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/skills"
)

const maxSkillToolChars = 8000

// SearchSkillCatalogTool searches skills.sh, the public agent skills
// directory. It is discovery-only; installation stays out of the LLM tool
// surface until Aura has an admin review flow.
type SearchSkillCatalogTool struct {
	catalog *skills.CatalogClient
}

func NewSearchSkillCatalogTool(catalog *skills.CatalogClient) *SearchSkillCatalogTool {
	return &SearchSkillCatalogTool{catalog: catalog}
}

func (t *SearchSkillCatalogTool) Name() string { return "search_skill_catalog" }

func (t *SearchSkillCatalogTool) Description() string {
	return "Search skills.sh for installable agent skills. Returns skill names, sources, install counts, and suggested npx skills commands. Read-only."
}

func (t *SearchSkillCatalogTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Skill search query. Empty returns the skills.sh leaderboard.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum results to return. Default 10, max 25.",
			},
		},
	}
}

func (t *SearchSkillCatalogTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.catalog == nil {
		return "", fmt.Errorf("search_skill_catalog: skills catalog unavailable")
	}
	query := strings.TrimSpace(stringArg(args, "query"))
	limit := intArg(args, "limit", 10, 1, 25)
	items, err := t.catalog.Search(ctx, query, limit)
	if err != nil {
		return "", fmt.Errorf("search_skill_catalog: %w", err)
	}
	if len(items) == 0 {
		return fmt.Sprintf("No skills.sh results found for %q.", query), nil
	}

	var sb strings.Builder
	if query == "" {
		fmt.Fprintf(&sb, "Top %d skills from skills.sh:\n\n", len(items))
	} else {
		fmt.Fprintf(&sb, "skills.sh results for %q:\n\n", query)
	}
	for i, item := range items {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, item.Name)
		fmt.Fprintf(&sb, "Source: %s\n", item.Source)
		if item.SkillID != "" && item.SkillID != item.Name {
			fmt.Fprintf(&sb, "Skill ID: %s\n", item.SkillID)
		}
		fmt.Fprintf(&sb, "Installs: %d\n", item.Installs)
		fmt.Fprintf(&sb, "Install: `%s`\n\n", item.InstallCommand())
	}
	return truncateForToolContext(sb.String(), maxSkillToolChars), nil
}

// ListSkillsTool lets the LLM inspect local SKILL.md packages without
// granting mutation rights.
type ListSkillsTool struct {
	loader *skills.Loader
}

func NewListSkillsTool(loader *skills.Loader) *ListSkillsTool {
	return &ListSkillsTool{loader: loader}
}

func (t *ListSkillsTool) Name() string { return "list_skills" }

func (t *ListSkillsTool) Description() string {
	return "List local Aura skills loaded from SKILL.md files. Read-only; use read_skill for full instructions."
}

func (t *ListSkillsTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *ListSkillsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.loader == nil {
		return "", fmt.Errorf("list_skills: skills loader unavailable")
	}
	loaded, err := t.loader.LoadAll()
	if err != nil {
		return "", fmt.Errorf("list_skills: %w", err)
	}
	if len(loaded) == 0 {
		return "No local skills found.", nil
	}

	type metadata struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	out := make([]metadata, 0, len(loaded))
	for _, skill := range loaded {
		out = append(out, metadata{Name: skill.Name, Description: skill.Description})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("list_skills: %w", err)
	}
	return string(data), nil
}

// ReadSkillTool returns one local skill's full SKILL.md instructions.
type ReadSkillTool struct {
	loader *skills.Loader
}

func NewReadSkillTool(loader *skills.Loader) *ReadSkillTool {
	return &ReadSkillTool{loader: loader}
}

func (t *ReadSkillTool) Name() string { return "read_skill" }

func (t *ReadSkillTool) Description() string {
	return "Read the full content of a local Aura skill by name. Read-only; use when a listed skill is relevant."
}

func (t *ReadSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Skill name from list_skills.",
			},
		},
		"required": []string{"name"},
	}
}

func (t *ReadSkillTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.loader == nil {
		return "", fmt.Errorf("read_skill: skills loader unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	skill, err := t.loader.LoadByName(name)
	if err != nil {
		return "", fmt.Errorf("read_skill: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", skill.Name)
	if skill.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n\n", skill.Description)
	}
	sb.WriteString(skill.Content)
	return truncateForToolContext(sb.String(), maxSkillToolChars), nil
}
