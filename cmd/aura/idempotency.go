package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

type cliMutationMeta struct {
	Scope     idempotency.Scope
	Normalize string
	KeyPolicy string
}

const cliExplicitOrGeneratedKey = "explicit_or_generated"

const (
	cliIdempotencyChildEnv  = "AURA_CLI_IDEMPOTENCY_CHILD"
	cliReplayRetention      = 30 * 24 * time.Hour
	maxCLIReplayStreamBytes = 96 << 10
)

type cliOperationRegistry interface {
	Begin(context.Context, idempotency.BeginRequest) (idempotency.BeginDecision, error)
	Complete(context.Context, idempotency.CompleteRequest) error
	MarkIndeterminate(context.Context, idempotency.OperationKey, [32]byte) error
}

type cliCommandExecutor func(context.Context, []string, io.Writer, io.Writer) int

type cliReplayEnvelope struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// cliInvocationContext is initialized once by main before command dispatch. CLI
// composition roots that accept a context can consume this value so retries keep
// the same operation while creating fresh audit/request IDs beneath it.
var cliInvocationContext = context.Background()

var cliMutationCommands = map[string]cliMutationMeta{
	"chat archive":              cliMutationMetaFor("chat_archive"),
	"chat delete":               cliMutationMetaFor("chat_delete"),
	"chat new":                  cliMutationMetaFor("chat_new"),
	"chat rename":               cliMutationMetaFor("chat_rename"),
	"chat resume":               cliMutationMetaFor("chat_resume"),
	"chat unarchive":            cliMutationMetaFor("chat_unarchive"),
	"config set":                cliMutationMetaFor("config_set"),
	"db migrate":                cliMutationMetaFor("db_migrate"),
	"db reset":                  cliMutationMetaFor("db_reset"),
	"docs ingest":               cliMutationMetaFor("docs_ingest"),
	"documents backfill":        cliMutationMetaFor("documents_backfill"),
	"identity grant":            cliMutationMetaFor("identity_grant"),
	"identity recover":          cliMutationMetaFor("identity_recover"),
	"identity recover-operator": cliMutationMetaFor("identity_recover_operator"),
	"identity revoke":           cliMutationMetaFor("identity_revoke"),
	"mcp add":                   cliMutationMetaFor("mcp_add"),
	"mcp disable":               cliMutationMetaFor("mcp_disable"),
	"mcp enable":                cliMutationMetaFor("mcp_enable"),
	"mcp install":               cliMutationMetaFor("mcp_install"),
	"mcp remove":                cliMutationMetaFor("mcp_remove"),
	"mcp trust":                 cliMutationMetaFor("mcp_trust"),
	"memory add-entity":         cliMutationMetaFor("memory_add_entity"),
	"memory add-fact":           cliMutationMetaFor("memory_add_fact"),
	"memory add-preference":     cliMutationMetaFor("memory_add_preference"),
	"memory forget":             cliMutationMetaFor("memory_forget"),
	"memory relationship":       cliMutationMetaFor("memory_relationship"),
	"memory store-message":      cliMutationMetaFor("memory_store_message"),
	"neo4j cypher":              cliMutationMetaFor("neo4j_cypher"),
	"neo4j migrate":             cliMutationMetaFor("neo4j_migrate"),
	"neo4j reset":               cliMutationMetaFor("neo4j_reset"),
	"objectstore bootstrap":     cliMutationMetaFor("objectstore_bootstrap"),
	"paused-states purge":       cliMutationMetaFor("paused_states_purge"),
	"retention apply":           cliMutationMetaFor("retention_apply"),
	"skills always":             cliMutationMetaFor("skill_always"),
	"skills approve":            cliMutationMetaFor("skill_approve"),
	"skills create":             cliMutationMetaFor("skill_create"),
	"skills delete":             cliMutationMetaFor("skill_delete"),
	"skills update":             cliMutationMetaFor("skill_update"),
	"task approve":              cliMutationMetaFor("task_approve"),
	"task cancel":               cliMutationMetaFor("task_cancel"),
	"task run_now":              cliMutationMetaFor("task_run_now"),
	"task schedule":             cliMutationMetaFor("task_schedule"),
}

