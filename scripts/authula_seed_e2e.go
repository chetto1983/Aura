//go:build ignore

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	authulamodels "github.com/Authula/authula/models"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/webauth"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbURL := requiredEnv("AURA_DB_URL")
	email := firstEnv("AURA_E2E_AUTHULA_EMAIL", "AURA_AUTHULA_OPERATOR_EMAIL")
	password := firstEnv("AURA_E2E_AUTHULA_PASSWORD", "AURA_AUTHULA_OPERATOR_PASSWORD")
	if email == "" || password == "" {
		fatalf("set AURA_E2E_AUTHULA_EMAIL and AURA_E2E_AUTHULA_PASSWORD")
	}

	pool, err := db.Open(ctx, &db.Config{URL: dbURL})
	if err != nil {
		fatalf("%v", err)
	}
	defer pool.Close()

	dsn := strings.TrimSpace(os.Getenv("AURA_AUTHULA_DATABASE_URL"))
	if dsn == "" {
		dsn = dbURL
	}
	provider, err := webauth.New(webauth.Config{
		DSN:            dsn,
		Secret:         requiredEnv("AURA_AUTHULA_SECRET"),
		TrustedOrigins: []string{"http://127.0.0.1:9080", "https://127.0.0.1:9080"},
	})
	if err != nil {
		fatalf("open Authula provider: %v", err)
	}
	defer func() {
		if err := provider.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close Authula provider: %v\n", err)
		}
	}()

	core := provider.CoreServices()
	user, err := core.UserService.GetByEmail(ctx, email)
	if err != nil {
		fatalf("lookup Authula user: %v", err)
	}
	if user == nil {
		user, err = core.UserService.Create(ctx, email, email, true, nil, nil)
		if err != nil {
			fatalf("create Authula user: %v", err)
		}
	}

	hash, err := core.PasswordService.Hash(password)
	if err != nil {
		fatalf("hash E2E password: %v", err)
	}
	account, err := core.AccountService.GetByUserIDAndProvider(ctx, user.ID, authulamodels.AuthProviderEmail.String())
	if err != nil {
		fatalf("lookup Authula account: %v", err)
	}
	if account == nil {
		if _, err := core.AccountService.Create(ctx, user.ID, email, authulamodels.AuthProviderEmail.String(), &hash); err != nil {
			fatalf("create Authula account: %v", err)
		}
	} else {
		account.AccountID = email
		account.Password = &hash
		if _, err := core.AccountService.Update(ctx, account); err != nil {
			fatalf("update Authula account: %v", err)
		}
	}

	identityID, err := operatorIdentity(ctx, pool, email)
	if err != nil {
		fatalf("%v", err)
	}
	if err := webauth.NewIdentityLinker(pool).LinkOperator(ctx, identityID, user.ID); err != nil {
		fatalf("link Authula user to its identity: %v", err)
	}
	fmt.Printf("seeded Authula E2E operator %s -> %s\n", email, identityID)
}

// operatorIdentity returns the `user` identity named after the operator's email, creating it
// with the bootstrap operator's capabilities when it is missing. It is what the first-run
// bootstrap gives a real operator; the seeded `local` row is a `system` identity, which Aura
// mints no first-party MCP grants for, so an operator linked to it never mounts calendar,
// memory or whatsapp.
func operatorIdentity(ctx context.Context, pool *pgxpool.Pool, email string) (string, error) {
	existing, err := identity.New(pool).GetIdentityByName(ctx, email)
	switch {
	case err == nil && existing.Kind == "user":
		return existing.ID, nil
	case err == nil:
		return "", fmt.Errorf("identity %q exists with kind %q, not user", email, existing.Kind)
	case !errors.Is(err, identity.ErrIdentityNotFound):
		return "", fmt.Errorf("resolve operator identity: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id, err := identity.CreateUserTx(ctx, tx, email, identity.All())
	if err != nil {
		return "", fmt.Errorf("create operator identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit operator identity: %w", err)
	}
	return id.String(), nil
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func requiredEnv(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		fatalf("set %s", name)
	}
	return v
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "authula_seed_e2e: "+format+"\n", args...)
	os.Exit(1)
}
