package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainRunsMigrationsBeforeStoreConstruction(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	openIdx := strings.Index(source, "auradb.Open(openedDBPath)")
	migrateIdx := strings.Index(source, "migrations.Run(context.Background(), pool)")
	integrityIdx := strings.Index(source, "auradb.CheckIntegrity(context.Background(), pool)")
	settingsIdx := strings.Index(source, "settings.NewStoreWithDB(pool)")
	telegramIdx := strings.Index(source, "telegram.New(cfg, settingsStore, pool, logger")

	if openIdx < 0 || migrateIdx < 0 || integrityIdx < 0 || settingsIdx < 0 || telegramIdx < 0 {
		t.Fatalf("startup markers missing: open=%d migrate=%d integrity=%d settings=%d telegram=%d", openIdx, migrateIdx, integrityIdx, settingsIdx, telegramIdx)
	}
	if !(openIdx < migrateIdx && migrateIdx < integrityIdx && integrityIdx < settingsIdx && settingsIdx < telegramIdx) {
		t.Fatalf("startup order invalid: open=%d migrate=%d integrity=%d settings=%d telegram=%d", openIdx, migrateIdx, integrityIdx, settingsIdx, telegramIdx)
	}
}

func TestMainBootstrapsRuntimeLayoutBeforeDatabaseOpen(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	loadIdx := strings.Index(source, "cfg, err := config.Load()")
	bootstrapIdx := strings.Index(source, "runtimebootstrap.EnsureLayout(runtimebootstrap.LayoutConfig{")
	openIdx := strings.Index(source, "auradb.Open(openedDBPath)")

	if loadIdx < 0 || bootstrapIdx < 0 || openIdx < 0 {
		t.Fatalf("startup markers missing: load=%d bootstrap=%d open=%d", loadIdx, bootstrapIdx, openIdx)
	}
	if !(loadIdx < bootstrapIdx && bootstrapIdx < openIdx) {
		t.Fatalf("runtime layout must bootstrap after config load and before DB open: load=%d bootstrap=%d open=%d", loadIdx, bootstrapIdx, openIdx)
	}
}

func TestMainPassesRuntimeLayoutConfig(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	for _, want := range []string{
		`RuntimeWorkspacePath: cfg.RuntimeWorkspacePath`,
		`EnvPath:              cfg.EnvPath`,
		`DBPath:               cfg.DBPath`,
		`LogDir:               cfg.LogDir`,
		`WikiPath:             cfg.WikiPath`,
		`SkillsPath:           cfg.SkillsPath`,
		`MCPServersPath:       cfg.MCPServersPath`,
		`PromptOverlayPath:    cfg.PromptOverlayPath`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("main.go missing %q", want)
		}
	}
}

func TestMainStartsAuraBeforeTrayBlocks(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	goIdx := strings.Index(source, "go func() {")
	startIdx := strings.Index(source, "startAura(logger, cleanupLog, cfg)")
	trayIdx := strings.Index(source, "tray.Run(tray.Options{")

	if goIdx < 0 || startIdx < 0 || trayIdx < 0 {
		t.Fatalf("startup markers missing: go=%d start=%d tray=%d", goIdx, startIdx, trayIdx)
	}
	if !(goIdx < startIdx && startIdx < trayIdx) {
		t.Fatalf("tray blocks before Aura startup begins: go=%d start=%d tray=%d", goIdx, startIdx, trayIdx)
	}
}

func TestHeadlessModeBypassesTrayRun(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	headlessIdx := strings.Index(source, "if cfg.Headless {")
	runHeadlessIdx := strings.Index(source, "runHeadless(logger, cleanupLog, cfg)")
	trayIdx := strings.Index(source, "tray.Run(tray.Options{")

	if headlessIdx < 0 || runHeadlessIdx < 0 || trayIdx < 0 {
		t.Fatalf("headless markers missing: headless=%d runHeadless=%d tray=%d", headlessIdx, runHeadlessIdx, trayIdx)
	}
	if !(headlessIdx < runHeadlessIdx && runHeadlessIdx < trayIdx) {
		t.Fatalf("headless path must be checked before tray run: headless=%d runHeadless=%d tray=%d", headlessIdx, runHeadlessIdx, trayIdx)
	}
}

func TestMainUsesConfiguredEnvPath(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	for _, want := range []string{
		`initialEnvPath := config.EnvPathFromEnvironment()`,
		`loadDotEnv(initialEnvPath)`,
		`DotEnvPath:      cfg.EnvPath`,
		`loadDotEnv(cfg.EnvPath)`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("main.go missing %q", want)
		}
	}
}

