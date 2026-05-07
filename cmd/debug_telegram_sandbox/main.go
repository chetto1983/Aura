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
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	expectProfile := flag.String("expect-profile", "", "expected orchestration tool profile; used only for reporting/validation when set")
	expectNoTools := flag.Bool("expect-no-tools", false, "expect the synthetic Telegram turn to make no tool calls")
	expectSkillRead := flag.Bool("expect-skill-read", false, "expect the synthetic Telegram turn to call read_skill")
	expectSwarm := flag.Bool("expect-swarm", false, "expect the synthetic Telegram turn to use run_aurabot_swarm")
	expectTerminalSwarm := flag.Bool("expect-terminal-swarm", false, "expect swarm_research to finalize immediately after run_aurabot_swarm")
	expectTokenMetrics := flag.Bool("expect-token-metrics", false, "expect non-zero token usage metrics")
	expectSandbox := flag.Bool("expect-sandbox", false, "expect the synthetic Telegram turn to use the Python sandbox")
	maxElapsedMS := flag.Int64("max-elapsed-ms", 0, "fail if the synthetic Telegram turn exceeds this elapsed_ms budget")
	writeLiveDB := flag.Bool("write-live-db", false, "open the configured DB directly instead of a temporary copy; unsafe while Docker Aura is running")
	timeout := flag.Duration("timeout", 2*time.Minute, "smoke timeout")
	flag.Parse()
	if strings.TrimSpace(*prompt) == "" {
		if *artifactSmoke {
			*prompt = defaultArtifactSmokePrompt()
		} else {
			*prompt = defaultArithmeticSmokePrompt()
		}
	}

	envPath := envDefault("AURA_ENV_PATH", ".env")
	if err := loadDotEnv(envPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		fail("load .env: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		fail("load config: %v", err)
	}
	sourceDBPath := resolveDebugDBPath(envPath, cfg.DBPath)
	if *writeLiveDB {
		cfg.DBPath = sourceDBPath
	} else {
		dbPath, cleanup, err := prepareDebugDBCopy(sourceDBPath)
		if err != nil {
			fail("prepare temporary debug database: %v", err)
		}
		defer cleanup()
		cfg.DBPath = dbPath
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
	cfg.DBPath = resolveRuntimeDBPath(cfg.DBPath, sourceDBPath, *writeLiveDB)

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
	fmt.Printf("db_path=%s\n", cfg.DBPath)
	fmt.Printf("db_source_path=%s\n", sourceDBPath)
	fmt.Printf("db_write_live=%v\n", *writeLiveDB)
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
	fmt.Printf("profile_select_reason=%s\n", result.ProfileSelectReason)
	fmt.Printf("tools_exposed=%s\n", strings.Join(result.ToolsExposed, ","))
	fmt.Printf("hidden_tool_rejected=%v\n", result.HiddenToolRejected)
	fmt.Printf("skills_read=%v\n", result.SkillsRead)
	fmt.Printf("swarm_used=%v\n", result.SwarmUsed)
	fmt.Printf("sandbox_used=%v\n", result.SandboxUsed)
	fmt.Printf("terminal_swarm=%v\n", result.TerminalSwarm)
	fmt.Printf("swarm_finalization=%s\n", result.SwarmFinalization)
	fmt.Printf("post_swarm_tool_calls=%d\n", result.PostSwarmToolCalls)
	fmt.Printf("duplicate_swarm_rejected=%v\n", result.DuplicateSwarmRejected)
	fmt.Printf("worker_count=%d\n", result.WorkerCount)
	fmt.Printf("worker_failures=%d\n", result.WorkerFailures)
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
	fmt.Printf("elapsed_ms=%d\n", result.ElapsedMS)
	expectations := debugExpectations{
		Profile:       *expectProfile,
		Tools:         splitCSV(*expectTools),
		NoTools:       *expectNoTools,
		SkillRead:     *expectSkillRead,
		SwarmUsed:     *expectSwarm,
		TerminalSwarm: *expectTerminalSwarm,
		TokenMetrics:  *expectTokenMetrics,
		SandboxUsed:   *expectSandbox,
		MaxElapsedMS:  *maxElapsedMS,
	}
	if err := validateDebugExpectations(result, expectations); err != nil {
		fail("%v", err)
	}
	if len(expectations.Tools) > 0 {
		fmt.Printf("expected_tools_present=%s\n", strings.Join(expectations.Tools, ","))
	}
	if strings.TrimSpace(expectations.Profile) != "" {
		fmt.Printf("expected_profile_present=%s\n", strings.TrimSpace(expectations.Profile))
	}
	if expectations.NoTools {
		fmt.Printf("expected_no_tools=true\n")
	}
	if expectations.SkillRead {
		fmt.Printf("expected_skill_read=true\n")
	}
	if expectations.SwarmUsed {
		fmt.Printf("expected_swarm=true\n")
	}
	if expectations.TerminalSwarm {
		fmt.Printf("expected_terminal_swarm=true\n")
	}
	if expectations.TokenMetrics {
		fmt.Printf("expected_token_metrics=true\n")
	}
	if expectations.SandboxUsed {
		fmt.Printf("expected_sandbox=true\n")
	}
	if expectations.MaxElapsedMS > 0 {
		fmt.Printf("expected_max_elapsed_ms=%d\n", expectations.MaxElapsedMS)
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

func resolveDebugDBPath(envPath, dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" || filepath.IsAbs(dbPath) {
		return filepath.Clean(dbPath)
	}
	envDir := filepath.Dir(strings.TrimSpace(envPath))
	if envDir == "." || envDir == "" {
		return filepath.Clean(dbPath)
	}
	return filepath.Clean(filepath.Join(envDir, dbPath))
}

func prepareDebugDBCopy(sourcePath string) (string, func(), error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return "", func() {}, errors.New("DB path is empty")
	}
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", func() {}, err
	}
	tmpDir, err := os.MkdirTemp("", "aura-debug-db-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	dest := filepath.Join(tmpDir, filepath.Base(absSource))
	if err := copyDebugDBFile(absSource, dest); err != nil {
		cleanup()
		return "", func() {}, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyOptionalDebugDBSidecar(absSource+suffix, dest+suffix); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dest, cleanup, nil
}

func resolveRuntimeDBPath(openedDBPath, sourceDBPath string, writeLive bool) string {
	if writeLive {
		return filepath.Clean(strings.TrimSpace(sourceDBPath))
	}
	return filepath.Clean(strings.TrimSpace(openedDBPath))
}

func copyOptionalDebugDBSidecar(sourcePath, destPath string) error {
	if _, err := os.Stat(sourcePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return copyDebugDBFile(sourcePath, destPath)
}

func copyDebugDBFile(sourcePath, destPath string) error {
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
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

type debugExpectations struct {
	Profile       string
	Tools         []string
	NoTools       bool
	SkillRead     bool
	SwarmUsed     bool
	TerminalSwarm bool
	TokenMetrics  bool
	SandboxUsed   bool
	MaxElapsedMS  int64
}

func validateDebugExpectations(result telegram.DebugTextSmokeResult, expectations debugExpectations) error {
	if expectations.NoTools && len(expectations.Tools) > 0 {
		return errors.New("cannot combine -expect-no-tools with -expect-tools")
	}
	if profile := strings.TrimSpace(expectations.Profile); profile != "" && result.ToolProfile != profile {
		return fmt.Errorf("expected profile %q, got %q", profile, result.ToolProfile)
	}
	if len(expectations.Tools) > 0 {
		if missing := missingTools(result.ToolCalls, expectations.Tools); len(missing) > 0 {
			return fmt.Errorf("missing expected tools: %s", strings.Join(missing, ","))
		}
	}
	if expectations.NoTools && len(result.ToolCalls) > 0 {
		return fmt.Errorf("expected no tool calls, got %s", strings.Join(result.ToolCalls, ","))
	}
	if expectations.SkillRead && !result.SkillsRead {
		return errors.New("expected read_skill usage")
	}
	if expectations.SwarmUsed && !result.SwarmUsed {
		return errors.New("expected swarm usage")
	}
	if expectations.TerminalSwarm {
		if !result.TerminalSwarm {
			return errors.New("expected terminal swarm finalization")
		}
		if strings.TrimSpace(result.FinalText) == "" {
			return errors.New("expected terminal swarm final text")
		}
		if result.SwarmFinalization != "aggregate" && result.SwarmFinalization != "no_tool_llm" {
			return fmt.Errorf("expected known swarm finalization, got %q", result.SwarmFinalization)
		}
		if result.PostSwarmToolCalls != 0 {
			return fmt.Errorf("expected zero post-swarm tool calls, got %d", result.PostSwarmToolCalls)
		}
		if result.WorkerCount < 1 {
			return fmt.Errorf("expected worker_count >= 1, got %d", result.WorkerCount)
		}
		if result.WorkerCount > 3 {
			return fmt.Errorf("expected worker_count <= 3, got %d", result.WorkerCount)
		}
		if result.WorkerFailures != 0 {
			return fmt.Errorf("expected worker_failures=0, got %d", result.WorkerFailures)
		}
	}
	if expectations.TokenMetrics && !result.TokenUsageReported {
		return errors.New("expected token usage metrics")
	}
	if expectations.SandboxUsed && !result.SandboxUsed {
		return errors.New("expected sandbox usage")
	}
	if expectations.MaxElapsedMS > 0 && result.ElapsedMS > expectations.MaxElapsedMS {
		return fmt.Errorf("elapsed_ms %d exceeds budget %d", result.ElapsedMS, expectations.MaxElapsedMS)
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
