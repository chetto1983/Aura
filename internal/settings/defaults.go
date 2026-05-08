package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/config"
)

const (
	// BestDefaultsVersionKey is an internal settings row. It is intentionally
	// not dashboard-editable: it only records which one-time defaults migration
	// has already run for this database.
	BestDefaultsVersionKey = "AURA_BEST_DEFAULTS_VERSION"
	BestDefaultsVersion    = "2026-05-07-container-defaults-v1"
)

// BestDefaultChange reports one automatic settings repair performed at
// startup. Values are not logged by cmd/aura so secrets never leak through
// this path, but keeping them here makes unit tests precise.
type BestDefaultChange struct {
	Key      string
	OldValue string
	NewValue string
	Reason   string
}

type defaultRule struct {
	key       string
	value     string
	reason    string
	shouldSet func(current string, exists bool) bool
}

// ApplyBestDefaults upgrades stale settings rows that would otherwise hide
// safer Docker-first defaults from the user. It never touches chat provider
// credentials, embedding credentials, Telegram tokens, or OCR keys. The only
// secret rows it can rotate are known-public Garage demo credentials.
func ApplyBestDefaults(ctx context.Context, repo Repository, cfg *config.Config) ([]BestDefaultChange, error) {
	if repo == nil {
		return nil, nil
	}
	current, err := repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("settings best defaults: list: %w", err)
	}

	alreadyMigrated := strings.TrimSpace(current[BestDefaultsVersionKey]) == BestDefaultsVersion
	rules := validationRules(cfg)
	if !alreadyMigrated {
		rules = append(rules, migrationRules(cfg)...)
	}

	changes := make([]BestDefaultChange, 0, len(rules)+1)
	for _, rule := range rules {
		oldValue, exists := current[rule.key]
		if !rule.shouldSet(oldValue, exists) {
			continue
		}
		if err := repo.Set(ctx, rule.key, rule.value); err != nil {
			return changes, fmt.Errorf("settings best defaults: set %s: %w", rule.key, err)
		}
		current[rule.key] = rule.value
		changes = append(changes, BestDefaultChange{
			Key:      rule.key,
			OldValue: oldValue,
			NewValue: rule.value,
			Reason:   rule.reason,
		})
	}

	if !alreadyMigrated {
		oldValue := current[BestDefaultsVersionKey]
		if err := repo.Set(ctx, BestDefaultsVersionKey, BestDefaultsVersion); err != nil {
			return changes, fmt.Errorf("settings best defaults: set %s: %w", BestDefaultsVersionKey, err)
		}
		changes = append(changes, BestDefaultChange{
			Key:      BestDefaultsVersionKey,
			OldValue: oldValue,
			NewValue: BestDefaultsVersion,
			Reason:   "record defaults migration version",
		})
	}

	return changes, nil
}

