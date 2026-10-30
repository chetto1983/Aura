package main

import (
	"context"
	"log/slog"
	"net/mail"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/remotetunnel"
	"github.com/chetto1983/aura/internal/settings"
)

func wireRemoteAccess(ctx context.Context, server *agui.Server, chat *chatEnv) {
	if chat.pool == nil || chat.cfg == nil {
		return
	}
	secrets, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("Remote access settings unavailable")
		return
	}
	state := remotetunnel.NewStore(chat.pool)
	// This fixed Compose mount is disposable and is never a configuration authority.
	projection := remotetunnel.NewFileProjection("/var/lib/aura/cloudflared", 65532, 65532)
	controller := newRemoteAccessController(state, secrets, projection, remoteIdentityMembers{store: chat.identity}, remoteAccessCommit(secrets))
	server.SetRemoteAccess(controller)
	server.SetIdentityChanged(controller.Wake)
	controller.Start(ctx)
	chat.mcpClosers = append(chat.mcpClosers, controller.Close)
}

type remoteIdentityStore interface {
	ListIdentities(context.Context) ([]identity.Identity, error)
	HasCapability(context.Context, string, string) (bool, error)
}
type remoteIdentityMembers struct{ store remoteIdentityStore }

func (m remoteIdentityMembers) ActiveEmails(ctx context.Context) ([]string, error) {
	return m.emails(ctx, false)
}
func (m remoteIdentityMembers) ActiveAdminEmails(ctx context.Context) ([]string, error) {
	return m.emails(ctx, true)
}
func (m remoteIdentityMembers) emails(ctx context.Context, admins bool) ([]string, error) {
	identities, err := m.store.ListIdentities(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, id := range identities {
		if id.Deactivated || id.Kind != "user" {
			continue
		}
		email := strings.TrimSpace(id.Name)
		parsed, e := mail.ParseAddress(email)
		if e != nil || parsed.Address != email {
			continue
		}
		if admins {
			allowed, e := m.store.HasCapability(ctx, id.ID, identity.CapIdentityCreate)
			if e != nil {
				return nil, e
			}
			if !allowed {
				continue
			}
		}
		out = append(out, email)
	}
	return out, nil
}

// remoteAccessCommit stores the candidate token and the desired state in one transaction. When
// the token replaces one that could reach resources this one cannot (abandoned is not nil), the
// same transaction forgets those resources and the connector credential that served them, so a
// failure leaves the row, the list and both secrets as they were. Abandon keeps the generation,
// which is why SaveDesired follows it.
func remoteAccessCommit(secrets *settings.Store) remoteDesiredCommit {
	return func(ctx context.Context, generation int64, desired remotetunnel.Desired, token string, abandoned []remotetunnel.AbandonedResource, actor string) error {
		_, err := secrets.UpsertWith(ctx, "CLOUDFLARE_API_TOKEN", token, actor, func(tx sqlc.DBTX) error {
			state := remotetunnel.NewStore(tx)
			if abandoned != nil {
				if _, e := state.Abandon(ctx, generation, abandoned); e != nil {
					return e
				}
				if e := secrets.UpsertInTx(ctx, tx, "CLOUDFLARE_TUNNEL_TOKEN", "", actor); e != nil {
					return e
				}
			}
			_, e := state.SaveDesired(ctx, generation, desired, actor)
			return e
		})
		return err
	}
}
