package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/identity"
	auraskills "github.com/aura/aura/internal/skills"
)

const (
	maxSkillToolItems       = 50
	defaultSkillToolItems   = 10
	maxSkillToolBodyChars   = 16000
	skillInstallToolTimeout = 90 * time.Second
)

type skillLoader interface {
	LoadAll() ([]auraskills.Skill, error)
	LoadByName(name string) (auraskills.Skill, error)
	Invalidate()
}

type skillCatalogSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]auraskills.CatalogItem, error)
}

type skillInstaller interface {
	Install(ctx context.Context, source, skillID string) (string, error)
}

type skillDeleter interface {
	Delete(name string) error
}

// SkillTool exposes Aura's local skill lifecycle to the LLM through one
// action-enum surface. Reads use the existing loader/catalog directly; writes
// stay behind the same admin flag plus durable identity capabilities as the
// dashboard path.
type SkillTool struct {
	loader       skillLoader
	catalog      skillCatalogSearcher
	installer    skillInstaller
	deleter      skillDeleter
	adminEnabled bool
}

func NewSkillTool(loader skillLoader, catalog skillCatalogSearcher, installer skillInstaller, deleter skillDeleter, adminEnabled bool) *SkillTool {
	if loader == nil && catalog == nil && installer == nil && deleter == nil {
		return nil
	}
	return &SkillTool{
		loader:       loader,
		catalog:      catalog,
		installer:    installer,
		deleter:      deleter,
		adminEnabled: adminEnabled,
	}
}

func (t *SkillTool) Name() string { return "skill" }

func (t *SkillTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:            t.Name(),
		Description:     t.Description(),
		Parameters:      t.Parameters(),
		DestructiveHint: true,
		OpenWorldHint:   true,
		Examples: []ToolCallExample{
			{Description: "List installed skills.", Arguments: map[string]any{"action": "list", "limit": 10}},
			{Description: "Search the public skills catalog.", Arguments: map[string]any{"action": "catalog", "query": "frontend", "limit": 5}},
			{Description: "Inspect one installed skill.", Arguments: map[string]any{"action": "info", "name": "aura-runtime-safety"}},
			{Description: "Install a catalog skill when admin-gated.", Arguments: map[string]any{"action": "install", "source": "anthropics/skills", "skill_id": "frontend-design"}},
		},
	}
}

func (t *SkillTool) Description() string {
	return "List installed skills, browse catalog, inspect one skill, or admin-install/remove skills. Writes return capability_denied unless admin grants exist."
}

func (t *SkillTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        skillActions,
				"description": "Operation: list installed, catalog search, info, install, or remove.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "catalog only: optional search text.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     maxSkillToolItems,
				"description": "list/catalog only: maximum entries.",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "info/remove: installed skill name.",
			},
			"source": map[string]any{
				"type":        "string",
				"description": "install only: skills.sh source such as owner/repo or package.",
			},
			"skill_id": map[string]any{
				"type":        "string",
				"description": "install only: optional skills.sh skill id.",
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "list", RequiredKeys: nil},
			{Name: "catalog", RequiredKeys: nil},
			{Name: "info", RequiredKeys: []string{"name"}},
			{Name: "install", RequiredKeys: []string{"source"}},
			{Name: "remove", RequiredKeys: []string{"name"}},
		}),
	}
}

func (t *SkillTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if rewritten, ok := RewriteVerbKeyAsAction(args, skillActions, nil); ok {
		args = rewritten
	}
	switch strings.TrimSpace(stringArg(args, "action")) {
	case "list":
		return t.doList(args)
	case "catalog":
		return t.doCatalog(ctx, args)
	case "info":
		return t.doInfo(args)
	case "install":
		return t.doInstall(ctx, args)
	case "remove":
		return t.doRemove(ctx, args)
	case "":
		return "", ActionRequiredError("skill", skillActions, args, nil, "list")
	default:
		return "", UnknownActionError("skill", stringArg(args, "action"), skillActions, args)
	}
}

var skillActions = []string{"list", "catalog", "info", "install", "remove"}

func (t *SkillTool) doList(args map[string]any) (string, error) {
	if t.loader == nil {
		return marshalSkillTool(map[string]any{"schema": "skill_list", "count": 0, "skills": []any{}})
	}
	loaded, err := t.loader.LoadAll()
	if err != nil {
		return "", err
	}
	total := len(loaded)
	limit := skillLimitArg(args)
	truncated := total > limit
	if truncated {
		loaded = loaded[:limit]
	}
	out := make([]map[string]any, 0, len(loaded))
	for _, skill := range loaded {
		out = append(out, map[string]any{
			"name":        skill.Name,
			"description": skill.Description,
		})
	}
	return marshalSkillTool(map[string]any{"schema": "skill_list", "count": len(out), "total": total, "truncated": truncated, "skills": out})
}

