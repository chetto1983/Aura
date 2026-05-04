// debug_telegram_sandbox runs one live LLM/tool-loop smoke through Aura's
// Telegram conversation handler using a synthetic incoming private text update.
//
// It does not start long polling. It does use the real Telegram Bot API for
// outgoing placeholder/tool/final messages to the selected allowlisted user.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/telegram"

	_ "modernc.org/sqlite"
)

func main() {
	userIDFlag := flag.String("user", "", "Telegram user ID to smoke; defaults to first allowed_users row")
	username := flag.String("username", "", "optional Telegram username for the synthetic update")
	prompt := flag.String("prompt", "", "synthetic incoming Telegram text")
	artifactSmoke := flag.Bool("artifact-smoke", false, "require execute_code to create and deliver a sandbox artifact document")
	timeout := flag.Duration("timeout", 2*time.Minute, "smoke timeout")
	flag.Parse()
	if strings.TrimSpace(*prompt) == "" {
		if *artifactSmoke {
			*prompt = defaultArtifactSmokePrompt()
		} else {
			*prompt = defaultArithmeticSmokePrompt()
		}
	}

	if err := loadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fail("load .env: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		fail("load config: %v", err)
	}
	settingsStore, err := settings.OpenStore(cfg.DBPath)
	if err != nil {
		fail("open settings store: %v", err)
	}
	defer settingsStore.Close()
	settings.ApplyToConfig(context.Background(), settingsStore, cfg)

	userID := strings.TrimSpace(*userIDFlag)
	if userID == "" {
		userID, err = firstAllowedUserID(cfg.DBPath)
		if err != nil {
			fail("resolve first allowed user: %v", err)
		}
	}
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		fail("parse user id %q: %v", userID, err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	bot, err := telegram.New(cfg, settingsStore, logger)
	if err != nil {
		fail("create telegram bot: %v", err)
	}
	defer bot.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fmt.Printf("Aura Telegram sandbox smoke\n")
	fmt.Printf("user_id=%s\n", userID)
	fmt.Printf("model=%s base_url=%s\n", cfg.LLMModel, cfg.LLMBaseURL)
	fmt.Printf("runtime_dir=%s sandbox_enabled=%v\n", cfg.SandboxRuntimeDir, cfg.SandboxEnabled)
	fmt.Printf("prompt=%q\n\n", *prompt)

	result, err := bot.RunDebugTextSmoke(ctx, uid, *username, *prompt)
	if err != nil {
		fail("run debug text smoke: %v", err)
	}
	fmt.Printf("tool_calls=%s\n", strings.Join(result.ToolCalls, ","))
	fmt.Printf("called_execute_code=%v\n", result.CalledExecuteCode)
	fmt.Printf("contains_5050=%v\n", result.Contains5050)
	fmt.Printf("contains_artifact_metadata=%v\n", result.ContainsArtifactMetadata)
	if len(result.ArtifactFilenames) > 0 {
		fmt.Printf("artifact_filenames=%s\n", strings.Join(result.ArtifactFilenames, ","))
	}
	fmt.Printf("document_sends=%d\n", len(result.DocumentSends))
	for _, send := range result.DocumentSends {
		fmt.Printf("document=%s size=%d caption=%q\n", send.Filename, send.SizeBytes, singleLine(send.Caption, 160))
	}
	if result.FinalText != "" {
		fmt.Printf("final=%s\n", singleLine(result.FinalText, 500))
	}
	if err := validateTelegramSandboxSmoke(result, *artifactSmoke); err != nil {
		fail("%v", err)
	}
	if *artifactSmoke {
		fmt.Println("PASS: synthetic Telegram turn used execute_code and delivered a sandbox artifact document")
		return
	}
	fmt.Println("PASS: synthetic Telegram turn used execute_code and surfaced 5050")
}

func defaultArithmeticSmokePrompt() string {
	return "Use execute_code to compute sum(range(1, 101)) and tell me the result."
}

func defaultArtifactSmokePrompt() string {
	return "Use execute_code to create a small text artifact. In Python, write exactly 'hello from Aura artifact smoke' to /tmp/aura_out/aura_artifact.txt, then tell me when it has been sent. Do not use create_xlsx, create_docx, or create_pdf."
}

func validateTelegramSandboxSmoke(result telegram.DebugTextSmokeResult, artifactSmoke bool) error {
	if !result.CalledExecuteCode {
		return errors.New("expected execute_code call")
	}
	if artifactSmoke {
		if !result.ContainsArtifactMetadata {
			return errors.New("expected execute_code artifact metadata")
		}
		if len(result.DocumentSends) == 0 {
			return errors.New("expected at least one Telegram document delivery")
		}
		return nil
	}
	if !result.Contains5050 {
		return errors.New("expected final/tool output containing 5050")
	}
	return nil
}

func firstAllowedUserID(dbPath string) (string, error) {
	store, err := scheduler.OpenStore(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()

	var userID string
	err = store.DB().QueryRowContext(context.Background(),
		`SELECT user_id FROM allowed_users ORDER BY created_at ASC LIMIT 1`).
		Scan(&userID)
	if err == sql.ErrNoRows {
		return "", errors.New("allowed_users is empty; bootstrap Telegram /start first")
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func singleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
