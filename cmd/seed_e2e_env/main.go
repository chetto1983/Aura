// Command seed_e2e_env mints a fresh bearer token for an existing
// allowlisted user and prints AURA_E2E_TOKEN + AURA_E2E_CHAT_ID to stdout
// in shell-eval format. Pipe through `eval` (bash) or `Invoke-Expression`
// (PowerShell) to populate the env vars Playwright reads.
//
// It does NOT touch any file. By default it refuses to run if no allowed
// user exists yet (run the bot once via Telegram /start to bootstrap).
// The optional -bootstrap-user path is for local smoke tests only and
// marks that user as e2e_bootstrap so it cannot block the first real
// Telegram owner from claiming the install.
//
// Usage:
//
//	eval $(go run ./cmd/seed_e2e_env [-db ./aura.db] [-user <id>])
//	Invoke-Expression (& go run ./cmd/seed_e2e_env [-db ./aura.db] [-user <id>] -shell powershell)
//	go run ./cmd/seed_e2e_env [-db ./aura.db] -bootstrap-user <id>
//
// Without -user it picks the first non-e2e_bootstrap row of allowed_users
// (typically the owner). Without -db it uses the project default ./aura.db.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/identity"
)

func main() {
	dbPath := flag.String("db", "./aura.db", "path to the live SQLite database")
	userID := flag.String("user", "", "user id to issue the token for (default: first non-e2e_bootstrap allowed_users row)")
	bootstrapUserID := flag.String("bootstrap-user", "", "debug/E2E only: insert this user as first allowed user when allowlist is empty")
	seedTurns := flag.Bool("seed-turns", false, "if true and the conversations table is empty, inject 3 synthetic turns so the Playwright drawer test has data")
	shell := flag.String("shell", "sh", "stdout shell syntax: sh or powershell")
	flag.Parse()
	if _, err := envShellKind(*shell); err != nil {
		log.Fatalf("output env: %v", err)
	}

	if _, err := os.Stat(*dbPath); err != nil {
		log.Fatalf("database not found at %s: %v", *dbPath, err)
	}
	if err := guardLiveDBWriteForSeed(context.Background(), *dbPath, db.IsComposeAuraRunning); err != nil {
		log.Fatalf("%v", err)
	}

	// Open via cron.OpenStore so all migrations run; auth shares the DB.
	schedStore, err := cron.OpenStore(*dbPath)
	if err != nil {
		log.Fatalf("open scheduler store: %v", err)
	}
	defer schedStore.Close()

	db := schedStore.DB()
	authStore, err := auth.NewStoreWithDB(db)
	if err != nil {
		log.Fatalf("open auth store: %v", err)
	}

	ctx := context.Background()

	if strings.TrimSpace(*bootstrapUserID) != "" {
		created, err := authStore.BootstrapE2EUser(ctx, strings.TrimSpace(*bootstrapUserID))
		if err != nil {
			log.Fatalf("bootstrap user: %v", err)
		}
		if created {
			log.Printf("bootstrapped allowed user %s", strings.TrimSpace(*bootstrapUserID))
		}
		if *userID == "" {
			*userID = strings.TrimSpace(*bootstrapUserID)
		}
	}

	// Resolve the target user.
	resolvedUserID := *userID
	resolvedSource := ""
	if resolvedUserID == "" {
		var err error
		resolvedUserID, resolvedSource, err = resolveDefaultSeedUser(ctx, db)
		if err != nil {
			if err == sql.ErrNoRows {
				log.Fatalf("no real allowed_users row found - bootstrap a Telegram owner first with /start or pass -user for an explicitly allowlisted real user")
			}
			log.Fatalf("lookup default allowed user: %v", err)
		}
	} else {
		var err error
		resolvedSource, err = lookupAllowedUserSource(ctx, db, resolvedUserID)
		if err != nil && err != sql.ErrNoRows {
			log.Fatalf("lookup allowed user source: %v", err)
		}
	}

	if resolvedSource == auth.SourceE2EBootstrap {
		log.Printf("warn: selected user %s has source=%s; token may not pass api.chat capability checks", resolvedUserID, resolvedSource)
	} else if resolvedSource != "" {
		if _, err := authStore.EnsureTelegramAllowlistedIdentity(ctx, resolvedUserID, resolvedSource); err != nil {
			log.Fatalf("ensure dashboard identity for %s: %v", resolvedUserID, err)
		}
		decision, err := authStore.Authorize(ctx, identity.AuthorizeParams{
			ActorID:    identity.TelegramSessionActorID(resolvedUserID),
			Capability: identity.CapabilityAPIChat,
			Resource:   identity.ResourceRef{Type: "api", ID: "chat"},
		})
		if err != nil {
			log.Fatalf("verify api.chat grant for %s: %v", resolvedUserID, err)
		}
		if decision.Decision != identity.DecisionAllow {
			log.Fatalf("selected user %s cannot use /api/chat: %s (%s)", resolvedUserID, decision.Decision, decision.Reason)
		}
	}

	// Mint a fresh token. The plaintext is the only copy we'll ever see
	// (auth stores SHA-256 hash). Older e2e tokens for this user remain
	// valid until manually revoked — Issue does not auto-revoke.
	token, err := authStore.Issue(ctx, resolvedUserID)
	if err != nil {
		log.Fatalf("issue token: %v", err)
	}

	// Pick a chat_id with archived turns so the Playwright drawer test
	// has something to click on. Any conversation row works; prefer the
	// most recent. If empty and -seed-turns is set, inject synthetic
	// turns under the resolved user id (for personal chats chat_id =
	// user_id).
	var chatID sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT chat_id FROM conversations ORDER BY created_at DESC LIMIT 1`).
		Scan(&chatID); err != nil && err != sql.ErrNoRows {
		log.Printf("warn: chat_id lookup failed: %v", err)
	}

	if !chatID.Valid && *seedTurns {
		uid, err := strconv.ParseInt(resolvedUserID, 10, 64)
		if err != nil {
			log.Fatalf("seed-turns: user id %q is not numeric: %v", resolvedUserID, err)
		}
		archive, err := conversation.NewArchiveStore(db)
		if err != nil {
			log.Fatalf("seed-turns: NewArchiveStore: %v", err)
		}
		fixtures := []conversation.Turn{
			{ChatID: uid, UserID: uid, TurnIndex: 0, Role: "user", Content: "Hello Aura — this is an E2E seed turn."},
			{ChatID: uid, UserID: uid, TurnIndex: 1, Role: "assistant", Content: "Hi! Your dashboard archive is now wired up. Open /conversations to see this turn."},
			{ChatID: uid, UserID: uid, TurnIndex: 2, Role: "user", Content: "Confirm — drawer test should now have a row to click."},
		}
		for _, t := range fixtures {
			if err := archive.Append(ctx, t); err != nil {
				log.Fatalf("seed-turns: append: %v", err)
			}
		}
		chatID = sql.NullInt64{Int64: uid, Valid: true}
		fmt.Fprintf(os.Stderr, "Seeded %d synthetic turns under chat_id=%d\n", len(fixtures), uid)
	}

	chatIDStr := ""
	if chatID.Valid {
		chatIDStr = strconv.FormatInt(chatID.Int64, 10)
	}

	// Print only shell-eval lines on stdout. Diagnostics go to stderr so
	// they don't poison eval/Invoke-Expression.
	if err := printEnv(os.Stdout, *shell, token, chatIDStr); err != nil {
		log.Fatalf("output env: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Minted AURA_E2E_TOKEN (%d chars) and AURA_E2E_CHAT_ID=%q\n",
		len(token), chatIDStr)
	fmt.Fprintf(os.Stderr, "user_id: %s\n", resolvedUserID)
	fmt.Fprintf(os.Stderr, "shell: %s\n", strings.TrimSpace(*shell))
	if !chatID.Valid {
		fmt.Fprintln(os.Stderr, "note: no archived conversations yet — drawer-click test will skip until a turn is seeded")
	}
}

func guardLiveDBWriteForSeed(ctx context.Context, dbPath string, auraRunning db.AuraRunningFunc) error {
	return db.RefuseLiveDockerDBWrite(ctx, dbPath, "seed_e2e_env", auraRunning)
}

func resolveDefaultSeedUser(ctx context.Context, db *sql.DB) (userID string, source string, err error) {
	if db == nil {
		return "", "", fmt.Errorf("database is required")
	}
	err = db.QueryRowContext(ctx, `
SELECT user_id, source
FROM allowed_users
WHERE source <> ?
ORDER BY created_at ASC
LIMIT 1
`, auth.SourceE2EBootstrap).Scan(&userID, &source)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(userID), strings.TrimSpace(source), nil
}

func lookupAllowedUserSource(ctx context.Context, db *sql.DB, userID string) (string, error) {
	if db == nil {
		return "", fmt.Errorf("database is required")
	}
	var source string
	err := db.QueryRowContext(ctx, `
SELECT source
FROM allowed_users
WHERE user_id = ?
`, strings.TrimSpace(userID)).Scan(&source)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(source), nil
}

func printEnv(w io.Writer, shell, token, chatID string) error {
	if w == nil {
		return fmt.Errorf("writer is required")
	}
	kind, err := envShellKind(shell)
	if err != nil {
		return err
	}
	switch kind {
	case "sh":
		fmt.Fprintf(w, "export AURA_E2E_TOKEN=%s\n", token)
		fmt.Fprintf(w, "export AURA_E2E_CHAT_ID=%s\n", chatID)
	case "powershell":
		fmt.Fprintf(w, "$env:AURA_E2E_TOKEN = '%s'\n", powerShellSingleQuote(token))
		fmt.Fprintf(w, "$env:AURA_E2E_CHAT_ID = '%s'\n", powerShellSingleQuote(chatID))
	}
	return nil
}

func envShellKind(shell string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "", "sh", "bash", "zsh":
		return "sh", nil
	case "powershell", "pwsh", "ps":
		return "powershell", nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want sh or powershell)", shell)
	}
}

func powerShellSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
