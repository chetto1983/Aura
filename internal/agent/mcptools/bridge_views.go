package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_views.go is the MCP Apps half of the bridge: a mounted server may bind a
// tool to a `ui://` document (SEP-1865), and a host that can render it shows the
// document instead of — or beside — the tool's text.
//
// Two things happen here and nowhere else. At MOUNT, every distinct `ui://`
// resource the advertised tools point at is read once into the process catalog,
// so no HTTP request later has to hold an MCP session. At CALL, a view-bound
// tool's result carries the descriptor the cockpit needs to render: which server,
// which document, and the server's own structuredContent.
//
// Nothing here names a server. The first one to use it is a fork of ours and that
// must stay invisible: the gate is the mount's TRUST CLASS, read through
// mcp.TrustMayRenderViews, never a table of known names.

// viewMetaKey is the ToolResult.Meta member a view-bound call hangs its descriptor
// off. llm_agent_events.go lifts exactly this key onto Actions.ViewDelta.
const viewMetaKey = "mcp_view"

// MaxViewPayloadBytes bounds the structuredContent one call may push to a view.
// The payload rides the SSE stream to the browser, so an unbounded one is a
// mounted server choosing how much of the operator's downlink to spend. Over the
// cap the descriptor still goes out — the view renders its empty state and the
// human sees the tool's text — because dropping the whole descriptor would look
// to the cockpit exactly like a server that declared no view at all.
const MaxViewPayloadBytes = 512 * 1024

// viewRefFor reads a tool's view binding, gated on the mount's policy. A mount
// whose trust class may not render views yields the zero ref, so no view-bound
// descriptor is ever produced for it — the tool still works and still returns its
// text, which is the extension's own progressive-enhancement contract.
func viewRefFor(policy bridgePolicy, t *sdkmcp.Tool) mcp.ViewRef {
	if !policy.views {
		return mcp.ViewRef{}
	}
	ref, ok := mcp.ViewRefFromMeta(t.Meta)
	if !ok {
		return mcp.ViewRef{}
	}
	return ref
}

// viewDescriptor builds the per-call descriptor a view-bound result carries, or
// reports false for a tool with no view. The tool_call_id is deliberately NOT set
// here: the bridge does not know it, and the lift in llm_agent_events.go does.
func (b *bridgedTool) viewDescriptor(payload mcp.ToolPayload) (map[string]any, bool) {
	if b.view.ResourceURI == "" {
		return nil, false
	}
	descriptor := map[string]any{
		"server":       b.srv.name,
		"resource_uri": b.view.ResourceURI,
	}
	switch {
	case len(payload.Structured) == 0:
	case len(payload.Structured) > MaxViewPayloadBytes:
		descriptor["payload_dropped_bytes"] = len(payload.Structured)
		slog.Warn("mcp view payload over cap",
			"server", b.srv.name, "tool", b.name,
			"bytes", len(payload.Structured), "cap", MaxViewPayloadBytes)
	default:
		descriptor["structured_content"] = payload.Structured
	}
	return descriptor, true
}

// hydrateViews reads each distinct `ui://` resource the advertised tools bind to
// and stores it in the catalog. It runs once per mount, on the raw session, and
// never fails the mount: a server whose document could not be read still serves
// its tools, it just has nothing to render — which is strictly better than
// refusing to mount a working server over a decoration.
func hydrateViews(ctx context.Context, session *sdkmcp.ClientSession, server string, catalog *mcp.ViewCatalog, advertised []*sdkmcp.Tool, policy bridgePolicy) {
	if catalog == nil || !policy.views || !servesResources(session) {
		return
	}
	for _, uri := range viewURIs(policy, advertised) {
		doc, err := readViewDocument(ctx, session, server, uri)
		if err != nil {
			slog.Warn("mcp view read failed", "server", server, "uri", uri, "error", err)
			continue
		}
		if err := catalog.Put(doc); err != nil {
			slog.Warn("mcp view rejected", "server", server, "uri", uri, "error", err)
			continue
		}
		slog.Info("mcp view catalogued",
			"server", server, "uri", uri,
			"bytes", len(doc.HTML), "sealed", doc.Policy.Sealed())
	}
}

