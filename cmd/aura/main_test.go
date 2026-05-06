package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainRunsMigrationsBeforeStoreConstruction(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(data)
	openIdx := strings.Index(source, "auradb.Open(cfg.DBPath)")
	migrateIdx := strings.Index(source, "migrations.Run(context.Background(), pool)")
	settingsIdx := strings.Index(source, "settings.NewStoreWithDB(pool)")
	telegramIdx := strings.Index(source, "telegram.New(cfg, settingsStore, pool, logger)")

	if openIdx < 0 || migrateIdx < 0 || settingsIdx < 0 || telegramIdx < 0 {
		t.Fatalf("startup markers missing: open=%d migrate=%d settings=%d telegram=%d", openIdx, migrateIdx, settingsIdx, telegramIdx)
	}
	if !(openIdx < migrateIdx && migrateIdx < settingsIdx && settingsIdx < telegramIdx) {
		t.Fatalf("startup order invalid: open=%d migrate=%d settings=%d telegram=%d", openIdx, migrateIdx, settingsIdx, telegramIdx)
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
