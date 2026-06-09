//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) < 3 {
		fatalf("usage: go run ./scripts/agui_db_seed.go {seed|delete} <conversation-id> [identity-id prompt]")
	}
	dsn := os.Getenv("AURA_DB_MIGRATE_URL")
	if dsn == "" {
		dsn = os.Getenv("AURA_DB_URL")
	}
	if dsn == "" {
		fatalf("AURA_DB_MIGRATE_URL/AURA_DB_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fatalf("open db: %v", err)
	}
	defer pool.Close()

	switch os.Args[1] {
	case "seed":
		if len(os.Args) != 5 {
			fatalf("usage: seed <conversation-id> <identity-id> <prompt>")
		}
		if err := seed(ctx, pool, os.Args[2], os.Args[3], os.Args[4]); err != nil {
			fatalf("seed: %v", err)
		}
	case "delete":
		if err := deleteConversation(ctx, pool, os.Args[2]); err != nil {
			fatalf("delete: %v", err)
		}
	default:
		fatalf("unknown action %q", os.Args[1])
	}
}

func seed(ctx context.Context, pool *pgxpool.Pool, conversationID, identityID, prompt string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx,
		`INSERT INTO aura.conversations (id, identity_id, model, status)
		 VALUES ($1::uuid, $2::uuid, 'agui-smoke', 'active')`,
		conversationID, identityID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO aura.conversation_turns (conversation_id, seq, role, content)
		 VALUES ($1::uuid, 1, 'system', 'you are aura'), ($1::uuid, 2, 'user', $2)`,
		conversationID, prompt,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func deleteConversation(ctx context.Context, pool *pgxpool.Pool, conversationID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM aura.conversation_turns WHERE conversation_id = $1::uuid`, conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM aura.conversations WHERE id = $1::uuid`, conversationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "agui db seed: "+format+"\n", args...)
	os.Exit(1)
}
