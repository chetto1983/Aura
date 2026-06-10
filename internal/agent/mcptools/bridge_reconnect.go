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
}

func newReconnectingServer(name string, cfg mcp.ServerConfig, client reconnectingClient) *reconnectingServer {
	return &reconnectingServer{name: name, cfg: cfg, client: client, bridged: map[string]*bridgedTool{}}
}

func (s *reconnectingServer) ListTools(ctx context.Context) ([]mcp.ToolDef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	defs, err := s.client.ListTools(ctx)
	if !mcp.IsTransportError(err) {
		return defs, err
	}
	defs, err = s.reconnectLocked(ctx)
	if err != nil {
		return nil, err
	}
	return defs, nil
}

func (s *reconnectingServer) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	text, err := s.client.CallTool(ctx, name, args)
	if !mcp.IsTransportError(err) {
		return text, err
	}
	if _, err := s.reconnectLocked(ctx); err != nil {
		return "", err
	}
	return s.client.CallTool(ctx, name, args)
}

func (s *reconnectingServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil
	}
	return s.client.Close()
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
