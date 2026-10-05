package main

// serve_notify.go wires the scheduler notifier (cron.Notifier) onto the live runtime: the
// MCP tools mounted from the WhatsApp and PIM recipes, and where each identity itself is
// reached (the WhatsApp number it paired, the email it signs in with). One notifier both
// delivers at fire time and answers the `task` tool's schedule-time route check.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sync/atomic"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/whatsappbridge"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schedulerNotifySessionID is a valid, reserved UUID naming the scheduler's own tool calls,
// mirroring toolPipeSessionID. A bridged MCP tool builds its result through
// tools.NewResult, which refuses to run without a tool-call context: measured 2026-10-05,
// a WhatsApp reminder was SENT and then reported failed, so each retry sent it again.
const schedulerNotifySessionID = "00000000-0000-0000-0000-000000000044"

// schedulerNotifyCallSeq names each self-send. The id would name a sidecar file if a result
// outgrew the preview cap, so two sends must not share one.
var schedulerNotifyCallSeq atomic.Uint64

// newSchedulerNotifier is the notifier the scheduler delivers through and the `task` tool
// checks a route against before persisting. A nil pool leaves email without a default
// recipient; that is only the pool-free manifest path, whose task tool never checks.
func newSchedulerNotifier(cfg *config.Config, reg *tools.Registry, pool *pgxpool.Pool) cron.Notifier {
	own := ownAddresses{whatsapp: whatsappbridge.New(cfg.WhatsAppBridgeURL, cfg.WhatsAppBridgeToken)}
	if pool != nil {
		own.identities = identity.New(pool)
	}
	return cron.NewNotifier(selfSendResolver{reg: reg, runDir: cfg.RunDir, previewCap: cfg.ToolPreviewCap}, own)
}

// selfSendResolver finds the tool a route rides by the managed recipe the bridge mounted
// it from, never by its registered name, which the bridge may truncate and hash.
type selfSendResolver struct {
	reg        *tools.Registry
	runDir     string
	previewCap int
}

func (r selfSendResolver) Resolve(recipe, tool string) (cron.SelfSendTool, bool) {
	if r.reg == nil {
		return nil, false
	}
	for _, t := range r.reg.All() {
		spec := t.Spec()
		if spec.TrustedRecipeSource == recipe && spec.TrustedRecipeTool == tool {
			return selfSendTool{tool: t, runDir: r.runDir, previewCap: r.previewCap}, true
		}
	}
	return nil, false
}

// selfSendTool executes a resolved MCP tool with the arguments the notifier built. An
// Execute error, including a result the MCP server flagged as failed, is undelivered.
type selfSendTool struct {
	tool       tools.Tool
	runDir     string
	previewCap int
}

func (s selfSendTool) Send(ctx context.Context, args json.RawMessage) error {
	callID := fmt.Sprintf("scheduler-notify-%d", schedulerNotifyCallSeq.Add(1))
	ctx = tools.WithToolCallContext(ctx, schedulerNotifySessionID, callID, s.runDir, s.previewCap)
	if _, err := s.tool.Execute(ctx, args); err != nil {
		return fmt.Errorf("mcp self-send %q: %w", s.tool.Spec().Name, err)
	}
	return nil
}

// ownAddresses reads where an identity itself is reached on an external route.
type ownAddresses struct {
	whatsapp   whatsappbridge.Client
	identities *identity.Store
}

func (a ownAddresses) OwnAddress(ctx context.Context, route cron.NotifyRoute, identityID string) (string, error) {
	switch route {
	case cron.RouteWhatsApp:
		return a.whatsapp.LinkedNumber(ctx, identityID)
	case cron.RouteEmail:
		if a.identities == nil {
			return "", errors.New("no identity store to read the sign-in email from")
		}
		owner, err := a.identities.GetIdentityByID(ctx, identityID)
		if err != nil {
			return "", err
		}
		return signInEmail(owner)
	default:
		return "", fmt.Errorf("route %s addresses no one", route)
	}
}

// signInEmail is the address a user identity signs in with: its name, which serve_auth.go
// joins to the Authula user's email.
func signInEmail(owner identity.Identity) (string, error) {
	if owner.Kind != "user" {
		return "", fmt.Errorf("a %s identity has no sign-in email", owner.Kind)
	}
	if _, err := mail.ParseAddress(owner.Name); err != nil {
		return "", errors.New("the identity's sign-in name is not an email address")
	}
	return owner.Name, nil
}
