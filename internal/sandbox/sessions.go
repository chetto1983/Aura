package sandbox

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// containerNamePrefix is the docker --name prefix for per-conversation session
// containers. Boot recovery's stray sweep (sessions_recovery.go) filters on it.
const containerNamePrefix = "aura-sandbox-sess-"

// sessionSidecarPort is the in-container port the session sidecar binds (0.0.0.0:18901).
// runArgv publishes it on an ephemeral host loopback port (127.0.0.1:0:18901); create
// resolves the assigned host port via dockerClient.port and registers the per-conv URL.
const sessionSidecarPort = "18901"

// dockerClient is the lifecycle carve-out (D-05): the SessionManager shells
// docker run/stop/rm for CONTAINER LIFECYCLE only; execution stays HTTP (08-07).
// It is an interface so unit tests inject a fake — the real impl (dockerCLI) is
// LookPath-gated, fixed-argv, NEVER touches /var/run/docker.sock.
type dockerClient interface {
	run(ctx context.Context, name string, argv []string) (string, error)
	stop(ctx context.Context, id string) error
	remove(ctx context.Context, id string) error
	listStray(ctx context.Context) ([]string, error)
	// port resolves the host loopback address a container's published containerPort
	// maps to (the 5th lifecycle verb). It shells `docker port <id> <containerPort>`
	// — LookPath-gated fixed argv, parses stdout ONLY, NEVER the docker socket
	// (T-08-10-SOCKET, extends T-05-03-EOP-SOCKET/D-05).
	port(ctx context.Context, id, containerPort string) (string, error)
}

// sessionStore is the narrow control-plane registry surface the SessionManager
// needs (the 4 sqlc queries from 08-02). *sqlc.Queries satisfies it; unit tests
// inject an in-memory fake so the cap/reaper/lock invariants run without Postgres.
type sessionStore interface {
	InsertSession(ctx context.Context, arg sqlc.InsertSessionParams) (sqlc.AuraSandboxSessions, error)
	TouchLastUsed(ctx context.Context, id pgtype.UUID) error
	MarkTerminated(ctx context.Context, id pgtype.UUID) error
	ListActive(ctx context.Context) ([]sqlc.AuraSandboxSessions, error)
}

// session is one live per-conversation container. mu serializes execs within the
// session (D-07); lastUsed is unix-nanos read by the reaper without holding mu.
type session struct {
	mu          sync.Mutex
	id          pgtype.UUID // sandbox_sessions.id (for MarkTerminated)
	convID      string
	containerID string
	lastUsed    atomic.Int64
}

// Session is the public handle returned by Acquire. The caller MUST Release it so
// the next exec on the same conversation can proceed (the per-session lock is held
// between Acquire and Release — D-07).
type Session struct {
	s           *session
	ContainerID string
	ConvID      string
}

// SessionDeps carries the SessionManager's collaborators + tuning. Docker/Store are
// interfaces so unit tests inject fakes. ReapEvery defaults to 60s; the synctest
// TTL test sets it explicitly to drive virtual-clock ticks.
type SessionDeps struct {
	Docker         dockerClient
	Store          sessionStore
	MaxN           int
	TTL            time.Duration
	Runtime        string
	Image          string
	SeccompProfile string
	PrivacyMode    string
	AllowHosts     string
	// EgressNetwork is the docker bridge the session container attaches to when an
	// allowlist is configured — the bridge whose gateway IP hosts the forward proxy
	// (landmine #3). Empty keeps the default (egressless) network posture.
	EgressNetwork string
	// ProxyEnv is the HTTP(S)_PROXY env (e.g. "HTTP_PROXY=http://<gw-ip>:<port>")
	// injected into the session container ONLY when an allowlist is configured, so
	// user code routes egress through the host proxy. Empty for the egressless case.
	ProxyEnv  []string
	ReapEvery time.Duration
	Now       func() time.Time
	// Endpoint is the 2b session-routing registry create Registers each per-conv
	// container URL into and the reaper/recovery Unregister from. Nil-tolerant: a nil
	// Endpoint falls back to the process-scoped shared defaultSessionEndpoint, so a
	// SessionManager built without an explicit resolver still routes exec through the
	// SAME table a bare NewDockerRunner reads (the BLOCKER-1 by-construction sharing).
	Endpoint *SessionEndpoint
}