func TestHandleCLIArgsPrintsHelpWithoutStartingAura(t *testing.T) {
	var out strings.Builder

	if !handleCLIArgs([]string{"--help"}, &out) {
		t.Fatal("handleCLIArgs(--help) = false, want true")
	}
	got := out.String()
	for _, want := range []string{"Aura ", "Usage:", "AURA_ENV_PATH"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help output = %q, missing %q", got, want)
		}
	}
}

func TestHandleCLIArgsPrintsVersionWithoutStartingAura(t *testing.T) {
	var out strings.Builder

	if !handleCLIArgs([]string{"--version"}, &out) {
		t.Fatal("handleCLIArgs(--version) = false, want true")
	}
	if !strings.Contains(out.String(), "Aura ") {
		t.Fatalf("version output = %q", out.String())
	}
}

func TestHandleCLIArgsIgnoresUnknownArgs(t *testing.T) {
	var out strings.Builder

	if handleCLIArgs([]string{"--not-a-real-flag"}, &out) {
		t.Fatal("handleCLIArgs(unknown) = true, want false")
	}
	if out.Len() != 0 {
		t.Fatalf("unknown arg wrote output %q", out.String())
	}
}

func TestMainKeepsOpenedDBPathAfterSettingsOverlay(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	for _, want := range []string{
		`openedDBPath := cfg.DBPath`,
		`auradb.Open(openedDBPath)`,
		`cfg.DBPath = openedDBPath`,
		`newCfg.DBPath = openedDBPath`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("main.go missing %q", want)
		}
	}
}

func TestLoadDotEnvDoesNotOverrideExistingEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("HTTP_PORT=127.0.0.1:8080\nTELEGRAM_TOKEN=from-file\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	t.Setenv("HTTP_PORT", "0.0.0.0:8080")
	os.Unsetenv("TELEGRAM_TOKEN")
	defer os.Unsetenv("TELEGRAM_TOKEN")

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	if got := os.Getenv("HTTP_PORT"); got != "0.0.0.0:8080" {
		t.Fatalf("HTTP_PORT = %q, want compose env preserved", got)
	}
	if got := os.Getenv("TELEGRAM_TOKEN"); got != "from-file" {
		t.Fatalf("TELEGRAM_TOKEN = %q, want file fallback", got)
	}
}

func TestComposeSetsContainerTimezone(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	source := string(data)
	if !strings.Contains(source, `AURA_TIMEZONE: "${AURA_TIMEZONE:-Europe/Rome}"`) {
		t.Fatalf("compose.yaml must set AURA_TIMEZONE so container wall-clock scheduling does not fall back to UTC")
	}
}

func TestComposeSetsContainerSQLiteJournalMode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	source := string(data)
	if !strings.Contains(source, `AURA_SQLITE_JOURNAL_MODE: "DELETE"`) {
		t.Fatalf("compose.yaml must set AURA_SQLITE_JOURNAL_MODE=DELETE so Docker bind-mounted SQLite avoids WAL shared-memory sidecars")
	}
}

func TestComposeMountsNarrowRuntimeWorkspace(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	source := string(data)
	for _, want := range []string{
		`AURA_RUNTIME_WORKSPACE_PATH: "/workspace"`,
		`AURA_WORKSPACE_ROOT: "/workspace"`,
		`PROMPT_OVERLAY_PATH: "/workspace"`,
		`WIKI_PATH: "/workspace/wiki"`,
		`SKILLS_PATH: "/workspace/skills"`,
		`MCP_SERVERS_PATH: "/workspace/mcp.json"`,
		`- ./runtime-workspace:/workspace`,
		`- aura-wiki:/workspace/wiki`,
		`- aura-skills:/workspace/skills`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("compose.yaml missing %q", want)
		}
	}
	for _, forbidden := range []struct {
		host      string
		container string
	}{
		{host: "wiki", container: "/workspace/wiki"},
		{host: "skills", container: "/workspace/skills"},
	} {
		bind := "- ./" + forbidden.host + ":" + forbidden.container
		if strings.Contains(source, bind) {
			t.Fatalf("compose.yaml must not bind-mount runtime data path %q", bind)
		}
	}
	if strings.Contains(source, `AURA_WORKSPACE_ROOT: "/app"`) {
		t.Fatalf("compose.yaml must not expose /app as Aura's bounded workspace root")
	}
}
