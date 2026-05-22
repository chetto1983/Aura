package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/telegram"
)

const (
	mcpSupervisorPollInterval  = 2 * time.Second
	mcpReconnectInitialBackoff = 500 * time.Millisecond
	mcpReconnectMaxBackoff     = 30 * time.Second
	mcpCircuitFailureThreshold = 3
)

type mcpServerRuntime struct {
	name     string
	cfg      mcp.ServerConfig
	registry *tools.Registry
	logger   *slog.Logger

	mu                 sync.RWMutex
	syncMu             sync.Mutex
	client             *mcp.Client
	registered         map[string]struct{}
	overrides          map[string]string
	overrideWarns      []string
	consecutiveFailure int
	circuitOpen        bool
	nextReconnect      time.Time
	reconnectBackoff   time.Duration
}

func registerMCPServers(cfg *config.Config, deps *telegram.Deps, toolRegistry *tools.Registry, logger *slog.Logger) (string, []*mcpServerRuntime) {
	overridePath := mcp.ToolOverridesPath(cfg.RuntimeWorkspacePath, cfg.MCPServersPath)
	mcpServers, mcpErr := mcp.LoadServers(cfg.MCPServersPath)
	if mcpErr != nil {
		logger.Warn("MCP config load failed; continuing without MCP", "error", mcpErr, "path", cfg.MCPServersPath)
	}

	names := make([]string, 0, len(mcpServers))
	for name := range mcpServers {
		names = append(names, name)
	}
	sort.Strings(names)

	mcpClients := make([]mcp.ConnectedClient, 0, len(mcpServers))
	runtimes := make([]*mcpServerRuntime, 0, len(mcpServers))
	for _, name := range names {
		srv := mcpServers[name]
		srv.Env = mcp.NormalizeRuntimeEnv(name, srv.Env)
		mcpServers[name] = srv

		rt := newMCPServerRuntime(name, srv, toolRegistry, logger)
		if err := rt.reconnect(context.Background()); err != nil {
			logger.Warn("MCP server unavailable; supervisor will retry", "server", name, "error", err)
			rt.markCircuitOpen()
		}
		mcpClients = append(mcpClients, rt)
		runtimes = append(runtimes, rt)
	}

	reloadMCPToolOverrides(mcpClients, overridePath, logger, "loaded")
	for _, rt := range runtimes {
		registeredTools := rt.syncRegisteredTools()
		logger.Info("MCP server registered", "server", rt.Name(), "tools", len(rt.Tools()), "aura_tools", registeredTools)
	}
	deps.MCPClients = mcpClients
	return overridePath, runtimes
}

func newMCPServerRuntime(name string, cfg mcp.ServerConfig, registry *tools.Registry, logger *slog.Logger) *mcpServerRuntime {
	if logger == nil {
		logger = slog.Default()
	}
	return &mcpServerRuntime{
		name:             name,
		cfg:              cfg,
		registry:         registry,
		logger:           logger,
		registered:       map[string]struct{}{},
		reconnectBackoff: mcpReconnectInitialBackoff,
	}
}

func (rt *mcpServerRuntime) Name() string { return rt.name }

func (rt *mcpServerRuntime) Transport() string {
	rt.mu.RLock()
	client := rt.client
	rt.mu.RUnlock()
	if client != nil {
		return client.Transport()
	}
	if strings.TrimSpace(rt.cfg.Command) != "" {
		return mcp.TransportStdio
	}
	if strings.TrimSpace(rt.cfg.URL) != "" {
		return mcp.TransportHTTP
	}
	return ""
}

func (rt *mcpServerRuntime) Tools() []mcp.Tool {
	rt.mu.RLock()
	if rt.circuitOpen || rt.client == nil {
		rt.mu.RUnlock()
		return nil
	}
	client := rt.client
	rt.mu.RUnlock()
	return client.Tools()
}

func (rt *mcpServerRuntime) CallTool(ctx context.Context, toolName string, arguments map[string]any) (string, error) {
	client, err := rt.activeClient()
	if err != nil {
		return "", err
	}
	out, err := client.CallTool(ctx, toolName, arguments)
	if err != nil {
		rt.recordFailure("tools/call", err)
		return "", err
	}
	rt.recordSuccess()
	return out, nil
}

func (rt *mcpServerRuntime) Close() error {
	rt.mu.Lock()
	client := rt.client
	rt.client = nil
	rt.circuitOpen = true
	rt.mu.Unlock()
	if client != nil {
		return client.Close()
	}
	return nil
}