func validationRules(cfg *config.Config) []defaultRule {
	searchBackendDefault := config.DefaultSearchBackend
	webSearchDefault := "disabled"
	sandboxRuntimeModeDefault := config.DefaultSandboxRuntimeMode
	if cfg != nil && cfg.Headless {
		if strings.TrimSpace(cfg.QdrantURL) != "" {
			searchBackendDefault = "qdrant"
		}
		if strings.TrimSpace(cfg.SearXNGBaseURL) != "" {
			webSearchDefault = "searxng"
		}
		if strings.TrimSpace(cfg.SandboxRuntimeURL) != "" {
			sandboxRuntimeModeDefault = "container"
		}
	}

	return []defaultRule{
		{
			key:       KeySummarizerMode,
			value:     config.DefaultSummarizerMode,
			reason:    "repair invalid memory capture mode",
			shouldSet: missingOrInvalidSet("off", "review", "auto_low_risk", "auto"),
		},
		{
			key:       KeyToolProfileMode,
			value:     "auto",
			reason:    "repair invalid tool profile mode",
			shouldSet: missingOrInvalidSet("auto", "default", "compute", "document", "admin", "memory", "swarm_research", "sandbox_compute", "admin_review"),
		},
		{
			key:       KeyOrchestrationLogLevel,
			value:     "summary",
			reason:    "repair invalid orchestration log level",
			shouldSet: missingOrInvalidSet("summary", "debug"),
		},
		{
			key:       KeySkillPreflight,
			value:     config.DefaultSkillPreflight,
			reason:    "repair invalid skill preflight policy",
			shouldSet: missingOrInvalidSet("advisory", "off"),
		},
		{
			key:       KeySkillRoutingMode,
			value:     config.DefaultSkillRoutingMode,
			reason:    "repair invalid skill routing mode",
			shouldSet: missingOrInvalidSet("manifest", "manifest_llm_review"),
		},
		{
			key:       KeyAgentLoopMaxSteps,
			value:     strconv.Itoa(config.DefaultAgentLoopMaxSteps),
			reason:    "repair invalid agent loop step limit",
			shouldSet: missingOrOutOfRangeInt(1, 50),
		},
		{
			key:       KeyTerminalToolPolicy,
			value:     config.DefaultTerminalToolPolicy,
			reason:    "repair invalid terminal tool policy",
			shouldSet: missingOrInvalidSet("profile", "off"),
		},
		{
			key:       KeyDelegationMode,
			value:     config.DefaultDelegationMode,
			reason:    "repair invalid delegation mode",
			shouldSet: missingOrInvalidSet("fast", "bounded", "async"),
		},
		{
			key:       KeyTraceRetentionDays,
			value:     strconv.Itoa(config.DefaultTraceRetentionDays),
			reason:    "repair invalid trace retention days",
			shouldSet: missingOrOutOfRangeInt(1, 365),
		},
		{
			key:       KeySearchBackend,
			value:     searchBackendDefault,
			reason:    "repair invalid search backend",
			shouldSet: missingOrInvalidSet("chromem", "qdrant"),
		},
		{
			key:       KeyWebSearchProvider,
			value:     webSearchDefault,
			reason:    "repair invalid web search provider",
			shouldSet: missingOrInvalidSet("disabled", "searxng", "ollama"),
		},
		{
			key:       KeySandboxRuntimeMode,
			value:     sandboxRuntimeModeDefault,
			reason:    "repair invalid sandbox runtime mode",
			shouldSet: missingOrInvalidSet("auto", "container", "local"),
		},
		{
			key:       KeySandboxAutoImproveMode,
			value:     "dry_run",
			reason:    "repair invalid sandbox auto-improve mode",
			shouldSet: missingOrInvalidSet("off", "dry_run", "auto"),
		},
	}
}

