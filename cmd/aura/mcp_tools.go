package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcpoauth"
)

func mcpTools(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp tools <name>")
	}
	name := args[0]
	if server, ok, err := effectiveManagedMCPServer(name); err != nil {
		return err
	} else if ok {
		session, advertised, err := openAndListManagedMCPTools(ctx, name, server)
		if err != nil {
			return err
		}
		defer func() { _ = session.Close() }()
		return writeMCPTools(out, advertised)
	}
	cfg, err := effectiveMCPServer(name)
	if err != nil {
		return err
	}
	session, advertised, err := openAndListMCPTools(ctx, name, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	return writeMCPTools(out, advertised)
}

func writeMCPTools(out io.Writer, advertised []*sdkmcp.Tool) error {
	sort.Slice(advertised, func(i, j int) bool { return advertised[i].Name < advertised[j].Name })
	for _, t := range advertised {
		if err := writef(out, "%s\tmounted\t%s\n",
			t.Name,
			firstMCPDescriptionLine(t.Description),
		); err != nil {
			return err
		}
	}
	return nil
}

func effectiveManagedMCPServer(name string) (mcp.ManagedServer, bool, error) {
	_, policies, err := mcpRuntimeSet()
	if err != nil {
		return mcp.ManagedServer{}, false, err
	}
	server, ok := policies[name]
	return server, ok, nil
}

func effectiveMCPServer(name string) (mcp.ServerConfig, error) {
	servers, _, err := mcpRuntimeSet()
	if err != nil {
		return mcp.ServerConfig{}, err
	}
	server, ok := servers[name]
	if !ok {
		return mcp.ServerConfig{}, fmt.Errorf("MCP server %q is not configured or is disabled", name)
	}
	return server, nil
}

func runtimeMCPEgressPolicy(server mcp.ManagedServer) mcp.EgressPolicy {
	cfg := config.LoadDB()
	return mcp.RuntimeEgressPolicy(cfg.Profile.Strict(), server)
}

// cliSessionOptions is the SessionOptions every CLI-opened session carries: the
// _meta.aura operation stamp, the same middleware mount.go's sendingMiddleware
// attaches to every agent-bridge mount — a policy fix in one cannot miss the other.
func cliSessionOptions() mcp.SessionOptions {
	return mcp.SessionOptions{Sending: []sdkmcp.Middleware{mcp.OperationMetaMiddleware()}}
}

// openManagedMCPSession opens a managed server's session through the SDK. Renamed
// from its pre-SDK "...Transport" name: that word now names a specific, different
// SDK type, and keeping the old name would mislead a future reader.
func openManagedMCPSession(ctx context.Context, name string, server mcp.ManagedServer) (*sdkmcp.ClientSession, error) {
	return mcp.OpenSDKSession(ctx, name, server, runtimeMCPEgressPolicy(server), cliSessionOptions())
}

// probeManagedMCPServer is the board's per-row liveness check. It carries the same
// credentials a mount would, or an authorized server reports "dial failed" on screen while
// its tools work perfectly well in the agent.
func probeManagedMCPServer(ctx context.Context, name string, server mcp.ManagedServer) mcp.ProbeResult {
	return mcp.ProbeServerWithOptions(ctx, name, server, runtimeMCPEgressPolicy(server), mcp.SessionOptions{
		OAuth: runtimeMCPOAuth(ctx),
	})
}

func openAndListMCPTools(ctx context.Context, name string, cfg mcp.ServerConfig) (*sdkmcp.ClientSession, []*sdkmcp.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpInspectionTimeout())
	defer cancel()
	session, err := mcp.OpenSDKSessionForConfig(ctx, ctx, name, cfg, cliSessionOptions())
	if err != nil {
		return nil, nil, err
	}
	advertised, err := drainSDKTools(ctx, session)
	if err != nil {
		mcp.ReleaseSession(session)
		return nil, nil, err
	}
	return session, advertised, nil
}

func openAndListManagedMCPTools(ctx context.Context, name string, server mcp.ManagedServer) (*sdkmcp.ClientSession, []*sdkmcp.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpInspectionTimeout())
	defer cancel()
	var (
		session *sdkmcp.ClientSession
		err     error
	)
	if strings.TrimSpace(server.Type) == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != "" {
		session, err = openManagedMCPSession(ctx, name, server)
	} else {
		cfg, cfgErr := mcpmanager.RuntimeLaunchConfig(name, server)
		if cfgErr != nil {
			return nil, nil, cfgErr
		}
		session, err = mcp.OpenSDKSessionForConfig(ctx, ctx, name, cfg, cliSessionOptions())
	}
	if err != nil {
		return nil, nil, err
	}
	advertised, err := drainSDKTools(ctx, session)
	if err != nil {
		mcp.ReleaseSession(session)
		return nil, nil, err
	}
	return session, advertised, nil
}