func (rt *mcpServerRuntime) OverrideWarnings() []string {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return append([]string(nil), rt.overrideWarns...)
}

func (rt *mcpServerRuntime) SetOverrideWarnings(warnings []string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.overrideWarns = append([]string(nil), warnings...)
}

func (rt *mcpServerRuntime) ApplyToolDescriptionOverrides(overrides map[string]string) []string {
	rt.mu.Lock()
	rt.overrides = cloneMCPStringMap(overrides)
	client := rt.client
	rt.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.ApplyToolDescriptionOverrides(overrides)
}

func (rt *mcpServerRuntime) activeClient() (*mcp.Client, error) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	if rt.circuitOpen {
		return nil, fmt.Errorf("mcp %s: circuit open", rt.name)
	}
	if rt.client == nil {
		return nil, fmt.Errorf("mcp %s: client unavailable", rt.name)
	}
	return rt.client, nil
}

func (rt *mcpServerRuntime) refreshAndSync(ctx context.Context) error {
	client, err := rt.activeClient()
	if err != nil {
		return err
	}
	if err := client.RefreshTools(ctx); err != nil {
		rt.recordFailure("tools/list", err)
		return err
	}
	rt.recordSuccess()
	rt.syncRegisteredTools()
	return nil
}

func (rt *mcpServerRuntime) reconnect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := connectMCPClient(rt.name, rt.cfg)
	if err != nil {
		rt.scheduleReconnect()
		return err
	}

	rt.mu.RLock()
	overrides := cloneMCPStringMap(rt.overrides)
	rt.mu.RUnlock()
	if len(overrides) > 0 {
		client.ApplyToolDescriptionOverrides(overrides)
	}

	rt.mu.Lock()
	old := rt.client
	rt.client = client
	rt.circuitOpen = false
	rt.consecutiveFailure = 0
	rt.reconnectBackoff = mcpReconnectInitialBackoff
	rt.nextReconnect = time.Time{}
	rt.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	rt.syncRegisteredTools()
	rt.logger.Info("MCP server connected", "server", rt.name, "tools", len(rt.Tools()))
	return nil
}

func (rt *mcpServerRuntime) run(ctx context.Context) {
	ticker := time.NewTicker(mcpSupervisorPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if rt.needsReconnect() {
				rt.tryReconnect(ctx)
				continue
			}
			if err := rt.refreshAndSync(ctx); err != nil {
				rt.logger.Warn("MCP tool list refresh failed", "server", rt.name, "error", err)
			}
		}
	}
}

func (rt *mcpServerRuntime) needsReconnect() bool {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.client == nil || rt.circuitOpen
}

func (rt *mcpServerRuntime) tryReconnect(ctx context.Context) {
	rt.mu.RLock()
	next := rt.nextReconnect
	rt.mu.RUnlock()
	if !next.IsZero() && time.Now().Before(next) {
		return
	}
	if err := rt.reconnect(ctx); err != nil {
		rt.logger.Warn("MCP reconnect failed", "server", rt.name, "error", err)
	}
}

func (rt *mcpServerRuntime) recordSuccess() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.consecutiveFailure = 0
}

func (rt *mcpServerRuntime) recordFailure(operation string, err error) {
	rt.mu.Lock()
	rt.consecutiveFailure++
	failures := rt.consecutiveFailure
	opened := failures >= mcpCircuitFailureThreshold && !rt.circuitOpen
	if opened {
		rt.circuitOpen = true
		rt.nextReconnect = time.Now().Add(rt.reconnectBackoff)
	}
	rt.mu.Unlock()

	if opened {
		rt.logger.Warn("MCP circuit opened", "server", rt.name, "operation", operation, "failures", failures, "error", err)
		rt.syncRegisteredTools()
	}
}

func (rt *mcpServerRuntime) markCircuitOpen() {
	rt.mu.Lock()
	rt.circuitOpen = true
	rt.nextReconnect = time.Now().Add(rt.reconnectBackoff)
	rt.mu.Unlock()
}

func (rt *mcpServerRuntime) scheduleReconnect() {
	rt.mu.Lock()
	backoff := rt.reconnectBackoff
	if backoff <= 0 {
		backoff = mcpReconnectInitialBackoff
	}
	rt.circuitOpen = true
	rt.nextReconnect = time.Now().Add(backoff)
	nextBackoff := backoff * 2
	if nextBackoff > mcpReconnectMaxBackoff {
		nextBackoff = mcpReconnectMaxBackoff
	}
	rt.reconnectBackoff = nextBackoff
	rt.mu.Unlock()
}