func migrationRules(cfg *config.Config) []defaultRule {
	rules := []defaultRule{
		{
			key:       KeySummarizerMode,
			value:     config.DefaultSummarizerMode,
			reason:    "move old direct-write memory capture to low-risk automatic default",
			shouldSet: valueIs("auto"),
		},
		{
			key:       KeySummarizerTurnInterval,
			value:     strconv.Itoa(config.DefaultSummarizerTurnInterval),
			reason:    "capture after a normal user/assistant turn by default",
			shouldSet: valueIs("5"),
		},
		{
			key:       KeySummarizerCooldownSeconds,
			value:     strconv.Itoa(config.DefaultSummarizerCooldownSeconds),
			reason:    "remove legacy capture cooldown",
			shouldSet: valueIs("59", "60"),
		},
	}

	if cfg == nil || !cfg.Headless {
		return rules
	}

	rules = append(rules,
		containerRule(KeyHTTPPort, cfg.HTTPPort, "use container dashboard bind address", isLoopbackBind),
		containerRule(KeyHeadless, "true", "keep container runtime headless", valueIs("false")),
		containerRule(KeyEnvPath, cfg.EnvPath, "use mounted container env path", isLocalPathLike),
		containerRule(KeyDBPath, cfg.DBPath, "use mounted container database path", isLocalPathLike),
		containerRule(KeyLogDir, cfg.LogDir, "use mounted container log directory", isLocalPathLike),
		containerRule(KeyWikiPath, cfg.WikiPath, "use mounted container wiki path", isLocalPathLike),
		containerRule(KeySkillsPath, cfg.SkillsPath, "use mounted container skills path", isLocalPathLike),
		containerRule(KeySkillsInstallProjectDir, cfg.SkillsInstallProjectDir, "persist installed skills in mounted volume", isLocalPathLike),
		containerRule(KeyMCPServersPath, cfg.MCPServersPath, "use mounted container MCP config path", isLocalPathLike),
		containerRule(KeyPromptOverlayPath, cfg.PromptOverlayPath, "use mounted container prompt overlay path", isLocalPathLike),
	)

	if strings.TrimSpace(cfg.SearXNGBaseURL) != "" {
		rules = append(rules,
			containerRule(KeyWebSearchProvider, "searxng", "use bundled SearXNG web search in Docker", valueIs("disabled", "ollama")),
			containerRule(KeySearXNGBaseURL, cfg.SearXNGBaseURL, "use container SearXNG service URL", isLocalURLLike),
		)
	}
	if strings.TrimSpace(cfg.QdrantURL) != "" {
		rules = append(rules,
			containerRule(KeySearchBackend, "qdrant", "use bundled Qdrant search sidecar in Docker", valueIs("chromem")),
			containerRule(KeyQdrantURL, cfg.QdrantURL, "use container Qdrant service URL", isLocalURLLike),
		)
	}
	if strings.TrimSpace(cfg.GarageS3Endpoint) != "" {
		rules = append(rules,
			containerRule(KeyGarageS3Endpoint, cfg.GarageS3Endpoint, "use container Garage service URL", isLocalURLLike),
		)
	}
	if strings.TrimSpace(cfg.GarageS3AccessKey) != "" {
		rules = append(rules,
			containerRule(KeyGarageS3AccessKey, cfg.GarageS3AccessKey, "replace public Garage demo access key", valueIs("aura-local-access")),
		)
	}
	if strings.TrimSpace(cfg.GarageS3SecretKey) != "" {
		rules = append(rules,
			containerRule(KeyGarageS3SecretKey, cfg.GarageS3SecretKey, "replace public Garage demo secret key", valueIs("aura-local-secret-change-me")),
		)
	}
	if strings.TrimSpace(cfg.SandboxRuntimeURL) != "" {
		rules = append(rules,
			containerRule(KeySandboxRuntimeMode, "container", "use always-on Pyodide sidecar in Docker", valueIs("auto", "local")),
			containerRule(KeySandboxRuntimeURL, cfg.SandboxRuntimeURL, "use container Pyodide service URL", isLocalURLLike),
		)
	}
	return rules
}

func containerRule(key, value, reason string, pred func(string, bool) bool) defaultRule {
	return defaultRule{
		key:    key,
		value:  value,
		reason: reason,
		shouldSet: func(current string, exists bool) bool {
			return strings.TrimSpace(value) != "" && pred(current, exists)
		},
	}
}

func missingOrInvalidSet(allowed ...string) func(string, bool) bool {
	valid := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		valid[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	return func(current string, exists bool) bool {
		normalized := strings.ToLower(strings.TrimSpace(current))
		if normalized == "" {
			return false
		}
		_, ok := valid[normalized]
		return !ok
	}
}

func missingOrOutOfRangeInt(min, max int) func(string, bool) bool {
	return func(current string, exists bool) bool {
		normalized := strings.TrimSpace(current)
		if normalized == "" {
			return false
		}
		value, err := strconv.Atoi(normalized)
		return err != nil || value < min || value > max
	}
}

func valueIs(values ...string) func(string, bool) bool {
	match := make(map[string]struct{}, len(values))
	for _, value := range values {
		match[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	return func(current string, exists bool) bool {
		if !exists {
			return false
		}
		_, ok := match[strings.ToLower(strings.TrimSpace(current))]
		return ok
	}
}

func isLoopbackBind(current string, exists bool) bool {
	if !exists {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(current))
	return strings.HasPrefix(normalized, "127.0.0.1:") || strings.HasPrefix(normalized, "localhost:")
}

func isLocalURLLike(current string, exists bool) bool {
	if !exists {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(current))
	return normalized == "" ||
		strings.Contains(normalized, "127.0.0.1") ||
		strings.Contains(normalized, "localhost")
}

func isLocalPathLike(current string, exists bool) bool {
	if !exists {
		return false
	}
	trimmed := strings.TrimSpace(current)
	if trimmed == "" {
		return true
	}
	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	lower := strings.ToLower(normalized)
	return normalized == "." ||
		strings.HasPrefix(normalized, "./") ||
		strings.HasPrefix(normalized, "../") ||
		strings.Contains(lower, ":/") ||
		strings.HasPrefix(lower, "/app/") ||
		strings.HasPrefix(lower, "/tmp/") ||
		!strings.HasPrefix(normalized, "/")
}
