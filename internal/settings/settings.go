// Package settings is the cockpit-editable runtime override layer for Aura's
// model-backend knobs — the "Settings" page where the operator swaps any backend
// local↔cloud (embed/STT/TTS/vision), sets the single OpenRouter key, and picks
// the embed dimension. Rows live in aura.settings (migration 0024); secret rows are
// AES-GCM ciphertext (secrets.go), which the Store decrypts for its callers. At
// daemon boot OverlayEnv applies them onto the process environment BEFORE
// config.Load, so the existing env readers pick them up with NO per-field mapping;
// DB values WIN over pre-set env (the operator's UI choice is authoritative).
// The primary LLM profile is also published to the live runtime by the Settings API;
// the remaining backend knobs still take effect on restart.
//
// The overlay applies ONLY an allowlist of model-backend keys, so a settings row
// can never clobber connection/security env (POSTGRES_*, ARCADEDB_PASSWORD,
// AURA_WEB_AUTH_SECRET).
package settings

import (
	"context"
	"crypto/cipher"
	"log/slog"
	"os"
	"sort"
	"strings"

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
	"AURA_LLM_PROVIDER":            {Kind: KindString, Label: "Primary LLM provider (openrouter|llamacpp|ollama)"},
	"AURA_LLM_MODEL":               {Kind: KindString, Label: "Primary LLM model"},
	"AURA_LLM_BASE_URL":            {Kind: KindString, Label: "Primary LLM base URL"},
	"AURA_LLM_MAX_TOKENS":          {Kind: KindInt, Label: "Max response tokens"},
	"AURA_MODEL_CONTEXT_WINDOW":    {Kind: KindInt, Label: "Context window tokens"},
	"AURA_MODEL_MAX_OUTPUT_TOKENS": {Kind: KindInt, Label: "Reserved output tokens"},
	// The share of the window a conversation may spend replaying itself before L2.4
	// condenses the older rounds. Editable here because the right value depends on what
	// the operator is doing -- long analytical threads want it high, cheap chat low --
	// and because it is a percentage, which is the one form of this number a person can
	// judge without knowing the tokenizer.
	"AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT": {Kind: KindInt, Label: "Compact history at % of window"},
	// The agent-loop budget (amendment #188): hot like the model profile, so the
	// operator who needs more than the shipped 25 steps for one real delegation no
	// longer recreates the container to get them.
	"AURA_LOOP_MAX_STEPS":         {Kind: KindInt, Label: "Max agent steps per turn"},
	"AURA_LOOP_MAX_WALLCLOCK_SEC": {Kind: KindInt, Label: "Max wallclock per turn (seconds)"},
	// The proactive per-message memory preload. Hot because it is the one knob that
	// decides whether a turn ARRIVES with what the memory knows or has to go and ask:
	// off, the prompt carries only the pointer ("you have N facts across M entities")
	// and an agent that does not open memory answers from nothing. Its two bounds ride
	// with it -- a preload the operator cannot size or time out is one they will turn
	// off rather than tune.
	"AURA_MEMORY_PRELOAD_ENABLED":    {Kind: KindBool, Label: "Preload memory into every turn"},
	"AURA_MEMORY_PRELOAD_TOP_K":      {Kind: KindInt, Label: "Memory preload results"},
	"AURA_MEMORY_PRELOAD_TIMEOUT_MS": {Kind: KindInt, Label: "Memory preload timeout (ms)"},
	"OPENROUTER_API_KEY":             {Secret: true, Kind: KindString, Label: "OpenRouter API key"},
	// The management credential (C-02/D-12, plan 02-06): mints/revokes per-identity
	// keys and reads analytics/credits, but cannot call completion endpoints (M-01) —
	// a different credential from the inference key above, stored the same way.
	"AURA_OPENROUTER_MANAGEMENT_KEY": {Secret: true, Kind: KindString, Label: "OpenRouter management key (mint/revoke, not inference)"},
	"AURA_EMBED_MODEL":               {Kind: KindString, Label: "Embedding cloud model"},
	"AURA_EMBED_DIMENSIONS":          {Kind: KindInt, Label: "Embedding dimensions"},
	"AURA_EMBED_BASE_URL":            {Kind: KindString, Label: "Embedding base URL"},
	"AURA_TTS_MODEL":                 {Kind: KindString, Label: "TTS cloud model"},
	"AURA_STT_CLOUD_MODEL":           {Kind: KindString, Label: "STT cloud model"},
	"TELEGRAM_BOT_TOKEN":             {Secret: true, Kind: KindString, Label: "Telegram bot token"},
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

// Store is the aura.settings CRUD over a pgx pool. Secret rows are encrypted on the way in
// and decrypted on the way out, so every caller sees plaintext and the table never holds it.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	aead cipher.AEAD // nil when built without AURA_AUTHULA_SECRET: secret rows unavailable
}

// NewStore builds a settings store over the pool. authulaSecretHex keys the secret rows (see
// secrets.go): empty leaves them unavailable, malformed fails.
func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error) {
	aead, err := newSecretAEAD(authulaSecretHex)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool, q: sqlc.New(pool), aead: aead}, nil
}

