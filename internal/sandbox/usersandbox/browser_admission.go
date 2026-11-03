package usersandbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/moby/moby/client"
)

// browserIdleClose is how long agent-browser keeps a Chrome open without a call
// (AGENT_BROWSER_IDLE_TIMEOUT_MS in docker/aura-sandbox/agent-browser.sh). A browser called
// within it counts as open without a look into its box.
const browserIdleClose = 180 * time.Second

// chromeProbe exits 0 while a Chrome runs in the box: agent-browser launches its main process
// with --remote-debugging-port (lab VM, 2026-10-11). The brackets keep grep, and the shell that
// runs it, from matching their own command lines.
const chromeProbe = `grep -qs -- '--remote-debugging-por[t]' /proc/[0-9]*/cmdline`

func browserCapacity(totalMemory, perBox int64) int {
	if perBox <= 0 {
		perBox = 2 << 30
	}
	return max(1, int(totalMemory/2/perBox))
}

func browserResources(r Resources) Resources {
	if r.NanoCPUs <= 0 {
		r.NanoCPUs = 2 * nanoCPUsPerCPU
	}
	if r.MemoryBytes <= 0 {
		r.MemoryBytes = 2 << 30
	}
	if r.PidsLimit <= 0 {
		r.PidsLimit = 1024
	}
	return r
}

// BrowserBoxes lists every identity's browser box, stopped ones included, so the boot start
// finds a box the daemon stopped.
func (b *DockerBackend) BrowserBoxes(ctx context.Context) ([]BoxHandle, error) {
	return b.listBrowserBoxes(ctx, true)
}

// listBrowserBoxes reads the daemon, not a cache, so a box from a prior Aura lifetime counts.
func (b *DockerBackend) listBrowserBoxes(ctx context.Context, all bool) ([]BoxHandle, error) {
	result, err := b.cli.ContainerList(ctx, client.ContainerListOptions{All: all, Filters: make(client.Filters).Add("name", "^/aura-browser-")})
	if err != nil {
		return nil, err
	}
	var boxes []BoxHandle
	for _, item := range result.Items {
		for _, name := range item.Names {
			if id, ok := strings.CutPrefix(name, "/aura-browser-"); ok {
				boxes = append(boxes, BoxHandle{ContainerID: item.ID, IdentityID: id, Kind: Browser})
				break
			}
		}
	}
	return boxes, nil
}

// AdmitBrowser asks the host for room for the caller's Chrome, before a browser call that can
// open one.
func (r *SandboxRouter) AdmitBrowser(ctx context.Context) error {
	b, ok := backendAs[interface {
		AdmitBrowser(context.Context, string) error
	}](r)
	if !ok {
		return fmt.Errorf("sandbox backend does not support browser admission")
	}
	return b.AdmitBrowser(ctx, r.identityID(ctx))
}

// AdmitBrowser lets identityID's browser call through when the host has room for its Chrome.
// A slot is a Chrome, not a box: an always-on box without one holds about 4 MiB (operator,
// 2026-10-15). A browser called within browserIdleClose, or found running in its box, holds a
// slot, so a Chrome left open across an Aura restart still counts.
func (b *DockerBackend) AdmitBrowser(ctx context.Context, identityID string) error {
	b.admission.Lock()
	defer b.admission.Unlock()
	now := time.Now()
	if now.Sub(b.browserCalls[identityID]) < browserIdleClose {
		b.browserCalls[identityID] = now
		return nil
	}
	open, err := b.openBrowsers(ctx, now)
	if err != nil {
		return fmt.Errorf("browser admission inventory: %w", err)
	}
	if !open[identityID] {
		limit, err := b.browserLimit(ctx)
		if err != nil {
			return err
		}
		if len(open) >= limit {
			return fmt.Errorf("browser capacity: the %d browsers this host admits are open for other members; one closes 3 minutes after its last call (AURA_SANDBOX_BROWSER_MAX_ACTIVE)", limit)
		}
	}
	b.browserCalls[identityID] = now
	return nil
}

// openBrowsers names the identities whose Chrome counts as open: called within
// browserIdleClose, or running in a running browser box. A box that cannot be probed counts as
// open, so a failed look never frees a slot.
func (b *DockerBackend) openBrowsers(ctx context.Context, now time.Time) (map[string]bool, error) {
	open := make(map[string]bool)
	for id, at := range b.browserCalls {
		if now.Sub(at) < browserIdleClose {
			open[id] = true
		} else {
			delete(b.browserCalls, id)
		}
	}
	boxes, err := b.listBrowserBoxes(ctx, false)
	if err != nil {
		return nil, err
	}
	for _, h := range boxes {
		if open[h.IdentityID] {
			continue
		}
		res, err := b.Exec(ctx, h, ExecRequest{Command: chromeProbe})
		if err != nil {
			slog.Warn("sandbox: browser admission could not look into a box; its browser counts as open", "container", h.ContainerID, "err", err)
		}
		if err != nil || res.ExitCode == 0 {
			open[h.IdentityID] = true
		}
	}
	// A human's desktop can outlive the idle-call window and is a separate Chrome.
	desktops, err := b.cli.ContainerList(ctx, client.ContainerListOptions{Filters: make(client.Filters).
		Add("label", signInLabel).Add("label", signInRoleLabel+"=desktop")})
	if err != nil {
		return nil, fmt.Errorf("sign-in browser inventory: %w", err)
	}
	for _, desktop := range desktops.Items {
		if session := desktop.Labels[signInLabel]; session != "" {
			open[session] = true
		}
	}
	return open, nil
}

// browserLimit is AURA_SANDBOX_BROWSER_MAX_ACTIVE or, when that is unset, half the host's
// memory over the browser memory cap, read from the daemon once and kept in browserMaxActive.
func (b *DockerBackend) browserLimit(ctx context.Context) (int, error) {
	if b.browserMaxActive > 0 {
		return b.browserMaxActive, nil
	}
	info, err := b.cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return 0, fmt.Errorf("browser admission memory: %w", err)
	}
	perBrowser := browserResources(b.browserLimits).MemoryBytes
	b.browserMaxActive = browserCapacity(info.Info.MemTotal, perBrowser)
	slog.Info("sandbox: derived browser admission limit", "max_active", b.browserMaxActive, "host_memory_bytes", info.Info.MemTotal, "browser_memory_bytes", perBrowser)
	return b.browserMaxActive, nil
}
