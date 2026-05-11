package settings

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/aura/aura/internal/config"
)

func TestApplyBestDefaultsMigratesLegacyRowsOnce(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	changes, err := ApplyBestDefaults(ctx, s, &config.Config{})
	if err != nil {
		t.Fatalf("ApplyBestDefaults: %v", err)
	}
	assertChanged(t, changes, BestDefaultsVersionKey)

	changes, err = ApplyBestDefaults(ctx, s, &config.Config{})
	if err != nil {
		t.Fatalf("ApplyBestDefaults second run: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("second ApplyBestDefaults changes = %+v, want none", changes)
	}
}

func TestApplyBestDefaultsMigratesContainerRows(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for key, value := range map[string]string{
		KeyHTTPPort:                "127.0.0.1:8080",
		KeyHeadless:                "false",
		KeyEnvPath:                 ".env",
		KeyDBPath:                  "./aura.db",
		KeyLogDir:                  "./logs",
		KeyWikiPath:                "./wiki",
		KeySkillsPath:              "./skills",
		KeySkillsInstallProjectDir: "./skills",
		KeyMCPServersPath:          "./mcp.json",
		KeyPromptOverlayPath:       ".",
		KeyWorkspaceRoot:           "/app",
		KeyWebSearchProvider:       "disabled",
		KeySearXNGBaseURL:          "http://127.0.0.1:8088",
		KeyQdrantURL:               "http://127.0.0.1:6333",
		KeyGarageS3Endpoint:        "http://localhost:3900",
		KeyGarageS3AccessKey:       "aura-local-access",
		KeyGarageS3SecretKey:       "aura-local-secret-change-me",
	} {
		mustSet(t, s, key, value)
	}

	cfg := containerDefaultsConfig()
	changes, err := ApplyBestDefaults(ctx, s, cfg)
	if err != nil {
		t.Fatalf("ApplyBestDefaults: %v", err)
	}
	for _, key := range []string{
		KeyHTTPPort, KeyHeadless, KeyEnvPath, KeyDBPath, KeyLogDir,
		KeyWikiPath, KeySkillsPath, KeySkillsInstallProjectDir, KeyMCPServersPath, KeyPromptOverlayPath,
		KeyWorkspaceRoot,
		KeyWebSearchProvider, KeySearXNGBaseURL, KeyQdrantURL,
		KeyGarageS3Endpoint, KeyGarageS3AccessKey, KeyGarageS3SecretKey,
	} {
		assertChanged(t, changes, key)
	}
	assertSetting(t, s, KeyHTTPPort, "0.0.0.0:8080")
	assertSetting(t, s, KeyEnvPath, "/data/.env")
	assertSetting(t, s, KeyDBPath, "/data/aura.db")
	assertSetting(t, s, KeyWikiPath, "/workspace/wiki")
	assertSetting(t, s, KeySkillsPath, "/workspace/skills")
	assertSetting(t, s, KeySkillsInstallProjectDir, "/workspace/skills")
	assertSetting(t, s, KeyMCPServersPath, "/workspace/mcp.json")
	assertSetting(t, s, KeyPromptOverlayPath, "/workspace")
	assertSetting(t, s, KeyWorkspaceRoot, "/workspace")
	assertSetting(t, s, KeyWebSearchProvider, "searxng")
	assertSetting(t, s, KeySearXNGBaseURL, "http://searxng:8080")
	assertSetting(t, s, KeyQdrantURL, "http://qdrant:6333")
	assertSetting(t, s, KeyGarageS3Endpoint, "http://garage:3900")
	assertSetting(t, s, KeyGarageS3AccessKey, "generated-access")
	assertSetting(t, s, KeyGarageS3SecretKey, "generated-secret")
}

func TestApplyBestDefaultsDoesNotTouchProviderSecrets(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	originals := map[string]string{
		KeyLLMAPIKey:         "sk-live",
		KeyLLMBaseURL:        "https://custom.example/v1",
		KeyLLMModel:          "custom/model",
		KeyEmbeddingAPIKey:   "embed-secret",
		KeyEmbeddingBaseURL:  "https://embeddings.example/v1",
		KeyEmbeddingModel:    "custom-embed",
		KeyMistralAPIKey:     "mistral-secret",
		KeyGarageS3AccessKey: "garage-access",
		KeyGarageS3SecretKey: "garage-secret",
	}
	for key, value := range originals {
		mustSet(t, s, key, value)
	}

	if _, err := ApplyBestDefaults(ctx, s, containerDefaultsConfig()); err != nil {
		t.Fatalf("ApplyBestDefaults: %v", err)
	}
	for key, value := range originals {
		assertSetting(t, s, key, value)
	}
}

func TestApplyBestDefaultsRepairsInvalidValuesAfterMigration(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	mustSet(t, s, BestDefaultsVersionKey, BestDefaultsVersion)
	mustSet(t, s, KeyWebSearchProvider, "unknown")
	mustSet(t, s, KeySkillRoutingMode, "llm")
	mustSet(t, s, KeyAgentLoopMaxSteps, "0")
	mustSet(t, s, KeyTerminalToolPolicy, "always")
	mustSet(t, s, KeyDelegationMode, "unbounded")
	mustSet(t, s, KeyTraceRetentionDays, "366")

	changes, err := ApplyBestDefaults(ctx, s, containerDefaultsConfig())
	if err != nil {
		t.Fatalf("ApplyBestDefaults: %v", err)
	}
	for _, key := range []string{
		KeyWebSearchProvider,
		KeySkillRoutingMode, KeyAgentLoopMaxSteps, KeyTerminalToolPolicy, KeyDelegationMode, KeyTraceRetentionDays,
	} {
		assertChanged(t, changes, key)
	}
	assertSetting(t, s, KeyWebSearchProvider, "searxng")
	assertSetting(t, s, KeySkillRoutingMode, config.DefaultSkillRoutingMode)
	assertSetting(t, s, KeyAgentLoopMaxSteps, strconv.Itoa(config.DefaultAgentLoopMaxSteps))
	assertSetting(t, s, KeyTerminalToolPolicy, config.DefaultTerminalToolPolicy)
	assertSetting(t, s, KeyDelegationMode, config.DefaultDelegationMode)
	assertSetting(t, s, KeyTraceRetentionDays, "30")
}

func TestApplyBestDefaultsMovesPromptOverlayFromDataToWorkspaceAfterMigration(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	mustSet(t, s, BestDefaultsVersionKey, BestDefaultsVersion)
	mustSet(t, s, KeyPromptOverlayPath, "/data")

	changes, err := ApplyBestDefaults(ctx, s, containerDefaultsConfig())
	if err != nil {
		t.Fatalf("ApplyBestDefaults: %v", err)
	}
	assertChanged(t, changes, KeyPromptOverlayPath)
	assertSetting(t, s, KeyPromptOverlayPath, "/workspace")
}

func containerDefaultsConfig() *config.Config {
	return &config.Config{
		Headless:                true,
		HTTPPort:                "0.0.0.0:8080",
		EnvPath:                 "/data/.env",
		DBPath:                  "/data/aura.db",
		LogDir:                  "/data/logs",
		WikiPath:                "/workspace/wiki",
		SkillsPath:              "/workspace/skills",
		SkillsInstallProjectDir: "/workspace/skills",
		MCPServersPath:          "/workspace/mcp.json",
		PromptOverlayPath:       "/workspace",
		WorkspaceRoot:           "/workspace",
		RuntimeWorkspacePath:    "/workspace",
		SearXNGBaseURL:          "http://searxng:8080",
		QdrantURL:               "http://qdrant:6333",
		GarageS3Endpoint:        "http://garage:3900",
		GarageS3AccessKey:       "generated-access",
		GarageS3SecretKey:       "generated-secret",
	}
}

func mustSet(t *testing.T, s *Store, key, value string) {
	t.Helper()
	if err := s.Set(context.Background(), key, value); err != nil {
		t.Fatalf("Set(%s): %v", key, err)
	}
}

func assertSetting(t *testing.T, s *Store, key, want string) {
	t.Helper()
	got, err := s.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get(%s): %v", key, err)
	}
	if got != want {
		t.Fatalf("Get(%s) = %q, want %q", key, got, want)
	}
}

func assertChanged(t *testing.T, changes []BestDefaultChange, key string) {
	t.Helper()
	if !slices.ContainsFunc(changes, func(change BestDefaultChange) bool {
		return change.Key == key
	}) {
		t.Fatalf("changes missing %s: %+v", key, changes)
	}
}