// SessionManager is the per-conversation container control plane (D-04). It owns
// container lifecycle via dockerClient (NEVER the socket, D-05), a hard concurrency
// cap (D-12), a per-session lock (D-07), an idle-TTL reaper (D-03), and boot
// recovery (D-06, sessions_recovery.go).
type SessionManager struct {
	sessions sync.Map // convID string -> *session
	capMu    sync.Mutex
	count    int

	docker dockerClient
	store  sessionStore

	maxN           int
	ttl            time.Duration
	runtime        string
	image          string
	seccompProfile string
	privacyMode    string
	allowHosts     string
	egressNetwork  string
	proxyEnv       []string
	now            func() time.Time
	endpointReg    *SessionEndpoint

	cancel  context.CancelFunc
	done    chan struct{}
	started bool
}

const defaultReapEvery = 60 * time.Second

// NewSessionManager builds the manager and (unless deps disable it) starts the
// reaper bound to ctx. Close cancels the reaper ctx and waits on done — goleak-
// clean (RESEARCH Anti-Patterns "letting the reaper leak").
func NewSessionManager(ctx context.Context, deps SessionDeps) *SessionManager {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	reapEvery := deps.ReapEvery
	if reapEvery <= 0 {
		reapEvery = defaultReapEvery
	}
	rctx, cancel := context.WithCancel(ctx)
	m := &SessionManager{
		docker:         deps.Docker,
		store:          deps.Store,
		maxN:           deps.MaxN,
		ttl:            deps.TTL,
		runtime:        deps.Runtime,
		image:          deps.Image,
		seccompProfile: deps.SeccompProfile,
		privacyMode:    deps.PrivacyMode,
		allowHosts:     deps.AllowHosts,
		egressNetwork:  deps.EgressNetwork,
		proxyEnv:       deps.ProxyEnv,
		now:            now,
		endpointReg:    deps.Endpoint,
		cancel:         cancel,
		done:           make(chan struct{}),
	}
	// The reaper always runs (D-03): it is the only idle-eviction path. It is bound
	// to rctx and Close waits on done, so it is goleak-clean. The synctest TTL test
	// sets ReapEvery explicitly to drive virtual-clock ticks deterministically.
	m.started = true
	go m.reapLoop(rctx, reapEvery)
	return m
}

// Close stops the reaper and blocks until it has exited (goleak-clean). It is
// idempotent.
func (m *SessionManager) Close() {
	m.cancel()
	if m.started {
		<-m.done
		m.started = false
	}
}