func cliMutationMetaFor(normalizer string) cliMutationMeta {
	return cliMutationMeta{Scope: idempotency.ScopeCLICommand, Normalize: normalizer, KeyPolicy: cliExplicitOrGeneratedKey}
}

// prepareCLIIdempotency removes Aura's cross-command --operation-key flag and
// creates one stable operation before the selected command starts. One-shot CLI
// calls without a key receive a generated key that is printed before execution.
func prepareCLIIdempotency(ctx context.Context, args []string, output io.Writer) (context.Context, []string, error) {
	command, mutating := cliMutationPath(args)
	cleaned, explicitKey, err := removeOperationKeyFlag(args)
	if err != nil {
		return nil, nil, err
	}
	if !mutating {
		if explicitKey != "" {
			return nil, nil, errors.New("--operation-key is only valid for mutating commands")
		}
		return ctx, cleaned, nil
	}
	key := explicitKey
	if key == "" {
		key = uuid.NewString()
		if output != nil {
			if _, err := fmt.Fprintf(output, "operation-key: %s\n", key); err != nil {
				return nil, nil, fmt.Errorf("display generated operation key: %w", err)
			}
		}
	}
	operationKey := idempotency.OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: idempotency.ScopeCLICommand, Key: key}
	if err := operationKey.Validate(); err != nil {
		return nil, nil, fmt.Errorf("--operation-key: %w", err)
	}
	fingerprint, err := idempotency.FingerprintTyped(struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}{Command: command, Args: append([]string(nil), cleaned[2:]...)})
	if err != nil {
		return nil, nil, err
	}
	trusted := identityctx.WithIdentityID(ctx, identityctx.LocalOperatorIdentity)
	ctx, err = idempotency.WithOperation(trusted, idempotency.Operation{Key: operationKey, Fingerprint: fingerprint})
	if err != nil {
		return nil, nil, err
	}
	return ctx, cleaned, nil
}

func cliOperationFromContext(ctx context.Context) (string, [32]byte, bool) {
	operation, ok := idempotency.OperationFromContext(ctx)
	return operation.Key.Key, operation.Fingerprint, ok
}

func executeCLIIdempotentParent(ctx context.Context, args []string, stdout, stderr io.Writer) (bool, int) {
	if _, mutating := cliMutationPath(args); !mutating {
		return false, 0
	}
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "operation registry unavailable:", err)
		return true, 2
	}
	defer pool.Close()
	return true, runCLIIdempotent(ctx, args, stdout, stderr, idempotency.New(pool, idempotency.Config{}), executeCLIChild)
}

