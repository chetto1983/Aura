package mcptools

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

var errIdentitySessionPoolClosed = errors.New("remote MCP identity session pool is closed")

type identitySessionEntry struct {
	ready  chan struct{}
	server *MountedServer
	err    error
}

// identitySessionPool keeps one SDK session per OAuth subject behind one global
// tool manifest. Each child restores its token through the existing grant store;
// the parent never owns a bearer and cannot accidentally share one across users.
type identitySessionPool struct {
	parent     *MountedServer
	connect    openSessionFunc
	processCtx context.Context

	mu      sync.Mutex
	closed  bool
	entries map[string]*identitySessionEntry
}

func newIdentitySessionPool(parent *MountedServer, connect openSessionFunc, processCtx context.Context) *identitySessionPool {
	return &identitySessionPool{
		parent: parent, connect: connect, processCtx: processCtx,
		entries: make(map[string]*identitySessionEntry),
	}
}

func (p *identitySessionPool) openInitial(ctx context.Context) (*sdkmcp.ClientSession, []*sdkmcp.Tool, error) {
	owner := identityctx.IdentityID(ctx)
	if owner == "" {
		return nil, nil, errMissingRemoteIdentity
	}
	entry := &identitySessionEntry{ready: make(chan struct{})}
	p.mu.Lock()
	p.entries[owner] = entry
	p.mu.Unlock()

	child, session, advertised, err := p.open(ctx, owner)
	entry.server, entry.err = child, err
	close(entry.ready)
	if err != nil {
		p.mu.Lock()
		delete(p.entries, owner)
		p.mu.Unlock()
		return nil, nil, err
	}
	return session, advertised, nil
}

func (p *identitySessionPool) server(ctx context.Context) (*MountedServer, error) {
	owner := identityctx.IdentityID(ctx)
	if owner == "" {
		return nil, errMissingRemoteIdentity
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errIdentitySessionPoolClosed
	}
	entry, ok := p.entries[owner]
	if !ok {
		entry = &identitySessionEntry{ready: make(chan struct{})}
		p.entries[owner] = entry
	}
	p.mu.Unlock()

	if !ok {
		handshakeCtx, cancel := context.WithTimeout(ctx, defaultMCPRedialTimeout)
		child, _, _, err := p.open(handshakeCtx, owner)
		cancel()
		entry.server, entry.err = child, err
		close(entry.ready)
		if err != nil {
			p.mu.Lock()
			delete(p.entries, owner)
			p.mu.Unlock()
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-entry.ready:
		return entry.server, entry.err
	}
}

func (p *identitySessionPool) open(ctx context.Context, owner string) (*MountedServer, *sdkmcp.ClientSession, []*sdkmcp.Tool, error) {
	processCtx := identityctx.WithIdentityID(p.processCtx, owner)
	handshakeCtx := identityctx.WithIdentityID(ctx, owner)

	var child *MountedServer
	open := func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.ToolListChanged = child.onToolListChanged
		return p.connect(pctx, hctx, o)
	}
	child = NewMountedServer(p.parent.name, open)
	child.setProcessContext(processCtx)
	p.syncChild(child)

	session, err := open(processCtx, handshakeCtx, mcp.SessionOptions{})
	if err != nil {
		return nil, nil, nil, err
	}
	advertised, err := drainTools(handshakeCtx, session)
	if err != nil {
		mcp.ReleaseSession(session)
		return nil, nil, nil, err
	}
	if err := p.validate(advertised); err != nil {
		_ = session.Close()
		return nil, nil, nil, err
	}
	child.Attach(session)
	child.trackAcceptedTools(advertised)
	return child, session, advertised, nil
}

func (p *identitySessionPool) validate(advertised []*sdkmcp.Tool) error {
	p.parent.mu.Lock()
	defer p.parent.mu.Unlock()
	if err := p.parent.validateToolSetLocked(advertised); err != nil {
		return fmt.Errorf("identity-scoped session: %w", err)
	}
	return nil
}

func (p *identitySessionPool) syncChild(child *MountedServer) {
	p.parent.mu.Lock()
	accepted := make(map[string]struct{}, len(p.parent.acceptedToolNames))
	maps.Copy(accepted, p.parent.acceptedToolNames)
	bridged := make(map[string]*bridgedTool, len(p.parent.bridged))
	maps.Copy(bridged, p.parent.bridged)
	hook := p.parent.refreshHook
	p.parent.mu.Unlock()

	child.mu.Lock()
	child.acceptedToolNames = accepted
	child.bridged = bridged
	child.refreshHook = hook
	child.mu.Unlock()
}

func (p *identitySessionPool) syncChildren() {
	p.mu.Lock()
	children := make([]*MountedServer, 0, len(p.entries))
	for _, entry := range p.entries {
		select {
		case <-entry.ready:
			if entry.server != nil {
				children = append(children, entry.server)
			}
		default:
		}
	}
	p.mu.Unlock()
	for _, child := range children {
		p.syncChild(child)
	}
}

func (p *identitySessionPool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	entries := make([]*identitySessionEntry, 0, len(p.entries))
	for _, entry := range p.entries {
		entries = append(entries, entry)
	}
	p.mu.Unlock()

	var joined error
	for _, entry := range entries {
		<-entry.ready
		if entry.server != nil {
			joined = errors.Join(joined, entry.server.Close())
		}
	}
	return joined
}
