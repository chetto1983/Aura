// Package mcpregistry owns the MCP server registry: which servers this deployment has
// configured, what transport each speaks, whether it is enabled, and which profiles it
// belongs to.
//
// It exists to be the ONE place that answers those questions. Before it, the registry was a
// single root-owned JSON file, and the cockpit board did not read that file alone — it
// served `file + the config snapshot taken at boot`, while every write read and wrote the
// file only. Two read paths over one logical thing is a split brain, and on 2026-08-24 it
// produced the failure that shape predicts: the file was truncated to an empty server map
// while the board went on listing servers from the boot snapshot, so Remove answered "not
// found" for a server the operator could see on screen.
//
// The shape is LibreChat's IServerConfigsRepositoryInterface (packages/api/src/mcp/registry):
// one interface with get / getAll / upsert / remove, and storage behind it. Their DB
// implementation holds the servers a person added; servers declared by the deployment stay
// declared and are overlaid at read time. Aura draws the same line — catalog recipes remain
// code-declared so an upgrade still updates them — but keeps operator-installed servers in
// Postgres rather than a file, next to the MCP audit trail (migration 0022) and the
// per-identity OAuth grants (0100) that were already there.
package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/secret"
)

// keyDerivationInfo domain-separates this store's wrapping key from every other key derived
// from AURA_AUTHULA_SECRET. Sharing a label with internal/mcpoauth or internal/objectstore
// would mean one leaked key opens all three.
const keyDerivationInfo = "aura-mcp-registry-key-v1"

// ErrNotFound reports that no server is registered under that name.
var ErrNotFound = errors.New("mcpregistry: no such server")

// Store is the Postgres-backed registry.
type Store struct {
	q      *sqlc.Queries
	sealer *secret.Sealer
}

// NewStore builds the store. authulaSecretHex is the 64-hex-character AURA_AUTHULA_SECRET,
// the same master secret the OAuth grant store and the identity object store derive from.
func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error) {
	if pool == nil {
		return nil, errors.New("mcpregistry: a database pool is required")
	}
	sealer, err := secret.NewSealer(authulaSecretHex, keyDerivationInfo)
	if err != nil {
		return nil, err
	}
	return &Store{q: sqlc.New(pool), sealer: sealer}, nil
}

// Tx returns a Store bound to an open transaction's queries, so a registry change can
// commit together with whatever else that transaction is doing.
//
// The audit ledger is the caller that needs this. The file-based registry it replaces got
// all-or-nothing by staging a temp file, inserting the audit row in a transaction and only
// renaming on commit (D-04); with both halves in Postgres the same guarantee is just a
// transaction, and there is no half-written file left behind when a process dies mid-write.
func (s *Store) Tx(q *sqlc.Queries) *Store {
	return &Store{q: q, sealer: s.sealer}
}

// Entry is one registered server plus the profile membership that decides whether anything
// will actually mount it.
//
// Membership lives on the entry rather than in a separate map because the file kept it
// apart, and that is precisely how an install could write the server and forget the
// profile: the row appeared on the board, mounted nowhere, and nothing on screen explained
// why. One object, written in one statement, cannot half-happen.
type Entry struct {
	Name     string
	Server   mcp.ManagedServer
	Profiles []string
}

// List returns every registered server, keyed by name, with profile membership rebuilt into
// the ManagedConfig shape the rest of Aura already speaks.
func (s *Store) List(ctx context.Context) (mcp.ManagedConfig, error) {
	rows, err := s.q.ListMCPServers(ctx)
	if err != nil {
		return mcp.ManagedConfig{}, fmt.Errorf("mcpregistry: list: %w", err)
	}
	doc := mcp.ManagedConfig{
		Version:    mcp.ManagedConfigVersion,
		MCPServers: make(map[string]mcp.ManagedServer, len(rows)),
		Profiles:   map[string]mcp.ManagedProfile{},
	}
	for _, row := range rows {
		entry, err := s.entryFrom(row)
		if err != nil {
			return mcp.ManagedConfig{}, err
		}
		doc.MCPServers[entry.Name] = entry.Server
		for _, profile := range entry.Profiles {
			p := doc.Profiles[profile]
			p.Servers = append(p.Servers, entry.Name)
			doc.Profiles[profile] = p
		}
	}
	mcp.Normalize(&doc)
	return doc, nil
}