// servesResources reports whether it is worth asking this peer for a resource at
// all — the guard go-sdk's own listfeatures example makes before touching
// resources/* (examples/client/listfeatures/main.go).
//
// A nil InitializeResult is a YES, not a no: protocol 2026-07-28 removed the
// initialize exchange entirely (SEP-2575), so a modern peer has no capability
// block to consult and the only way to learn is to ask.
func servesResources(session *sdkmcp.ClientSession) bool {
	result := session.InitializeResult()
	return result == nil || result.Capabilities.Resources != nil
}

// viewURIs returns the distinct `ui://` URIs the advertised tools bind to, sorted
// so a mount reads them in a stable order and its log is diffable.
func viewURIs(policy bridgePolicy, advertised []*sdkmcp.Tool) []string {
	seen := map[string]struct{}{}
	for _, t := range advertised {
		if ref := viewRefFor(policy, t); ref.ResourceURI != "" {
			seen[ref.ResourceURI] = struct{}{}
		}
	}
	uris := make([]string, 0, len(seen))
	for uri := range seen {
		uris = append(uris, uri)
	}
	sort.Strings(uris)
	return uris
}

// readViewDocument reads one `ui://` resource.
//
// The MIME check is the load-bearing line: the extension serves view documents as
// exactly one type, and a host that renders a `ui://` resource arriving under any
// other type would let a server smuggle a document past the one declaration that
// says "this is meant to be rendered".
func readViewDocument(ctx context.Context, session *sdkmcp.ClientSession, server, uri string) (mcp.ViewDocument, error) {
	result, err := mcp.BoundedCall(ctx, func(ctx context.Context) (*sdkmcp.ReadResourceResult, error) {
		return session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	}, nil)
	if err != nil {
		return mcp.ViewDocument{}, err
	}
	if result == nil {
		return mcp.ViewDocument{}, fmt.Errorf("empty read result")
	}
	for _, part := range result.Contents {
		if part == nil || part.MIMEType != mcp.AppMIMEType {
			continue
		}
		return mcp.ViewDocument{
			Server:      server,
			ResourceURI: uri,
			HTML:        part.Text,
			Policy:      mcp.ViewPolicyFromMeta(viewDeclaration(part, result)),
		}, nil
	}
	return mcp.ViewDocument{}, fmt.Errorf("no %s content", mcp.AppMIMEType)
}

// CallReadOnlyTool runs a tool on behalf of a RENDERED VIEW and refuses anything
// that is not read-only.
//
// A view asks its host for tools/call so it can page, refresh and drill down —
// that is what makes it a view rather than a screenshot. But the request comes
// from a document the server itself wrote, with no human in the loop and no
// agent turn around it, so it gets a strictly smaller grant than the model has:
// reads only, on the server that served the view, and nothing else. A mutating
// call still exists — the human asks for it in the chat, where the approval path
// lives.
//
// toolIsReadOnly fails closed for a name this server never advertised, so an
// unknown tool is refused by the same line that refuses a dangerous one.
func (s *MountedServer) CallReadOnlyTool(ctx context.Context, name string, args map[string]any) (mcp.ToolPayload, error) {
	if !s.toolIsReadOnly(name) {
		return mcp.ToolPayload{}, fmt.Errorf("mcp %q: tool %q is not callable from a view", s.name, name)
	}
	return s.CallTool(ctx, name, args)
}

// ViewCallers resolves the mounted server a rendered view is allowed to call
// back into. It is a map and not an interface per server because the ONE thing a
// caller must not be able to do is reach a server other than the one whose
// document it is: the lookup is the check.
type ViewCallers map[string]*MountedServer

// CallForView runs a read-only tool on the named server. An unknown server is an
// error, never a silent empty result — a view that reached the wrong host should
// say so rather than render nothing and look merely empty.
func (c ViewCallers) CallForView(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, error) {
	host, ok := c[server]
	if !ok {
		return nil, fmt.Errorf("mcp %q: not mounted, or its views may not call back", server)
	}
	payload, err := host.CallReadOnlyTool(ctx, tool, args)
	if err != nil {
		return nil, err
	}
	return payload.Structured, nil
}

// viewDeclaration picks where the framing declaration lives. The spec hangs it off
// the resource, and SDKs differ on whether that surfaces on the content part or on
// the read result, so both are consulted — the part first, because it is the more
// specific of the two.
func viewDeclaration(part *sdkmcp.ResourceContents, result *sdkmcp.ReadResourceResult) map[string]any {
	if len(part.Meta) > 0 {
		return part.Meta
	}
	return result.Meta
}