// drainSDKTools drains session's paginated tool list bounded by ctx rather than
// however long the peer takes to answer — mirrors bridge_supervisor.go's drainTools
// and probe.go's countTools (internal/mcp/bounded_call.go: the SDK observes a
// deadline only between whole dual-era handshake rungs, never mid-call).
func drainSDKTools(ctx context.Context, session *sdkmcp.ClientSession) ([]*sdkmcp.Tool, error) {
	return mcp.BoundedCall(ctx, func(ctx context.Context) ([]*sdkmcp.Tool, error) {
		var out []*sdkmcp.Tool
		for t, err := range session.Tools(ctx, nil) {
			if err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, nil
	}, nil)
}

// callSessionText calls tool on session and decodes its result through the shared
// domain-outcome chain — the ONE CLI decode site in the tree (RESEARCH Pitfall 1;
// mirrors bridge_supervisor.go's decodeResult). server names the MCP server the
// session belongs to, for the decoded error's "mcp %q: tool ..." shape.
//
// scoped gates the _meta.aura.user_identifier stamp (D-108): only the memory path
// carries it, through the same mcp.SetAuraMetaField helper the agent bridge's
// IdentityMetaMiddleware uses, so a key-name divergence between the two writers is
// a build-time-testable invariant. A scoped call with no resolvable ctx identity
// returns the existing "no identity to scope to" error rather than stamping an
// empty string — runMemory resolves the real owner up front, so an unscoped CLI
// context here is a wiring bug, not a call the bridge's operator-default should
// paper over.
func callSessionText(ctx context.Context, session *sdkmcp.ClientSession, server, tool string, args map[string]any, scoped bool) (string, error) {
	params := &sdkmcp.CallToolParams{Name: tool, Arguments: args}
	if scoped {
		identityID := identityctx.IdentityID(ctx)
		if identityID == "" {
			return "", errors.New(
				"memory call has no identity to scope to: resolve the operator identity before calling")
		}
		mcp.SetAuraMetaField(params, mcp.MetaFieldUserIdentifier, identityID)
	}
	res, err := session.CallTool(ctx, params)
	if err != nil {
		return "", err
	}
	text, isErr := mcp.DecodeToolResult(res)
	if isErr {
		return "", mcp.DecodeToolCallError(server, tool, text)
	}
	return text, nil
}

// mcpInspectionTimeout keeps single-server tools/doctor on the same operator-owned
// budget as status and doctor --all. A separate hard-coded 20s deadline made the
// timeout knob ineffective precisely on the interactive diagnostics used for a hung
// server.
func mcpInspectionTimeout() time.Duration {
	return resolveMCPProbeTimeout()
}

func firstMCPDescriptionLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func toolCount(n int) string {
	if n == 1 {
		return "1 tool"
	}
	return fmt.Sprintf("%d tools", n)
}

var (
	grantsOnce sync.Once
	grants     mcp.GrantStore
)

// runtimeMCPOAuth is the ONE answer to "which credentials does a session to this server
// carry", the twin of runtimeMCPEgressPolicy's "where may it reach".
//
// It exists because there were three: the boot mount, the post-authorization mount and the
// board probe each built their own SessionOptions, and each forgot something different —
// so a server the human had just authorized mounted at boot, failed the mount that
// followed the consent, and showed "dial failed" on the board, all at the same time, all
// from the same missing store. One resolver, three callers.
//
// The store is supplied ONLY when ctx carries an identity. It refuses an identity-less
// context by design (a token belongs to a person), so handing it one turns every internal
// sidecar's mount into "no identity on context" — measured 2026-08-24, when calendar spent
// 40 retries on it for a server that has no authorization to do.
func runtimeMCPOAuth(ctx context.Context) mcp.OAuthOptions {
	if strings.TrimSpace(identityctx.IdentityID(ctx)) == "" {
		return mcp.OAuthOptions{}
	}
	grantsOnce.Do(func() {
		cfg := config.LoadDB()
		if strings.TrimSpace(cfg.AuthulaSecret) == "" {
			return
		}
		pool, err := db.Open(ctx, &cfg.DB)
		if err != nil {
			slog.Warn("mcp oauth: no grant store; an authorized server will not mount", "err", err)
			return
		}
		store, err := mcpoauth.NewStore(pool, cfg.AuthulaSecret)
		if err != nil {
			slog.Warn("mcp oauth: no grant store; an authorized server will not mount", "err", err)
			return
		}
		grants = store
	})
	if grants == nil {
		return mcp.OAuthOptions{}
	}
	// No Fetcher anywhere: every caller here is unattended (a boot, a probe, a mount that
	// follows a consent already completed). A server with no stored grant must refuse with
	// the instruction to authorize it, not wait on a browser nobody is at.
	return mcp.OAuthOptions{Store: grants}
}
