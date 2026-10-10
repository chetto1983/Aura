package mcptools

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// maxOpenBrowsers is how many browsers the bridge lets one box hold. Each session is its own
// Chromium: on the lab VM a real site held 137-263 of the box's 1024 pids and up to 868 MiB of
// its 2 GiB, and a fifth browser exhausted the pids, after a model named a new session for
// every question and closed none, though the browser skill it had loaded said to (prd.md §12,
// measured 2026-10-10).
const maxOpenBrowsers = 3

const (
	browserOpenTool     = "agent_browser_open"
	browserCloseTool    = "agent_browser_close"
	browserProfilesTool = "agent_browser_tools_profiles"
)

// browserSessions lists, per identity, the browser sessions the bridge drove in its box, least
// recently used first. agent-browser lists its sessions only in an MCP profile Aura does not
// mount (state), so the list is the bridge's own: a browser the box closed when idle stays on
// it until it is the one closed to make room, and closing it then is a no-op.
type browserSessions struct {
	mu   sync.Mutex
	used map[string][]string
}

// admit puts session last on identity's list and names the browser to close first when the
// box would otherwise hold more than maxOpenBrowsers. A session the operator holds in the live
// view is never the one closed; when they hold every other one, the call is refused.
func (s *browserSessions) admit(ctrl BrowserControl, identity, session string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used == nil {
		s.used = map[string][]string{}
	}
	list := s.used[identity]
	if i := slices.Index(list, session); i >= 0 {
		s.used[identity] = append(slices.Delete(list, i, i+1), session)
		return "", nil
	}
	evict := ""
	if len(list) >= maxOpenBrowsers {
		i := slices.IndexFunc(list, func(open string) bool { return ctrl == nil || !ctrl.Held(identity, open) })
		if i < 0 {
			return "", fmt.Errorf("your sandbox has %d browsers open and the operator holds each of them in the live view (%s): "+
				"wait for them to let one go", len(list), strings.Join(list, ", "))
		}
		evict = list[i]
		list = slices.Delete(list, i, i+1)
	}
	s.used[identity] = append(list, session)
	return evict, nil
}

// forget drops what a close call ended: its session, or every session with "all".
func (s *browserSessions) forget(identity string, args map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if all, _ := args["all"].(bool); all {
		delete(s.used, identity)
		return
	}
	session, _ := args["session"].(string)
	if list, ok := s.used[identity]; ok {
		s.used[identity] = slices.DeleteFunc(list, func(open string) bool { return open == session })
	}
}

// makeBrowserRoom admits the call's session to its box, closing the least recently used browser
// first when the box is full. The returned line tells the model which one was closed.
func (b *bridgedTool) makeBrowserRoom(ctx context.Context, args map[string]any) (string, error) {
	if b.name == browserCloseTool || b.name == browserProfilesTool {
		return "", nil
	}
	session, _ := args["session"].(string)
	evict, err := b.srv.browsers.admit(b.srv.browser, browserIdentity(ctx), session)
	if err != nil || evict == "" {
		return "", err
	}
	closeArgs, err := withBrowserProfile(map[string]any{"session": evict})
	if err != nil {
		return "", err
	}
	if _, err := b.srv.CallTool(ctx, browserCloseTool, closeArgs); err != nil {
		slog.Warn("mcp browser: closing the least recently used session failed", "session", evict, "err", err)
	}
	return fmt.Sprintf("\n\nClosed browser session %q to make room: Aura keeps %d open in your sandbox, and its profile keeps the login.",
		evict, maxOpenBrowsers), nil
}

// browserCallDone forgets what a close ended and returns the line a successful open ends with.
func (b *bridgedTool) browserCallDone(ctx context.Context, args map[string]any) string {
	switch b.name {
	case browserCloseTool:
		b.srv.browsers.forget(browserIdentity(ctx), args)
	case browserOpenTool:
		session, _ := args["session"].(string)
		return fmt.Sprintf("\n\nSession %q is open. Close it with %s when the site is done.", session, browserCloseName(b.Spec().Name))
	}
	return ""
}

// browserOpenRule is what agent_browser_open's description gains on the browser recipe, where
// the model reads it as it picks a session.
func browserOpenRule(policy bridgePolicy, tool, name string) string {
	if policy.recipeSource != mcpmanager.BrowserRecipeSource || tool != browserOpenTool {
		return ""
	}
	return fmt.Sprintf("\n\nEach session is its own Chromium in your sandbox, about 200 processes and up to 900 MB. "+
		"Aura keeps %d open, and a session past them closes the one used least recently. Reuse the site's session "+
		"name, and call %s with it when the site is done: its profile keeps the login for the next open.",
		maxOpenBrowsers, browserCloseName(name))
}

// browserCloseName is the close tool's model-facing name, in the namespace open is mounted under.
func browserCloseName(openName string) string {
	return strings.TrimSuffix(openName, browserOpenTool) + browserCloseTool
}