// Upsert writes a server, creating or replacing it in one statement. createdBy is the
// identity performing the install; it is recorded for the audit trail and is never used to
// decide who may see the row.
func (s *Store) Upsert(ctx context.Context, entry Entry, createdBy string) error {
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		return errors.New("mcpregistry: a server needs a name")
	}
	// Env is split out and sealed; what remains is configuration the board can render
	// without a key.
	bare := entry.Server
	bare.Env = nil
	config, err := json.Marshal(bare)
	if err != nil {
		return fmt.Errorf("mcpregistry: encode config for %q: %w", name, err)
	}
	var envEnc []byte
	if len(entry.Server.Env) > 0 {
		plaintext, err := json.Marshal(entry.Server.Env)
		if err != nil {
			return fmt.Errorf("mcpregistry: encode env for %q: %w", name, err)
		}
		if envEnc, err = s.sealer.SealOptional(plaintext); err != nil {
			return fmt.Errorf("mcpregistry: seal env for %q: %w", name, err)
		}
	}
	profiles := entry.Profiles
	if profiles == nil {
		profiles = []string{}
	}
	if _, err := s.q.UpsertMCPServer(ctx, sqlc.UpsertMCPServerParams{
		Name:      name,
		Source:    strings.TrimSpace(entry.Server.Source),
		Enabled:   boolValue(entry.Server.Enabled),
		Config:    config,
		EnvEnc:    envEnc,
		Profiles:  profiles,
		CreatedBy: uuidValue(createdBy),
	}); err != nil {
		return fmt.Errorf("mcpregistry: upsert %q: %w", name, err)
	}
	return nil
}

// Remove deletes a server and, with it, its profile membership — the row carries both, so
// there is no second place left holding a name that no longer resolves.
func (s *Store) Remove(ctx context.Context, name string) error {
	affected, err := s.q.DeleteMCPServer(ctx, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("mcpregistry: remove %q: %w", name, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return nil
}

func (s *Store) entryFrom(row sqlc.AuraMcpServer) (Entry, error) {
	var server mcp.ManagedServer
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &server); err != nil {
			return Entry{}, fmt.Errorf("mcpregistry: decode config for %q: %w", row.Name, err)
		}
	}
	server.Source = row.Source
	if row.Enabled.Valid {
		enabled := row.Enabled.Bool
		server.Enabled = &enabled
	} else {
		server.Enabled = nil
	}
	plaintext, err := s.sealer.OpenOptional(row.EnvEnc)
	if err != nil {
		return Entry{}, fmt.Errorf("mcpregistry: open env for %q: %w", row.Name, err)
	}
	if len(plaintext) > 0 {
		if err := json.Unmarshal(plaintext, &server.Env); err != nil {
			return Entry{}, fmt.Errorf("mcpregistry: decode env for %q: %w", row.Name, err)
		}
	}
	return Entry{Name: row.Name, Server: server, Profiles: row.Profiles}, nil
}

func boolValue(enabled *bool) pgtype.Bool {
	if enabled == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *enabled, Valid: true}
}

func uuidValue(id string) pgtype.UUID {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return pgtype.UUID{}
	}
	var out pgtype.UUID
	if err := out.Scan(trimmed); err != nil {
		// A non-UUID actor (the CLI's service identity) is recorded as absent rather than
		// failing the write: created_by is audit metadata, and losing it must never cost
		// the operator the install.
		return pgtype.UUID{}
	}
	return out
}
