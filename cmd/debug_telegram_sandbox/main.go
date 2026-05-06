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
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/telegram"
)

func main() {
	userIDFlag := flag.String("user", "", "Telegram user ID to smoke; defaults to first allowed_users row")
	username := flag.String("username", "", "optional Telegram username for the synthetic update")
	prompt := flag.String("prompt", "", "synthetic incoming Telegram text")
	artifactSmoke := flag.Bool("artifact-smoke", false, "require execute_code to create and deliver a sandbox artifact document")
	noValidate := flag.Bool("no-validate", false, "print Telegram-like logs and result without enforcing the legacy execute_code smoke assertion")
	expectTools := flag.String("expect-tools", "", "comma-separated tool names expected in the synthetic Telegram turn; used only for reporting/validation when set")
	timeout := flag.Duration("timeout", 2*time.Minute, "smoke timeout")
	flag.Parse()
	if strings.TrimSpace(*prompt) == "" {
		if *artifactSmoke {
			*prompt = defaultArtifactSmokePrompt()
		} else {
			*prompt = defaultArithmeticSmokePrompt()
		}
	}

	if err := loadDotEnv(envDefault("AURA_ENV_PATH", ".env")); err != nil && !errors.Is(err, os.ErrNotExist) {
		fail("load .env: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		fail("load config: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	pool, err := auradb.Open(cfg.DBPath)
	if err != nil {
		fail("open database: %v", err)
	}
	defer pool.Close()
	if err := migrations.Run(context.Background(), pool); err != nil {
		logger.Error("failed to migrate database", "error", err, "db_path", cfg.DBPath)
		os.Exit(1)
	}
	settingsStore, err := settings.NewStoreWithDB(pool)
	if err != nil {
		fail("open settings store: %v", err)
	}
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

	bot, err := telegram.New(cfg, settingsStore, pool, logger)
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
	fmt.Printf("prompt_version=%s\n", result.PromptVersion)
	fmt.Printf("prompt_hash=%s\n", result.PromptHash)
	fmt.Printf("prompt_modules=%s\n", strings.Join(result.PromptModules, ","))
	fmt.Printf("tool_profile=%s\n", result.ToolProfile)
	fmt.Printf("tools_exposed=%s\n", strings.Join(result.ToolsExposed, ","))
	fmt.Printf("skills_read=%v\n", result.SkillsRead)
	fmt.Printf("swarm_used=%v\n", result.SwarmUsed)
	fmt.Printf("sandbox_used=%v\n", result.SandboxUsed)
	fmt.Printf("called_execute_code=%v\n", result.CalledExecuteCode)
	fmt.Printf("contains_5050=%v\n", result.Contains5050)
	fmt.Printf("contains_artifact_metadata=%v\n", result.ContainsArtifactMetadata)
	if len(result.ArtifactFilenames) > 0 {
		fmt.Printf("artifact_filenames=%s\n", strings.Join(result.ArtifactFilenames, ","))
	}
	if len(result.ArtifactSourceIDs) > 0 {
		fmt.Printf("artifact_source_ids=%s\n", strings.Join(result.ArtifactSourceIDs, ","))
	}
	fmt.Printf("document_sends=%d\n", len(result.DocumentSends))
	for _, send := range result.DocumentSends {
		fmt.Printf("document=%s size=%d caption=%q\n", send.Filename, send.SizeBytes, singleLine(send.Caption, 160))
	}
	if result.FinalText != "" {
		fmt.Printf("final=%s\n", singleLine(result.FinalText, 500))
	}
	fmt.Printf("token_usage_reported=%v\n", result.TokenUsageReported)
	fmt.Printf("tokens_prompt=%d\n", result.TokensPrompt)
	fmt.Printf("tokens_completion=%d\n", result.TokensCompletion)
	fmt.Printf("tokens_total=%d\n", result.TokensTotal)
	fmt.Printf("estimated_context_tokens=%d\n", result.EstimatedContextTokens)
	fmt.Printf("cost_usd=%.6f\n", result.CostUSD)
	if strings.TrimSpace(*expectTools) != "" {
		expected := splitCSV(*expectTools)
		if missing := missingTools(result.ToolCalls, expected); len(missing) > 0 {
			fail("missing expected tools: %s", strings.Join(missing, ","))
		}
		fmt.Printf("expected_tools_present=%s\n", strings.Join(expected, ","))
	}
	if !*noValidate {
		if err := validateTelegramSandboxSmoke(result, *artifactSmoke); err != nil {
			fail("%v", err)
		}
	}
	if *artifactSmoke {
		fmt.Println("PASS: synthetic Telegram turn used execute_code and delivered a sandbox artifact document")
		return
	}
	if *noValidate {
		fmt.Println("PASS: synthetic Telegram turn completed; legacy execute_code assertion skipped")
	} else {
		fmt.Println("PASS: synthetic Telegram turn used execute_code and surfaced 5050")
	}
}

func defaultArithmeticSmokePrompt() string {
	return "Use execute_code to compute sum(range(1, 101)) and tell me the result."
}

func defaultArtifactSmokePrompt() string {
	return strings.Join([]string{
		"Use execute_code to create two computed sandbox artifacts, not a hello-world text file.",
		"In Python, build a tiny sales DataFrame with months Jan-Apr, revenue, cost, profit, and margin.",
		"Write the CSV exactly to /tmp/aura_out/aura_sales_summary.csv.",
		"Also create a matplotlib chart and save it exactly to /tmp/aura_out/aura_sales_plot.png.",
		"Then tell me both files were sent and persisted.",
		"Do not use create_xlsx, create_docx, or create_pdf.",
	}, " ")
}

func validateTelegramSandboxSmoke(result telegram.DebugTextSmokeResult, artifactSmoke bool) error {
	if !result.CalledExecuteCode {
		return errors.New("expected execute_code call")
	}
	if artifactSmoke {
		if !result.ContainsArtifactMetadata {
			return errors.New("expected execute_code artifact metadata")
		}
		if !hasAll(result.ArtifactFilenames, "aura_sales_summary.csv", "aura_sales_plot.png") {
			return errors.New("expected rich artifact filenames aura_sales_summary.csv and aura_sales_plot.png")
		}
		if len(result.DocumentSends) == 0 {
			return errors.New("expected at least one Telegram document delivery")
		}
		if !hasDocumentSends(result.DocumentSends, "aura_sales_summary.csv", "aura_sales_plot.png") {
			return errors.New("expected Telegram document delivery for both rich artifacts")
		}
		if len(result.ArtifactSourceIDs) < 2 {
			return errors.New("expected sandbox artifact source persistence")
		}
		return nil
	}
	if !result.Contains5050 {
		return errors.New("expected final/tool output containing 5050")
	}
	return nil
}

func hasAll(got []string, want ...string) bool {
	seen := make(map[string]bool, len(got))
	for _, item := range got {
		seen[item] = true
	}
	for _, item := range want {
		if !seen[item] {
			return false
		}
	}
	return true
}

func hasDocumentSends(sends []telegram.DebugDocumentSend, filenames ...string) bool {
	seen := make(map[string]bool, len(sends))
	for _, send := range sends {
		seen[send.Filename] = true
	}
	for _, filename := range filenames {
		if !seen[filename] {
			return false
		}
	}
	return true
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if item := strings.TrimSpace(part); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func missingTools(got, expected []string) []string {
	var missing []string
	for _, want := range expected {
		if !hasAll(got, want) {
			missing = append(missing, want)
		}
	}
	return missing
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

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
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
