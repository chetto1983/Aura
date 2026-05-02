// Command seed_e2e_env mints a fresh bearer token for an existing
// allowlisted user and patches the AURA_E2E_TOKEN + AURA_E2E_CHAT_ID
// keys in .env so `npm run e2e` (Playwright) has live credentials.
//
// It does NOT alter any other key in .env. It does NOT issue tokens for
// new users. It refuses to run if no allowed user exists yet (run the
// bot once via Telegram /start to bootstrap).
//
// Usage:
//
//	go run ./cmd/seed_e2e_env [-db ./aura.db] [-env .env] [-user <id>]
//
// Without -user it picks the first row of allowed_users (typically the
// owner). Without -db / -env it uses the project defaults.
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

	"github.com/aura/aura/internal/auth"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/scheduler"
)

func main() {
	dbPath := flag.String("db", "./aura.db", "path to the live SQLite database")
	envPath := flag.String("env", ".env", "path to the .env file to patch")
	userID := flag.String("user", "", "user id to issue the token for (default: first allowed_users row)")
	seedTurns := flag.Bool("seed-turns", false, "if true and the conversations table is empty, inject 3 synthetic turns so the Playwright drawer test has data")
	flag.Parse()

	if _, err := os.Stat(*dbPath); err != nil {
		log.Fatalf("database not found at %s: %v", *dbPath, err)
	}

	// Open via scheduler.OpenStore so all migrations run; auth shares the DB.
	schedStore, err := scheduler.OpenStore(*dbPath)
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

	if err := patchEnv(*envPath, map[string]string{
		"AURA_E2E_TOKEN":   token,
		"AURA_E2E_CHAT_ID": chatIDStr,
	}); err != nil {
		log.Fatalf("patch env: %v", err)
	}

	fmt.Printf("Wrote AURA_E2E_TOKEN (%d chars) and AURA_E2E_CHAT_ID=%q to %s\n",
		len(token), chatIDStr, *envPath)
	fmt.Printf("user_id: %s\n", resolvedUserID)
	if !chatID.Valid {
		fmt.Println("note: no archived conversations yet — drawer-click test will skip until a turn is seeded")
	}
}

// patchEnv updates only the listed keys in path. Lines outside the keys
// are preserved byte-for-byte. Missing keys are appended at the end.
func patchEnv(path string, kv map[string]string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	seen := make(map[string]bool, len(kv))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		if val, ok := kv[key]; ok {
			lines[i] = key + "=" + val
			seen[key] = true
		}
	}
	for key, val := range kv {
		if !seen[key] {
			lines = append(lines, key+"="+val)
		}
	}
	out := strings.Join(lines, "\n")
	return os.WriteFile(path, []byte(out), 0o600)
}
