package pimprovider

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/secret"
)

// keyDerivationInfo must differ from every other store's label: two stores sharing one would
// share a key, so one leak would open both.
const keyDerivationInfo = "aura-pim-provider-app-key-v1"

// Store is the Postgres table aura.pim_provider_app.
type Store struct {
	q      *sqlc.Queries
	sealer *secret.Sealer
}

// NewStore builds the store. authulaSecretHex is AURA_AUTHULA_SECRET, the master secret every
// sealed Aura store derives its own key from.
func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error) {
	if pool == nil {
		return nil, errors.New("pimprovider: a database pool is required")
	}
	sealer, err := secret.NewSealer(authulaSecretHex, keyDerivationInfo)
	if err != nil {
		return nil, err
	}
	return &Store{q: sqlc.New(pool), sealer: sealer}, nil
}

// List returns every configured app without decrypting a secret: SecretSet says whether one is
// stored, which is all the provider list shows.
func (s *Store) List(ctx context.Context) ([]App, error) {
	rows, err := s.q.ListPIMProviderApps(ctx)
	if err != nil {
		return nil, fmt.Errorf("pimprovider: list: %w", err)
	}
	apps := make([]App, 0, len(rows))
	for _, row := range rows {
		apps = append(apps, App{
			Provider:  row.Provider,
			ClientID:  row.ClientID,
			TenantID:  row.TenantID,
			SecretSet: row.SecretSet,
			UpdatedAt: row.UpdatedAt.Time,
			UpdatedBy: row.UpdatedBy,
		})
	}
	return apps, nil
}

// Get returns one provider's app with its secret decrypted, or ErrNotConfigured.
func (s *Store) Get(ctx context.Context, provider string) (App, error) {
	row, err := s.q.GetPIMProviderApp(ctx, provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return App{}, ErrNotConfigured
	}
	if err != nil {
		return App{}, fmt.Errorf("pimprovider: get %s: %w", provider, err)
	}
	plaintext, err := s.sealer.OpenOptional(row.ClientSecretCiphertext)
	if err != nil {
		return App{}, fmt.Errorf("pimprovider: open the %s secret: %w", provider, err)
	}
	return App{
		Provider:     row.Provider,
		ClientID:     row.ClientID,
		TenantID:     row.TenantID,
		ClientSecret: string(plaintext),
		SecretSet:    len(row.ClientSecretCiphertext) > 0,
		UpdatedAt:    row.UpdatedAt.Time,
		UpdatedBy:    row.UpdatedBy,
	}, nil
}

// Upsert saves app. A Google app without a secret keeps the stored one through a plain UPDATE:
// Postgres checks the row CHECK against the proposed row of an INSERT … ON CONFLICT before it
// handles the conflict, so a ('google', NULL) upsert fails even when a secret is stored. That
// UPDATE matches only the stored client ID, and ErrStale reports that it no longer is.
func (s *Store) Upsert(ctx context.Context, app App, updatedBy string) error {
	if app.Provider == Google && app.ClientSecret == "" {
		n, err := s.q.UpdatePIMProviderAppKeepSecret(ctx, sqlc.UpdatePIMProviderAppKeepSecretParams{
			Provider: app.Provider, ClientID: app.ClientID, UpdatedBy: updatedBy,
		})
		if err != nil {
			return fmt.Errorf("pimprovider: update %s: %w", app.Provider, err)
		}
		if n == 0 {
			return ErrStale
		}
		return nil
	}
	ciphertext, err := s.sealer.SealOptional([]byte(app.ClientSecret))
	if err != nil {
		return fmt.Errorf("pimprovider: seal the %s secret: %w", app.Provider, err)
	}
	if err := s.q.UpsertPIMProviderApp(ctx, sqlc.UpsertPIMProviderAppParams{
		Provider: app.Provider, ClientID: app.ClientID, TenantID: app.TenantID,
		ClientSecretCiphertext: ciphertext, UpdatedBy: updatedBy,
	}); err != nil {
		return fmt.Errorf("pimprovider: upsert %s: %w", app.Provider, err)
	}
	return nil
}
