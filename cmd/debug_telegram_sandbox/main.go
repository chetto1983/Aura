// debug_telegram_sandbox runs one live LLM/tool-loop smoke through Aura's
// Telegram conversation handler using a synthetic incoming private text update.
//
// It does not start long polling. It does use the real Telegram Bot API for
// outgoing placeholder/tool/final messages to the selected allowlisted user.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/auth"
	"github.com/aura/aura/internal/config"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/orchestration"
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
	expectProfile := flag.String("expect-profile", "", "expected orchestration toolset; used only for reporting/validation when set")
	expectNoTools := flag.Bool("expect-no-tools", false, "expect the synthetic Telegram turn to make no tool calls")
	var expectSkillRead optionalCSVFlag
	flag.Var(&expectSkillRead, "expect-skill-read", "expect the synthetic Telegram turn to inspect a skill via file tools; optional comma-separated skill names or capabilities via -expect-skill-read=docx")
	expectSwarm := flag.Bool("expect-swarm", false, "expect the synthetic Telegram turn to use run_aurabot_swarm")
	expectHiddenToolRejected := flag.Bool("expect-hidden-tool-rejected", false, "expect a hidden tool call to be rejected by orchestration policy")
	expectTerminalTool := flag.String("expect-terminal-tool", "", "expected terminal tool name recorded by orchestration policy")
	expectLoopStepsMax := flag.Int("expect-loop-steps-max", 0, "fail if orchestration loop_steps exceeds this budget")
	expectTraceField := flag.String("expect-trace-field", "", "comma-separated DebugTextSmokeResult field names expected to be present/non-zero")
	expectNoStaleSkillRef := flag.Bool("expect-no-stale-skill-ref", false, "expect debug result skill/reference fields to contain no stale worker aliases or legacy .yaml/.yml refs")
	expectTokenMetrics := flag.Bool("expect-token-metrics", false, "expect non-zero token usage metrics")
	expectSandbox := flag.Bool("expect-sandbox", false, "expect the synthetic Telegram turn to use the Python sandbox")
	maxElapsedMS := flag.Int64("max-elapsed-ms", 0, "fail if the synthetic Telegram turn exceeds this elapsed_ms budget")
	writeLiveDB := flag.Bool("write-live-db", false, "open the configured DB directly instead of a temporary copy; unsafe while Docker Aura is running")
	timeout := flag.Duration("timeout", 2*time.Minute, "smoke timeout")
	flag.Parse()
	customPrompt := strings.TrimSpace(*prompt) != ""
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
	if !*writeLiveDB {
		wikiPath, cleanup, err := prepareDebugWikiCopy(cfg.WikiPath)
		if err != nil {
			fail("prepare temporary debug wiki: %v", err)
		}
		defer cleanup()
		cfg.WikiPath = wikiPath
		cfg.SummarizerMode = "off"
	}

	userID := strings.TrimSpace(*userIDFlag)
	if userID == "" {
		userID, err = firstAllowedUserID(cfg.DBPath, cfg.Allowlist)
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
	fmt.Printf("wiki_path=%s\n", cfg.WikiPath)
	fmt.Printf("model=%s base_url=%s\n", cfg.LLMModel, cfg.LLMBaseURL)
	fmt.Printf("runtime_dir=%s sandbox_enabled=%v\n", cfg.SandboxRuntimeDir, cfg.SandboxEnabled)
	fmt.Printf("prompt=%q\n\n", redactForReport(*prompt))

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
	fmt.Printf("active_capabilities=%s\n", strings.Join(result.ActiveCapabilities, ","))
	fmt.Printf("hidden_tool_rejected=%v\n", result.HiddenToolRejected)
	fmt.Printf("skill_preflight_failed=%v\n", result.SkillPreflightFailed)
	fmt.Printf("skills_read=%v\n", result.SkillsRead)
	if len(result.ReadSkills) > 0 {
		fmt.Printf("read_skills=%s\n", strings.Join(result.ReadSkills, ","))
	}
	fmt.Printf("loop_steps=%d\n", result.LoopSteps)
	fmt.Printf("swarm_used=%v\n", result.SwarmUsed)
	fmt.Printf("sandbox_used=%v\n", result.SandboxUsed)
	fmt.Printf("terminal_tool=%s\n", result.TerminalTool)
	fmt.Printf("duplicate_tool_call_rejected=%v\n", result.DuplicateToolCallRejected)
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
		fmt.Printf("document=%s size=%d caption=%q\n", send.Filename, send.SizeBytes, singleLine(redactForReport(send.Caption), 160))
	}
	if result.FinalText != "" {
		fmt.Printf("final=%s\n", singleLine(redactForReport(result.FinalText), 500))
	}
	fmt.Printf("token_usage_reported=%v\n", result.TokenUsageReported)
	fmt.Printf("tokens_prompt=%d\n", result.TokensPrompt)
	fmt.Printf("tokens_completion=%d\n", result.TokensCompletion)
	fmt.Printf("tokens_total=%d\n", result.TokensTotal)
	fmt.Printf("estimated_context_tokens=%d\n", result.EstimatedContextTokens)
	fmt.Printf("cost_usd=%.6f\n", result.CostUSD)
	fmt.Printf("elapsed_ms=%d\n", result.ElapsedMS)
	expectations := debugExpectations{
		Profile:            *expectProfile,
		Tools:              splitCSV(*expectTools),
		NoTools:            *expectNoTools,
		SkillRead:          expectSkillRead.Any,
		SkillReadNames:     expectSkillRead.Values,
		SwarmUsed:          *expectSwarm,
		HiddenToolRejected: *expectHiddenToolRejected,
		TerminalTool:       *expectTerminalTool,
		MaxLoopSteps:       *expectLoopStepsMax,
		TraceFields:        splitCSV(*expectTraceField),
		NoStaleSkillRef:    *expectNoStaleSkillRef,
		TokenMetrics:       *expectTokenMetrics,
		SandboxUsed:        *expectSandbox,
		MaxElapsedMS:       *maxElapsedMS,
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
	if expectations.SkillRead || len(expectations.SkillReadNames) > 0 {
		if len(expectations.SkillReadNames) > 0 {
			fmt.Printf("expected_skill_read=%s\n", strings.Join(expectations.SkillReadNames, ","))
		} else {
			fmt.Printf("expected_skill_read=true\n")
		}
	}
	if expectations.SwarmUsed {
		fmt.Printf("expected_swarm=true\n")
	}
	if expectations.HiddenToolRejected {
		fmt.Printf("expected_hidden_tool_rejected=true\n")
	}
	if strings.TrimSpace(expectations.TerminalTool) != "" {
		fmt.Printf("expected_terminal_tool=%s\n", strings.TrimSpace(expectations.TerminalTool))
	}
	if expectations.MaxLoopSteps > 0 {
		fmt.Printf("expected_loop_steps_max=%d\n", expectations.MaxLoopSteps)
	}
	if len(expectations.TraceFields) > 0 {
		fmt.Printf("expected_trace_fields_present=%s\n", strings.Join(expectations.TraceFields, ","))
	}
	if expectations.NoStaleSkillRef {
		fmt.Printf("expected_no_stale_skill_ref=true\n")
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
		if err := validateTelegramSandboxSmoke(result, *artifactSmoke, customPrompt); err != nil {
			fail("%v", err)
		}
	}
	if *artifactSmoke {
		fmt.Println("PASS: synthetic Telegram turn used execute_code and delivered a sandbox artifact document")
		return
	}
	if *noValidate {
		fmt.Println("PASS: synthetic Telegram turn completed; legacy execute_code assertion skipped")
	} else if customPrompt {
		fmt.Println("PASS: synthetic Telegram turn used execute_code and satisfied explicit expectations")
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

func prepareDebugWikiCopy(sourcePath string) (string, func(), error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return "", func() {}, errors.New("wiki path is empty")
	}
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", func() {}, err
	}
	info, err := os.Stat(absSource)
	if err != nil {
		return "", func() {}, err
	}
	if !info.IsDir() {
		return "", func() {}, fmt.Errorf("wiki path is not a directory: %s", absSource)
	}
	tmpDir, err := os.MkdirTemp("", "aura-debug-wiki-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	if err := copyDebugDir(absSource, tmpDir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmpDir, cleanup, nil
}

func copyDebugDir(sourcePath, destPath string) error {
	return filepath.WalkDir(sourcePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(destPath, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeType != 0 {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
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

func validateTelegramSandboxSmoke(result telegram.DebugTextSmokeResult, artifactSmoke bool, customPrompt bool) error {
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
	if customPrompt {
		return nil
	}
	if !result.Contains5050 {
		return errors.New("expected final/tool output containing 5050")
	}
	return nil
}

type debugExpectations struct {
	Profile            string
	Tools              []string
	NoTools            bool
	SkillRead          bool
	SkillReadNames     []string
	SwarmUsed          bool
	HiddenToolRejected bool
	TerminalTool       string
	MaxLoopSteps       int
	TraceFields        []string
	NoStaleSkillRef    bool
	TokenMetrics       bool
	SandboxUsed        bool
	MaxElapsedMS       int64
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
	if (expectations.SkillRead || len(expectations.SkillReadNames) > 0) && !result.SkillsRead {
		return errors.New("expected skill file read")
	}
	for _, expected := range expectations.SkillReadNames {
		if !expectedSkillReadSatisfied(result, expected) {
			return fmt.Errorf("expected skill file read for %q, got read_skills=%s active_capabilities=%s", expected, strings.Join(result.ReadSkills, ","), strings.Join(result.ActiveCapabilities, ","))
		}
	}
	if expectations.SwarmUsed && !result.SwarmUsed {
		return errors.New("expected swarm usage")
	}
	if expectations.HiddenToolRejected && !result.HiddenToolRejected {
		return errors.New("expected hidden tool rejection")
	}
	if terminalTool := strings.TrimSpace(expectations.TerminalTool); terminalTool != "" && result.TerminalTool != terminalTool {
		return fmt.Errorf("expected terminal tool %q, got %q", terminalTool, result.TerminalTool)
	}
	if expectations.MaxLoopSteps > 0 && result.LoopSteps > expectations.MaxLoopSteps {
		return fmt.Errorf("loop_steps %d exceeds budget %d", result.LoopSteps, expectations.MaxLoopSteps)
	}
	for _, field := range expectations.TraceFields {
		if err := expectResultFieldPresent(result, field); err != nil {
			return err
		}
	}
	if expectations.NoStaleSkillRef {
		if stale := staleDebugReferences(result); len(stale) > 0 {
			return fmt.Errorf("stale skill/reference values found: %s", strings.Join(stale, ","))
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

func expectedSkillReadSatisfied(result telegram.DebugTextSmokeResult, expected string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return true
	}
	for _, read := range result.ReadSkills {
		if strings.ToLower(strings.TrimSpace(read)) == expected {
			return true
		}
	}
	capability := orchestration.Capability(expected)
	req, ok := orchestration.SkillRequirementForCapability(capability, orchestration.SkillPreflightRequired)
	if !ok {
		return false
	}
	for _, active := range result.ActiveCapabilities {
		if strings.ToLower(strings.TrimSpace(active)) != expected {
			continue
		}
		decision := orchestration.NeedsSkillPreflight("", capability, "", orchestration.SkillPreflightState{
			ReadSkillNames: result.ReadSkills,
		}, orchestration.SkillPreflightRequired)
		if decision.Satisfied {
			return true
		}
		for _, read := range result.ReadSkills {
			for _, hint := range append(req.AllowedSkillNames, req.SkillHints...) {
				if strings.ToLower(strings.TrimSpace(read)) == strings.ToLower(strings.TrimSpace(hint)) {
					return true
				}
			}
		}
	}
	return false
}

func expectResultFieldPresent(result telegram.DebugTextSmokeResult, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	v := reflect.ValueOf(result)
	field := v.FieldByName(name)
	if !field.IsValid() {
		return fmt.Errorf("unknown trace/result field %q", name)
	}
	if field.IsZero() {
		return fmt.Errorf("expected trace/result field %q to be present", name)
	}
	return nil
}

func staleDebugReferences(result telegram.DebugTextSmokeResult) []string {
	var stale []string
	for _, value := range appendDebugReferenceValues(nil, result) {
		if isStaleDebugReference(value) {
			stale = append(stale, value)
		}
	}
	return stale
}

func appendDebugReferenceValues(values []string, result telegram.DebugTextSmokeResult) []string {
	values = append(values, result.ToolCalls...)
	values = append(values, result.ToolsExposed...)
	values = append(values, result.PromptModules...)
	values = append(values, result.ActiveCapabilities...)
	values = append(values, result.ReadSkills...)
	if result.TerminalTool != "" {
		values = append(values, result.TerminalTool)
	}
	return values
}

func isStaleDebugReference(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "golem" ||
		strings.Contains(normalized, "golem.yaml") ||
		strings.HasSuffix(normalized, ".yaml") ||
		strings.HasSuffix(normalized, ".yml")
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

func firstAllowedUserID(dbPath string, preferred []string) (string, error) {
	store, err := scheduler.OpenStore(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()

	rows, err := store.DB().QueryContext(context.Background(),
		`SELECT user_id, source FROM allowed_users ORDER BY created_at ASC`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type allowedCandidate struct {
		userID string
		source string
	}
	var real []allowedCandidate
	var synthetic []allowedCandidate
	for rows.Next() {
		var candidate allowedCandidate
		if err := rows.Scan(&candidate.userID, &candidate.source); err != nil {
			return "", err
		}
		candidate.userID = strings.TrimSpace(candidate.userID)
		candidate.source = strings.TrimSpace(candidate.source)
		if candidate.userID == "" {
			continue
		}
		if candidate.source == auth.SourceE2EBootstrap {
			synthetic = append(synthetic, candidate)
			continue
		}
		real = append(real, candidate)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(real) == 0 && len(synthetic) == 0 {
		return "", errors.New("allowed_users is empty; bootstrap Telegram /start first")
	}
	if len(real) == 0 {
		return "", errors.New("allowed_users only contains e2e_bootstrap users; pass -user with a real Telegram user ID or send /start from Telegram first")
	}
	for _, id := range preferred {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		for _, candidate := range real {
			if candidate.userID == id {
				return candidate.userID, nil
			}
		}
	}
	return real[0].userID, nil
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

func redactForReport(s string) string {
	out := orchestration.RedactTraceValue(s)
	for _, marker := range []string{
		"LLM_API_KEY=",
		"EMBEDDING_API_KEY=",
		"TELEGRAM_BOT_TOKEN=",
		"DASHBOARD_TOKEN=",
		"Authorization: Bearer ",
		"Bearer ",
	} {
		out = redactAfterMarker(out, marker)
	}
	return out
}

func redactAfterMarker(s, marker string) string {
	var b strings.Builder
	offset := 0
	for {
		idx := strings.Index(s[offset:], marker)
		if idx < 0 {
			b.WriteString(s[offset:])
			return b.String()
		}
		idx += offset
		start := idx + len(marker)
		end := start
		for end < len(s) {
			switch s[end] {
			case ' ', '\t', '\r', '\n', '"', '\'', ',', ';':
				goto done
			default:
				end++
			}
		}
	done:
		b.WriteString(s[offset:start])
		b.WriteString("[REDACTED]")
		offset = end
	}
}

type optionalCSVFlag struct {
	Any    bool
	Values []string
}

func (f *optionalCSVFlag) String() string {
	if f == nil {
		return ""
	}
	if len(f.Values) > 0 {
		return strings.Join(f.Values, ",")
	}
	if f.Any {
		return "true"
	}
	return "false"
}

func (f *optionalCSVFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "true") {
		f.Any = true
		return nil
	}
	if strings.EqualFold(value, "false") {
		f.Any = false
		f.Values = nil
		return nil
	}
	f.Any = true
	f.Values = splitCSV(value)
	return nil
}

func (f *optionalCSVFlag) IsBoolFlag() bool { return true }

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
