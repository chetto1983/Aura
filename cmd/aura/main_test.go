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