func (rt *mcpServerRuntime) syncRegisteredTools() int {
	if rt.registry == nil {
		return 0
	}
	rt.syncMu.Lock()
	defer rt.syncMu.Unlock()

	current := rt.Tools()
	desiredTools := make(map[string]tools.Tool, len(current))
	desiredNames := make(map[string]struct{}, len(current))
	for _, t := range current {
		if !mcp.ToolEnabledForAura(rt.name, rt.cfg.Env, t.Name) {
			continue
		}
		tool := tools.NewMCPTool(rt, rt.name, t)
		var registered tools.Tool = tool
		if mcp.ToolAutonomousForAura(rt.name, t.Name) {
			registered = tools.WithCategory(tool, tools.CategoryAutonomous)
		}
		name := registered.Name()
		desiredTools[name] = registered
		desiredNames[name] = struct{}{}
	}

	rt.mu.RLock()
	previous := make(map[string]struct{}, len(rt.registered))
	for name := range rt.registered {
		previous[name] = struct{}{}
	}
	rt.mu.RUnlock()

	for name, tool := range desiredTools {
		if _, ok := previous[name]; !ok {
			rt.registry.Register(tool)
		}
	}
	for name := range previous {
		if _, ok := desiredNames[name]; !ok {
			rt.registry.Unregister(name)
		}
	}

	rt.mu.Lock()
	rt.registered = desiredNames
	rt.mu.Unlock()
	return len(desiredNames)
}

func connectMCPClient(name string, srv mcp.ServerConfig) (*mcp.Client, error) {
	switch {
	case strings.TrimSpace(srv.Command) != "":
		return mcp.NewStdioClient(name, srv.Command, srv.Args, srv.Env)
	case strings.TrimSpace(srv.URL) != "":
		return mcp.NewHTTPClient(name, srv.URL, srv.Headers)
	default:
		return nil, fmt.Errorf("mcp %s: missing transport", name)
	}
}

func (a *App) startMCPSupervisors() {
	if a == nil || a.mcpSupervisorRun || len(a.mcpRuntimes) == 0 {
		return
	}
	a.mcpSupervisorRun = true
	for _, rt := range a.mcpRuntimes {
		runtime := rt
		a.startBg(func(ctx context.Context) {
			runtime.run(ctx)
		})
	}
}

func (a *App) startMCPOverrideWatcher() {
	if a == nil || a.mcpOverrideWatch || strings.TrimSpace(a.mcpOverridePath) == "" || len(a.deps.MCPClients) == 0 {
		return
	}
	a.mcpOverrideWatch = true

	if err := os.MkdirAll(filepath.Dir(a.mcpOverridePath), 0o755); err != nil {
		a.deps.Logger.Warn("MCP tool override watcher disabled", "error", err, "path", a.mcpOverridePath)
		return
	}
	watcher, err := mcp.New(mcp.Config{
		Path:     a.mcpOverridePath,
		Debounce: 500 * time.Millisecond,
		Callback: func() {
			reloadMCPToolOverrides(a.deps.MCPClients, a.mcpOverridePath, a.deps.Logger, "reloaded")
		},
		Logger: a.deps.Logger,
	})
	if err != nil {
		a.deps.Logger.Warn("MCP tool override watcher disabled", "error", err, "path", a.mcpOverridePath)
		return
	}
	a.startBg(func(ctx context.Context) {
		if err := watcher.Run(ctx); err != nil {
			a.deps.Logger.Warn("MCP tool override watcher stopped", "error", err, "path", watcher.Path())
		}
	})
	a.deps.Logger.Info("MCP tool override watcher started", "path", watcher.Path())
}

func reloadMCPToolOverrides(clients []mcp.ConnectedClient, path string, logger *slog.Logger, event string) {
	overrides, err := mcp.LoadToolDescriptionOverrides(path)
	if err != nil {
		logger.Warn("MCP tool override load failed; keeping previous tool descriptions", "error", err, "path", path)
		return
	}
	unmatched := mcp.ApplyToolDescriptionOverrides(clients, overrides)
	if len(unmatched) > 0 {
		logger.Warn("MCP tool override keys did not match any connected tool", "path", path, "keys", unmatched)
	}
	logger.Info("MCP tool description overrides "+event, "path", path, "overrides", len(overrides), "unmatched", len(unmatched))
}

func cloneMCPStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