// Acquire returns the session for sessionID (the conversation id string), creating
// the container on first use. It enforces the privacy-mode fail-fast (D-10), the
// hard cap (D-12, no LRU), then LOCKS the session's mutex so concurrent execs on
// the same conversation serialize (D-07). The caller MUST Release.
func (m *SessionManager) Acquire(ctx context.Context, sessionID string) (*Session, error) {
	if m.privacyMode == "local-only" && m.allowHosts != "" {
		return nil, fmt.Errorf("session %s: %w", sessionID, ErrPrivacyModeEgressDenied)
	}
	convUUID, err := parseSessionUUID(sessionID)
	if err != nil {
		return nil, err
	}

	if existing, ok := m.sessions.Load(sessionID); ok {
		s := existing.(*session)
		s.mu.Lock()
		s.lastUsed.Store(m.now().UnixNano())
		_ = m.store.TouchLastUsed(ctx, s.id)
		return &Session{s: s, ContainerID: s.containerID, ConvID: sessionID}, nil
	}

	s, err := m.create(ctx, sessionID, convUUID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.lastUsed.Store(m.now().UnixNano())
	_ = m.store.TouchLastUsed(ctx, s.id)
	return &Session{s: s, ContainerID: s.containerID, ConvID: sessionID}, nil
}

// create starts a new container under the cap. The cap check + count increment +
// map store happen under capMu so two racing Acquires for distinct conversations
// cannot both slip past a count of maxN-1.
func (m *SessionManager) create(ctx context.Context, sessionID string, convUUID pgtype.UUID) (*session, error) {
	m.capMu.Lock()
	defer m.capMu.Unlock()

	// Re-check: a concurrent Acquire for the SAME conv may have created it while we
	// waited on capMu. LoadOrStore semantics without double-spending the cap.
	if existing, ok := m.sessions.Load(sessionID); ok {
		return existing.(*session), nil
	}
	if m.count >= m.maxN {
		return nil, fmt.Errorf("session %s: %w", sessionID, ErrSessionCapReached)
	}

	name := containerNamePrefix + sessionID
	containerID, err := m.docker.run(ctx, name, m.runArgv(name))
	if err != nil {
		return nil, fmt.Errorf("session %s: docker run: %w", sessionID, err)
	}
	row, err := m.store.InsertSession(ctx, sqlc.InsertSessionParams{
		ConversationID: convUUID,
		ContainerID:    containerID,
		ImageDigest:    m.image,
	})
	if err != nil {
		_ = m.docker.stop(ctx, containerID)
		_ = m.docker.remove(ctx, containerID)
		return nil, fmt.Errorf("session %s: register: %w", sessionID, err)
	}
	// Resolve the published loopback host port and register the per-conv exec URL so
	// DockerRunner.sessionExec routes to THIS container (D-05 HTTP-to-per-conv-port).
	// A port-resolve or register failure must NOT leave a half-registered session:
	// tear the container down and surface the error.
	hostAddr, err := m.docker.port(ctx, containerID, sessionSidecarPort)
	if err != nil {
		_ = m.docker.stop(ctx, containerID)
		_ = m.docker.remove(ctx, containerID)
		return nil, fmt.Errorf("session %s: resolve published port: %w", sessionID, err)
	}
	m.endpoint().Register(sessionID, "http://"+hostAddr)
	s := &session{id: row.ID, convID: sessionID, containerID: containerID}
	m.sessions.Store(sessionID, s)
	m.count++
	return s, nil
}

// runArgv builds the docker run argv replicating ALL 2a hardening flags (Pitfall 6:
// gVisor + the security flags are NOT inherited by an ad-hoc docker run, they must
// be passed explicitly). It NEVER references /var/run/docker.sock (D-05).
//
// When an egress allowlist is configured (m.allowHosts != ""), the composition root
// supplies the connect-allowing session seccomp profile plus the egress bridge +
// HTTP(S)_PROXY env, so user code reaches the host forward proxy at the bridge
// gateway IP (landmine #3, extends AR-05-01); an empty allowlist keeps the 2a
// egressless posture (the egressless profile + default network, connect dead-ends).
func (m *SessionManager) runArgv(name string) []string {
	seccomp := m.seccompProfile
	if seccomp == "" {
		seccomp = "unconfined" // overridden by 08-07/08-08 compose-shipped session profile
	}
	argv := []string{
		"run", "-d",
		"--name", name,
		"--runtime=" + m.runtime,
		"--user", "65532:65532",
		"--read-only",
		"--tmpfs", "/tmp",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--security-opt", "seccomp=" + seccomp,
		"--pids-limit", "64",
		"--memory", "512m",
		"--cpus", "1.0",
		"--ulimit", "nofile=64",
		// Publish the session sidecar port on an ephemeral HOST LOOPBACK port. The
		// 127.0.0.1 bind keeps it host-only (reachable from the runner over loopback,
		// NEVER from inside another container netns — T-08-10-EOP-PORT); create
		// resolves the assigned port via `docker port` and registers the per-conv URL.
		"--publish", "127.0.0.1:0:" + sessionSidecarPort,
	}
	if m.allowHosts != "" {
		if m.egressNetwork != "" {
			argv = append(argv, "--network", m.egressNetwork)
		}
		for _, env := range m.proxyEnv {
			argv = append(argv, "--env", env)
		}
	}
	argv = append(argv, m.image)
	return argv
}

// Release unlocks the per-session lock (D-07). Calling Release on a zero Session is
// a no-op so an early-return Acquire error path is safe.
func (m *SessionManager) Release(sess *Session) {
	if sess == nil || sess.s == nil {
		return
	}
	sess.s.mu.Unlock()
}

// endpoint returns the injected session-routing registry, or the process-scoped
// shared defaultSessionEndpoint when none was injected — so create/evict/recovery
// touch the SAME table a bare DockerRunner reads (BLOCKER-1 by-construction sharing).
func (m *SessionManager) endpoint() *SessionEndpoint {
	if m.endpointReg != nil {
		return m.endpointReg
	}
	return defaultSessionEndpoint
}

// liveCount returns the current live session count (capMu-guarded). Test-only
// observability for the reaper eviction assertion.
func (m *SessionManager) liveCount() int {
	m.capMu.Lock()
	defer m.capMu.Unlock()
	return m.count
}

// parseSessionUUID converts the conversation id string to the pgtype.UUID the
// registry expects (landmine #1 — string arrives at the boundary, mirrors
// conversations.parseUUID).
func parseSessionUUID(sessionID string) (pgtype.UUID, error) {
	u, err := uuid.Parse(sessionID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid session_id %q: %w", sessionID, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}
