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
	startIdx := strings.Index(source, "go func() {\n\t\tstop, err := startAura(")
	trayIdx := strings.Index(source, "tray.Run(tray.Options{")

	if startIdx < 0 || trayIdx < 0 {
		t.Fatalf("startup markers missing: start=%d tray=%d", startIdx, trayIdx)
	}
	if startIdx > trayIdx {
		t.Fatalf("tray blocks before Aura startup begins: start=%d tray=%d", startIdx, trayIdx)
	}
}
