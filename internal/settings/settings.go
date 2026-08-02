// Package settings is the cockpit-editable runtime override layer for Aura's
// model-backend knobs — the "Settings" page where the operator swaps any backend
// local↔cloud (embed/STT/TTS/vision), sets the single OpenRouter key, and picks
// the embed dimension. Rows live in aura.settings (migration 0024). At
// daemon boot OverlayEnv applies them onto the process environment BEFORE
// config.Load, so the existing env readers pick them up with NO per-field mapping;
// DB values WIN over pre-set env (the operator's UI choice is authoritative).
// Changes take effect on the next restart (the clients are built at boot).
//
// The overlay applies ONLY an allowlist of model-backend keys, so a settings row
// can never clobber connection/security env (POSTGRES_*, ARCADEDB_PASSWORD,
// AURA_WEB_AUTH_SECRET).
package settings

import (
	"context"
	"os"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kind tags how the settings page should render/validate a value.
type Kind string

const (
	// KindString marks a free-form string setting.
	KindString Kind = "string"
	// KindBool marks a boolean setting.
	KindBool Kind = "bool"
	// KindInt marks an integer setting.
	KindInt Kind = "int"
)

// KeyMeta describes an allowlisted setting key for the API + settings page.
type KeyMeta struct {
	Secret bool   // value is redacted in API GET responses (e.g. the OpenRouter key)
	Kind   Kind   // string|bool|int — drives the UI control + PUT validation
	Label  string // human label for the settings page
}

// AllowedKeys is the whitelist of env knobs the Settings page may override.
// OverlayEnv + the API enforce it: a key outside this map is rejected/ignored, so
// the settings layer can never reach connection or security env.
var AllowedKeys = map[string]KeyMeta{
	"AURA_LLM_PROVIDER":            {Kind: KindString, Label: "Primary LLM provider (openrouter|llamacpp)"},
	"AURA_LLM_MODEL":               {Kind: KindString, Label: "Primary LLM model"},
	"AURA_LLM_BASE_URL":            {Kind: KindString, Label: "Primary LLM base URL"},
	"AURA_LLM_MAX_TOKENS":          {Kind: KindInt, Label: "Max response tokens"},
	"AURA_MODEL_CONTEXT_WINDOW":    {Kind: KindInt, Label: "Context window tokens"},
	"AURA_MODEL_MAX_OUTPUT_TOKENS": {Kind: KindInt, Label: "Reserved output tokens"},
	"OPENROUTER_API_KEY":           {Secret: true, Kind: KindString, Label: "OpenRouter API key"},
	"AURA_EMBED_MODEL":             {Kind: KindString, Label: "Embedding cloud model"},
	"AURA_EMBED_DIMENSIONS":        {Kind: KindInt, Label: "Embedding dimensions"},
	"AURA_EMBED_BASE_URL":          {Kind: KindString, Label: "Embedding base URL"},
	"AURA_TTS_MODEL":               {Kind: KindString, Label: "TTS cloud model"},
	"AURA_STT_CLOUD_MODEL":         {Kind: KindString, Label: "STT cloud model"},
	"AURA_VISION_CLOUD":            {Kind: KindBool, Label: "Vision uses cloud"},
	"TELEGRAM_BOT_TOKEN":           {Secret: true, Kind: KindString, Label: "Telegram bot token"},
}

// Allowed reports whether key may be set through the Settings layer.
func Allowed(key string) (KeyMeta, bool) {
	m, ok := AllowedKeys[key]
	return m, ok
}

// Lister is the read seam OverlayEnv needs (Store satisfies it); lets the overlay
// be unit-tested without a live Postgres.
type Lister interface {
	List(ctx context.Context) ([]sqlc.AuraSettings, error)
}

// Store is the aura.settings CRUD over a pgx pool.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// NewStore builds a settings store over the pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// List returns all settings rows ordered by key.
func (s *Store) List(ctx context.Context) ([]sqlc.AuraSettings, error) {
	return s.q.ListSettings(ctx)
}

// Upsert writes (or replaces) an allowlisted key. The is_secret flag is taken from
// the allowlist, not the caller, so a value is consistently redacted by the API.
func (s *Store) Upsert(ctx context.Context, key, value, by string) (sqlc.AuraSettings, error) {
	meta := AllowedKeys[key]
	var updatedBy pgtype.Text
	if by != "" {
		updatedBy = pgtype.Text{String: by, Valid: true}
	}
	var row sqlc.AuraSettings
	err := s.withWriteLock(ctx, func(q *sqlc.Queries) error {
		var err error
		row, err = q.UpsertSetting(ctx, sqlc.UpsertSettingParams{
			Key: key, Value: value, IsSecret: meta.Secret, UpdatedBy: updatedBy,
		})
		return err
	})
	return row, err
}

// Delete removes a key, reverting it to its environment/.env default on restart.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.withWriteLock(ctx, func(q *sqlc.Queries) error {
		return q.DeleteSetting(ctx, key)
	})
}

func (s *Store) withWriteLock(
	ctx context.Context, fn func(*sqlc.Queries) error,
) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if panicValue := recover(); panicValue != nil {
			_ = tx.Rollback(ctx)
			panic(panicValue)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()
	if _, err = tx.Exec(
		ctx, "SELECT pg_advisory_xact_lock($1)", benchmarkSettingsAdvisoryKey,
	); err != nil {
		return err
	}
	return fn(sqlc.New(tx))
}

// OverlayEnv applies the allowlisted aura.settings rows onto the process
// environment so a subsequent config.Load reads them. Non-allowlisted rows are
// ignored. Call at daemon boot BEFORE config.Load, after the pool is open. A row
// whose key is allowlisted but whose value fails os.Setenv (an invalid name can't
// occur for the static allowlist) is skipped without aborting the overlay.
func OverlayEnv(ctx context.Context, l Lister) error {
	rows, err := l.List(ctx)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if _, ok := AllowedKeys[r.Key]; !ok {
			continue
		}
		_ = os.Setenv(r.Key, r.Value)
	}
	return nil
}