func (t *SkillTool) doCatalog(ctx context.Context, args map[string]any) (string, error) {
	if t.catalog == nil {
		return marshalSkillTool(map[string]any{"schema": "skill_catalog", "count": 0, "items": []any{}})
	}
	limit := skillLimitArg(args)
	items, err := t.catalog.Search(ctx, strings.TrimSpace(stringArg(args, "query")), limit)
	if err != nil {
		return "", err
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"source":          item.Source,
			"skill_id":        item.SkillID,
			"name":            item.Name,
			"installs":        item.Installs,
			"install_command": item.InstallCommand(),
		})
	}
	return marshalSkillTool(map[string]any{"schema": "skill_catalog", "count": len(out), "items": out})
}

func (t *SkillTool) doInfo(args map[string]any) (string, error) {
	if t.loader == nil {
		return "", errors.New("skill info: loader unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	if !auraskills.ValidSkillName(name) {
		return "", fmt.Errorf("skill info: invalid skill name")
	}
	skill, err := t.loader.LoadByName(name)
	if err != nil {
		return "", err
	}
	body := skill.Content
	truncated := false
	if len(body) > maxSkillToolBodyChars {
		body = body[:maxSkillToolBodyChars]
		truncated = true
	}
	return marshalSkillTool(map[string]any{
		"schema":      "skill_info",
		"name":        skill.Name,
		"description": skill.Description,
		"content":     body,
		"truncated":   truncated,
	})
}

func (t *SkillTool) doInstall(ctx context.Context, args map[string]any) (string, error) {
	if denial, ok, err := t.authorizeMutation(ctx, identity.CapabilitySkillsInstall); !ok || err != nil {
		return denial, err
	}
	if t.installer == nil {
		return "", errors.New("skill install: installer unavailable")
	}
	source, err := requiredString(args, "source")
	if err != nil {
		return "", err
	}
	source = strings.TrimSpace(source)
	if !auraskills.ValidCatalogSource(source) {
		return "", fmt.Errorf("skill install: invalid source")
	}
	skillID := strings.TrimSpace(stringArg(args, "skill_id"))
	if !auraskills.ValidCatalogSkillID(skillID) {
		return "", fmt.Errorf("skill install: invalid skill_id")
	}
	runCtx, cancel := context.WithTimeout(ctx, skillInstallToolTimeout)
	defer cancel()
	output, err := t.installer.Install(runCtx, source, skillID)
	if err != nil {
		return "", err
	}
	if t.loader != nil {
		t.loader.Invalidate()
	}
	return marshalSkillTool(map[string]any{
		"schema":   "skill_install",
		"ok":       true,
		"source":   source,
		"skill_id": skillID,
		"output":   clipSkillToolOutput(output),
	})
}

func (t *SkillTool) doRemove(ctx context.Context, args map[string]any) (string, error) {
	if denial, ok, err := t.authorizeMutation(ctx, identity.CapabilitySkillsDelete); !ok || err != nil {
		return denial, err
	}
	if t.deleter == nil {
		return "", errors.New("skill remove: deleter unavailable")
	}
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	if !auraskills.ValidSkillName(name) {
		return "", fmt.Errorf("skill remove: invalid skill name")
	}
	if err := t.deleter.Delete(name); err != nil {
		return "", err
	}
	if t.loader != nil {
		t.loader.Invalidate()
	}
	return marshalSkillTool(map[string]any{"schema": "skill_remove", "ok": true, "name": name})
}

func (t *SkillTool) authorizeMutation(ctx context.Context, capability identity.Capability) (string, bool, error) {
	if !t.adminEnabled {
		return skillCapabilityDenied(capability, "skills admin is disabled; request admin approval"), false, nil
	}
	authorizer, ok := identity.AuthorizerFromContext(ctx)
	if !ok {
		return skillCapabilityDenied(capability, "identity authorizer is unavailable; request admin approval"), false, nil
	}
	decision, err := authorizer.Authorize(ctx, identity.AuthorizeParams{
		ActorID:    identity.ActorIDFromContext(ctx),
		Capability: capability,
		Resource:   identity.ResourceRef{Type: "skill", ID: string(capability)},
		RunID:      identity.RunIDFromContext(ctx),
		EventID:    identity.EventIDFromContext(ctx),
	})
	if err != nil {
		return "", false, err
	}
	if decision.Decision != identity.DecisionAllow {
		hint := "request admin approval"
		if decision.Reason != "" {
			hint += ": " + decision.Reason
		}
		return skillCapabilityDenied(capability, hint), false, nil
	}
	return "", true, nil
}

func skillCapabilityDenied(capability identity.Capability, hint string) string {
	out, _ := json.Marshal(map[string]any{
		"schema":     "denial",
		"error":      "capability_denied",
		"capability": string(capability),
		"hint":       hint,
	})
	return string(out)
}

func skillLimitArg(args map[string]any) int {
	return intArg(args, "limit", defaultSkillToolItems, 1, maxSkillToolItems)
}

func clipSkillToolOutput(s string) string {
	const max = 2048
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]"
}

func marshalSkillTool(v map[string]any) (string, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
