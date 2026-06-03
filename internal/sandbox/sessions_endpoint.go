package sandbox

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ErrSessionEndpointUnknown is the typed lookup-miss for the SessionEndpoint
// resolver: a sessionExec for a conversation whose container was never registered
// (or was already Unregistered by the reaper/recovery) returns it instead of
// silently falling through to the shared stateless sidecar URL — cross-conversation
// routing must be explicit (T-08-10-INFO-XCONV). errors.Is-friendly.
var ErrSessionEndpointUnknown = errors.New("sandbox: session endpoint unknown (container not registered)")

// SessionEndpoint is the 2b session-routing registry: it maps a sessionID
// (= conversation id) to its PER-CONVERSATION container's loopback base URL
// (http://127.0.0.1:NNNNN). SessionManager.create Registers the URL once the
// container's published port is resolved; DockerRunner.sessionExec consults it to
// POST to the conversation's own container (D-05/prd.md:1252 — execution stays HTTP
// toward the per-session container's port, NEVER the docker socket). It is
// concurrency-safe (sync.Map) so the reaper, boot recovery, create, and exec all
// touch it from different goroutines under -race.
type SessionEndpoint struct {
	urls sync.Map // sessionID string -> baseURL string
}

// NewSessionEndpoint builds an empty resolver. Production uses the package-level
// defaultSessionEndpoint; unit tests that must avoid cross-test pollution build a
// private instance.
func NewSessionEndpoint() *SessionEndpoint {
	return &SessionEndpoint{}
}

// defaultSessionEndpoint is THE PROCESS-SCOPED 2b session-routing registry: one
// Aura process has exactly one routing table, shared by every SessionManager and
// every DockerRunner it builds. It is process-scoped, NOT cross-process — a
// separate CLI process (SessionControl terminate/prune) gets its own empty table.
//
// This package-level singleton is the BLOCKER-1 fix: the live tier builds the
// runner (integrationRunner) and the manager (liveManager) as two INDEPENDENT
// objects with no resolver-injection seam, then calls m.Acquire then
// runner.RunPythonSession. Because SessionManager.create Registers into this var
// (when SessionDeps.Endpoint is nil) and NewDockerRunner DEFAULTS DockerRunner's
// resolver to the SAME var, the unmodified runner+manager pair shares routing state
// BY CONSTRUCTION — no test-helper edit required.
var defaultSessionEndpoint = NewSessionEndpoint()

// Register binds sessionID to its per-conversation container base URL. A later
// Register for the same sessionID overwrites (lazy-recreate replaces a stale URL).
func (e *SessionEndpoint) Register(sessionID, baseURL string) {
	e.urls.Store(sessionID, baseURL)
}

// URLFor returns the per-conversation base URL registered for sessionID. The bool
// is false on a miss (the caller maps it to ErrSessionEndpointUnknown).
func (e *SessionEndpoint) URLFor(sessionID string) (string, bool) {
	v, ok := e.urls.Load(sessionID)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// Unregister drops the binding for sessionID (reaper eviction + boot recovery), so
// a subsequent exec for a reaped/recovered conversation gets ErrSessionEndpointUnknown
// until Acquire recreates the container and Registers a fresh URL.
func (e *SessionEndpoint) Unregister(sessionID string) {
	e.urls.Delete(sessionID)
}

// parsePublishedHostPort extracts the host loopback address from `docker port`
// stdout. On a dual-stack host `docker port <id> 18901` emits BOTH an IPv4 line
// (`127.0.0.1:NNNNN`) AND an IPv6 line (`[::]:MMMMM` or `:::MMMMM`) — selecting the
// wrong one yields an unreachable/ambiguous address, so this scans for the line
// whose host is exactly 127.0.0.1 and returns it. Zero 127.0.0.1 lines is an error
// (the loopback publish did not take — FLAG 1).
func parsePublishedHostPort(stdout string) (string, error) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// `docker port` lines look like "0.0.0.0:18901" or "127.0.0.1:49154";
		// some forms include the container mapping ("18901/tcp -> 127.0.0.1:49154").
		if idx := strings.Index(line, "->"); idx >= 0 {
			line = strings.TrimSpace(line[idx+2:])
		}
		if strings.HasPrefix(line, "127.0.0.1:") {
			return line, nil
		}
	}
	return "", fmt.Errorf("docker port: no 127.0.0.1 line in %q", stdout)
}