func runCLIIdempotent(ctx context.Context, args []string, stdout, stderr io.Writer, registry cliOperationRegistry, execute cliCommandExecutor) int {
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok || registry == nil || execute == nil {
		_, _ = fmt.Fprintln(stderr, "operation context unavailable")
		return 2
	}
	decision, err := registry.Begin(ctx, idempotency.BeginRequest{Operation: operation.Key, Fingerprint: operation.Fingerprint})
	if err != nil && !errors.Is(err, idempotency.ErrConflict) {
		_, _ = fmt.Fprintln(stderr, "operation registry unavailable")
		return 2
	}
	switch decision.Decision {
	case idempotency.DecisionAcquired:
	case idempotency.DecisionReplay:
		return replayCLIResult(decision.Replay, stdout, stderr)
	case idempotency.DecisionResultExpired:
		_, _ = fmt.Fprintln(stderr, "operation completed but its replay result expired")
		return 2
	case idempotency.DecisionConflict:
		_, _ = fmt.Fprintln(stderr, "operation key conflicts with changed command arguments")
		return 2
	case idempotency.DecisionInProgress:
		_, _ = fmt.Fprintln(stderr, "operation is still in progress")
		return 2
	case idempotency.DecisionIndeterminate:
		_, _ = fmt.Fprintln(stderr, "operation outcome is indeterminate; do not retry automatically")
		return 2
	default:
		_, _ = fmt.Fprintln(stderr, "operation registry unavailable")
		return 2
	}

	outCapture := &boundedCLIOutput{dst: stdout, limit: maxCLIReplayStreamBytes}
	errCapture := &boundedCLIOutput{dst: stderr, limit: maxCLIReplayStreamBytes}
	exitCode := execute(ctx, appendOperationKey(args, operation.Key.Key), outCapture, errCapture)
	if exitCode != 0 || outCapture.overflow || errCapture.overflow {
		if markErr := registry.MarkIndeterminate(ctx, operation.Key, operation.Fingerprint); markErr != nil {
			_, _ = fmt.Fprintln(stderr, "mark operation indeterminate:", markErr)
		}
		if outCapture.overflow || errCapture.overflow {
			_, _ = fmt.Fprintln(stderr, "operation output exceeded durable replay limit")
			if exitCode == 0 {
				exitCode = 2
			}
		}
		return exitCode
	}
	body, err := json.Marshal(cliReplayEnvelope{ExitCode: exitCode, Stdout: outCapture.buf.String(), Stderr: errCapture.buf.String()})
	if err != nil {
		_ = registry.MarkIndeterminate(ctx, operation.Key, operation.Fingerprint)
		return 2
	}
	completeErr := registry.Complete(ctx, idempotency.CompleteRequest{
		Operation: operation.Key, Fingerprint: operation.Fingerprint,
		Result: idempotency.ReplayResult{Body: body, StatusCode: 200, ExpiresAt: time.Now().UTC().Add(cliReplayRetention)},
	})
	if completeErr != nil {
		_ = registry.MarkIndeterminate(ctx, operation.Key, operation.Fingerprint)
		_, _ = fmt.Fprintln(stderr, "operation completion unavailable")
		return 2
	}
	return 0
}

func executeCLIChild(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	executable, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "resolve executable:", err)
		return 2
	}
	command := exec.CommandContext(ctx, executable, args...) //nolint:gosec // executable is the current Aura binary; args are the already-parsed CLI invocation
	command.Env = append(os.Environ(), cliIdempotencyChildEnv+"=1")
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		_, _ = fmt.Fprintln(stderr, "execute command:", err)
		return 2
	}
	return 0
}

func appendOperationKey(args []string, key string) []string {
	childArgs := append([]string(nil), args...)
	return append(childArgs, "--operation-key", key)
}

func replayCLIResult(result *idempotency.ReplayResult, stdout, stderr io.Writer) int {
	if result == nil || len(result.Body) == 0 {
		_, _ = fmt.Fprintln(stderr, "operation replay unavailable")
		return 2
	}
	var replay cliReplayEnvelope
	if err := json.Unmarshal(result.Body, &replay); err != nil || replay.ExitCode != 0 {
		_, _ = fmt.Fprintln(stderr, "operation replay unavailable")
		return 2
	}
	_, _ = io.WriteString(stdout, replay.Stdout)
	_, _ = io.WriteString(stderr, replay.Stderr)
	return replay.ExitCode
}

type boundedCLIOutput struct {
	dst      io.Writer
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (w *boundedCLIOutput) Write(p []byte) (int, error) {
	written, err := w.dst.Write(p)
	remaining := w.limit - w.buf.Len()
	if remaining > 0 {
		copyLen := len(p)
		if copyLen > remaining {
			copyLen = remaining
		}
		_, _ = w.buf.Write(p[:copyLen])
	}
	if len(p) > remaining {
		w.overflow = true
	}
	return written, err
}

func cliMutationPath(args []string) (string, bool) {
	if len(args) < 2 {
		return "", false
	}
	command := args[0] + " " + args[1]
	_, ok := cliMutationCommands[command]
	return command, ok
}

func removeOperationKeyFlag(args []string) ([]string, string, error) {
	cleaned := make([]string, 0, len(args))
	var key string
	for i := 0; i < len(args); i++ {
		if args[i] != "--operation-key" {
			cleaned = append(cleaned, args[i])
			continue
		}
		if key != "" || i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return nil, "", errors.New("--operation-key requires exactly one value")
		}
		key = args[i+1]
		i++
	}
	return cleaned, key, nil
}
