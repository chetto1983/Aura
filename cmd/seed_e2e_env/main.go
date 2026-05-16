// Command seed_e2e_env mints a fresh bearer token for an existing
// allowlisted user and prints AURA_E2E_TOKEN + AURA_E2E_CHAT_ID to stdout
// in shell-eval format. Pipe through `eval` (bash) or copy-paste into
// PowerShell to populate the env vars Playwright reads.
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
//	go run ./cmd/seed_e2e_env [-db ./aura.db] -bootstrap-user <id>
//
// Without -user it picks the first row of allowed_users (typically the
// owner). Without -db it uses the project default ./aura.db.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/db"
)

func main() {
	dbPath := flag.String("db", "./aura.db", "path to the live SQLite database")
	userID := flag.String("user", "", "user id to issue the token for (default: first allowed_users row)")
	bootstrapUserID := flag.String("bootstrap-user", "", "debug/E2E only: insert this user as first allowed user when allowlist is empty")
	seedTurns := flag.Bool("seed-turns", false, "if true and the conversations table is empty, inject 3 synthetic turns so the Playwright drawer test has data")
	flag.Parse()

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
	if resolvedUserID == "" {
		row := db.QueryRowContext(ctx,
			`SELECT user_id FROM allowed_users ORDER BY created_at ASC LIMIT 1`)
		if err := row.Scan(&resolvedUserID); err != nil {
			if err == sql.ErrNoRows {
				log.Fatalf("allowed_users is empty — bootstrap a user first by running the bot and sending /start from Telegram")
			}
			log.Fatalf("lookup first allowed user: %v", err)
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
		fmt.Printf("Seeded %d synthetic turns under chat_id=%d\n", len(fixtures), uid)
	}

	chatIDStr := ""
	if chatID.Valid {
		chatIDStr = strconv.FormatInt(chatID.Int64, 10)
	}

	// Print shell-eval lines on stdout (consumable via `eval $(...)` in
	// bash/zsh). Diagnostics go to stderr so they don't poison the eval.
	fmt.Printf("export AURA_E2E_TOKEN=%s\n", token)
	fmt.Printf("export AURA_E2E_CHAT_ID=%s\n", chatIDStr)

	fmt.Fprintf(os.Stderr, "Minted AURA_E2E_TOKEN (%d chars) and AURA_E2E_CHAT_ID=%q\n",
		len(token), chatIDStr)
	fmt.Fprintf(os.Stderr, "user_id: %s\n", resolvedUserID)
	fmt.Fprintln(os.Stderr, "PowerShell: $env:AURA_E2E_TOKEN = '"+token+"'; $env:AURA_E2E_CHAT_ID = '"+chatIDStr+"'")
	if !chatID.Valid {
		fmt.Fprintln(os.Stderr, "note: no archived conversations yet — drawer-click test will skip until a turn is seeded")
	}
}

func guardLiveDBWriteForSeed(ctx context.Context, dbPath string, auraRunning db.AuraRunningFunc) error {
	return db.RefuseLiveDockerDBWrite(ctx, dbPath, "seed_e2e_env", auraRunning)
}

