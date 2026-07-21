package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

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