// List returns all settings rows ordered by key, secret values decrypted. A secret this store
// cannot open reads as empty and is logged, rather than failing the whole list: the Settings
// page and the overlay still work, and Secret reports the error to the reader who needs it.
func (s *Store) List(ctx context.Context) ([]sqlc.AuraSettings, error) {
	rows, err := s.q.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if !AllowedKeys[rows[i].Key].Secret {
			continue
		}
		plain, err := openSecret(s.aead, rows[i].Value)
		if err != nil {
			slog.Warn("settings: secret row unreadable", "key", rows[i].Key, "err", err)
			plain = ""
		}
		rows[i].Value = plain
	}
	return rows, nil
}

// Secret returns the decrypted value of one secret row, "" when the row is absent.
func (s *Store) Secret(ctx context.Context, key string) (string, error) {
	rows, err := s.q.ListSettings(ctx)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.Key == key {
			return openSecret(s.aead, row.Value)
		}
	}
	return "", nil
}

// storedValue encrypts a secret key's value; every other key is stored as given.
func (s *Store) storedValue(key, value string) (string, error) {
	if !AllowedKeys[key].Secret {
		return value, nil
	}
	return sealSecret(s.aead, value)
}

// EncryptPlaintextSecrets rewrites every secret row still stored in the clear, so an install
// upgraded from before encryption converges at boot. It returns how many rows it rewrote.
func (s *Store) EncryptPlaintextSecrets(ctx context.Context) (int, error) {
	if s.aead == nil {
		return 0, nil
	}
	rewritten := 0
	err := s.withWriteLock(ctx, func(q *sqlc.Queries) error {
		rows, err := q.ListSettings(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if !AllowedKeys[row.Key].Secret || row.Value == "" || strings.HasPrefix(row.Value, secretPrefix) {
				continue
			}
			sealed, err := sealSecret(s.aead, row.Value)
			if err != nil {
				return err
			}
			if _, err := q.UpsertSetting(ctx, sqlc.UpsertSettingParams{
				Key: row.Key, Value: sealed, IsSecret: true, UpdatedBy: row.UpdatedBy,
			}); err != nil {
				return err
			}
			rewritten++
		}
		return nil
	})
	return rewritten, err
}

// Upsert writes (or replaces) an allowlisted key. The is_secret flag is taken from
// the allowlist, not the caller, so a value is consistently redacted by the API.
func (s *Store) Upsert(ctx context.Context, key, value, by string) (sqlc.AuraSettings, error) {
	meta := AllowedKeys[key]
	stored, err := s.storedValue(key, value)
	if err != nil {
		return sqlc.AuraSettings{}, err
	}
	var updatedBy pgtype.Text
	if by != "" {
		updatedBy = pgtype.Text{String: by, Valid: true}
	}
	var row sqlc.AuraSettings
	err = s.withWriteLock(ctx, func(q *sqlc.Queries) error {
		var err error
		row, err = q.UpsertSetting(ctx, sqlc.UpsertSettingParams{
			Key: key, Value: stored, IsSecret: meta.Secret, UpdatedBy: updatedBy,
		})
		return err
	})
	if err != nil {
		return row, err
	}
	row.Value = value
	return row, nil
}

// ReplaceMany writes one model-profile mutation under one advisory-locked transaction.
// Model-scoped limits from the previous route are deleted in the same commit as the new
// route rows, then the caller publishes its already-prepared runtime snapshot.
func (s *Store) ReplaceMany(
	ctx context.Context, values map[string]string, deletes []string, by string,
) ([]sqlc.AuraSettings, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	deleteKeys := append([]string(nil), deletes...)
	sort.Strings(deleteKeys)
	rows := make([]sqlc.AuraSettings, 0, len(keys))
	err := s.withWriteLock(ctx, func(q *sqlc.Queries) error {
		for _, key := range deleteKeys {
			if err := q.DeleteSetting(ctx, key); err != nil {
				return err
			}
		}
		for _, key := range keys {
			meta := AllowedKeys[key]
			stored, err := s.storedValue(key, values[key])
			if err != nil {
				return err
			}
			var updatedBy pgtype.Text
			if by != "" {
				updatedBy = pgtype.Text{String: by, Valid: true}
			}
			row, err := q.UpsertSetting(ctx, sqlc.UpsertSettingParams{
				Key: key, Value: stored, IsSecret: meta.Secret, UpdatedBy: updatedBy,
			})
			if err != nil {
				return err
			}
			row.Value = values[key]
			rows = append(rows, row)
		}
		return nil
	})
	return rows, err
}

// Delete removes a key. The hot primary-route reloader restores its captured boot
// fallback immediately; other keys revert to environment/.env on restart.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.withWriteLock(ctx, func(q *sqlc.Queries) error {
		return q.DeleteSetting(ctx, key)
	})
}

// settingsWriteAdvisoryKey serializes every aura.settings write against itself.
const settingsWriteAdvisoryKey int64 = 4707759426009125972

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
		ctx, "SELECT pg_advisory_xact_lock($1)", settingsWriteAdvisoryKey,
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
