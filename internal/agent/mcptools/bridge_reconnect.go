package mcptools

import (
	"context"
	"fmt"
	"sync"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

type reconnectingClient interface {
	Server
	Close() error
}

var openMCPClient = func(ctx context.Context, name string, cfg mcp.ServerConfig) (reconnectingClient, error) {
	return mcp.Open(ctx, name, cfg)
}

type reconnectingServer struct {
	mu          sync.Mutex
	name        string
	cfg         mcp.ServerConfig
	client      reconnectingClient
	bridged     map[string]*bridgedTool
	refreshHook func()
	closed      bool
}

func newReconnectingServer(name string, cfg mcp.ServerConfig, client reconnectingClient) *reconnectingServer {
	return &reconnectingServer{name: name, cfg: cfg, client: client, bridged: map[string]*bridgedTool{}}
}

func (s *reconnectingServer) ListTools(ctx context.Context) ([]mcp.ToolDef, error) {
	client, err := s.currentClient()
	if err != nil {
		return nil, err
	}
	defs, err := client.ListTools(ctx)
	if !mcp.IsTransportError(err) {
		return defs, err
	}
	defs, retry, err := s.reconnectAfterTransport(ctx, client)
	if err != nil || defs != nil {
		return defs, err
	}
	return retry.ListTools(ctx)
}

func (s *reconnectingServer) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	client, err := s.currentClient()
	if err != nil {
		return "", err
	}
	text, err := client.CallTool(ctx, name, args)
	if !mcp.IsTransportError(err) {
		return text, err
	}
	_, retry, err := s.reconnectAfterTransport(ctx, client)
	if err != nil {
		return "", err
	}
	return retry.CallTool(ctx, name, args)
}

func (s *reconnectingServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}

func (s *reconnectingServer) trackBridgedTools(bridged []tools.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range bridged {
		if bt, ok := t.(*bridgedTool); ok {
			s.bridged[bt.name] = bt
		}
	}
}

func (s *reconnectingServer) setRefreshHook(hook func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshHook = hook
}

func (s *reconnectingServer) reconnectLocked(ctx context.Context) ([]mcp.ToolDef, error) {
	if s.closed {
		return nil, fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
	}
	if s.client != nil {
		_ = s.client.Close()
	}
	next, err := openMCPClient(ctx, s.name, s.cfg)
	if err != nil {
		return nil, fmt.Errorf("reconnect mcp %q: %w", s.name, err)
	}
	s.client = next
	defs, err := s.client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	s.refreshSpecsLocked(defs)
	return defs, nil
}

func (s *reconnectingServer) refreshSpecsLocked(defs []mcp.ToolDef) {
	changed := false
	for _, d := range defs {
		if bt, ok := s.bridged[d.Name]; ok {
			bt.refreshSpec(d)
			changed = true
		}
	}
	if changed && s.refreshHook != nil {
		s.refreshHook()
	}
}

func (s *reconnectingServer) currentClient() (reconnectingClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.client == nil {
		return nil, fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
	}
	return s.client, nil
}

func (s *reconnectingServer) reconnectAfterTransport(
	ctx context.Context, failed reconnectingClient,
) ([]mcp.ToolDef, reconnectingClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
	}
	if s.client == failed {
		defs, err := s.reconnectLocked(ctx)
		return defs, s.client, err
	}
	if s.client == nil {
		return nil, nil, fmt.Errorf("%w: mcp %q has no client", mcp.ErrTransport, s.name)
	}
	return nil, s.client, nil
}
