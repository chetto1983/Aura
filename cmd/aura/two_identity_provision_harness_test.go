//go:build db_integration && garage_integration && authula_integration && musr_e2e

// Harness for TestIdentityCreateProvisionsEveryPlane (two_identity_provision_e2e_test.go,
// D-09/E2E-02): a fake TelegramMint (D-08) plus one helper that composes the REAL
// agui.OnboardingService — the SAME production adapters serve.go wires (Authula, the aura
// leg, recovery, ArcadeDB memory, Garage/filesystem resource legs, and the new sandbox
// leg) — over the live pool, with ONLY the Telegram port replaced by the fake. The saga
// itself runs UNMODIFIED: every leg, real compensation, no CI secret, no live bot, no
// third-party network in this gate. The real bot is used exactly once, in the
// phase-closing scored live run (D-08).
package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/webauth"
)

// musrProvisionFakeBotUsername is the D-08 stub bot name the fake TelegramMint's deep
// link is minted against.
const musrProvisionFakeBotUsername = "musr_e2e_stub_bot"

// fakeTelegramPending is one in-memory pending onboarding token (D-08).
type fakeTelegramPending struct {
	identityID string
	expiresAt  time.Time
}

// fakeTelegramMint is the D-08 in-memory agui.TelegramMint: InsertPending/DeletePending/
// PendingConsumed backed by a map instead of the real telegram.Store, so Leg C and its
// compensation run against it with no live bot and no third-party network.
type fakeTelegramMint struct {
	mu      sync.Mutex
	pending map[string]fakeTelegramPending
}

func newFakeTelegramMint() *fakeTelegramMint {
	return &fakeTelegramMint{pending: map[string]fakeTelegramPending{}}
}

func (f *fakeTelegramMint) InsertPending(_ context.Context, token, identityID string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending[token] = fakeTelegramPending{identityID: identityID, expiresAt: expiresAt}
	return nil
}

func (f *fakeTelegramMint) DeletePending(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pending, token)
	return nil
}

func (f *fakeTelegramMint) PendingConsumed(_ context.Context, token string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.pending[token]; !ok {
		return false, nil
	}
	// No real Telegram scan happens in this gate; a minted-but-unconsumed token is the
	// expected state here. Consumption is exercised by the phase-closing live run (D-08).
	return false, nil
}

// musrProvisionTestEnv bundles what TestIdentityCreateProvisionsEveryPlane needs from the
// composition beyond the service itself: the config (for ArcadeDB/Garage/filesystem
// assertions), the sandbox router the sandbox leg provisioned through, and the SAME
// resource ports the saga used, so the test's own cleanup can de-provision every plane it
// caused to be provisioned (CLAUDE.md prohibition: no test identity may survive in the
// operator's live deployment data).
type musrProvisionTestEnv struct {
	cfg               *config.Config
	sandboxRouter     *usersandbox.SandboxRouter
	telegram          *fakeTelegramMint
	memory            agui.MemoryPurger
	objectStore       agui.ObjectStoreProvisioner
	filesystem        agui.FilesystemProvisioner
	deleteAuthulaUser func(ctx context.Context, authulaUserID string) error
}

// musrBuildProvisionService composes the onboarding service exactly like
// cmd/aura/serve_onboarding.go's buildOnboardingService, reusing the SAME production
// adapter types (authulaCoreAdapter, auraLegAdapter, recoverySetupAdapter,
// buildProvisioningPorts, buildArcadeMemoryProvisioner, sandboxProvisionerFor,
// buildSandboxRouter) — the only substitution is the D-08 fake TelegramMint, wired here
// at the test's own composition root, never inside production code.
func musrBuildProvisionService(t *testing.T, pool *pgxpool.Pool) (agui.OnboardingService, *musrProvisionTestEnv) {
	t.Helper()

	cfg := config.LoadDB()
	if strings.TrimSpace(cfg.GarageAdminEndpoint) == "" || strings.TrimSpace(cfg.GarageAdminToken) == "" {
		t.Skip("musr_e2e requires AURA_GARAGE_ADMIN_ENDPOINT and AURA_GARAGE_ADMIN_TOKEN")
	}

	sandboxRouter := buildSandboxRouter(cfg, pool)

	dsn := strings.TrimSpace(cfg.AuthulaDatabaseURL)
	if dsn == "" {
		dsn = cfg.DB.URL
	}
	authulaProvider, err := webauth.New(webauth.Config{
		DSN:            dsn,
		Secret:         musrEnvOrSkip(t, "AURA_AUTHULA_SECRET"),
		TrustedOrigins: []string{"http://127.0.0.1:9080"},
	})
	if err != nil {
		t.Fatalf("build authula provider: %v", err)
	}
	t.Cleanup(func() { _ = authulaProvider.Close() })
	core := authulaProvider.CoreServices()
	if core == nil {
		t.Fatal("authula provider has no CoreServices")
	}

	memory := buildArcadeMemoryProvisioner(cfg)
	if memory == nil {
		t.Skip("musr_e2e requires ARCADEDB_ADMIN_USER/ARCADEDB_ADMIN_PASSWORD and AURA_ARCADEDB_TENANT_SECRET")
	}

	chat := &chatEnv{cfg: cfg, pool: pool, identity: identity.New(pool), sandboxRouter: sandboxRouter}
	objProv, fsProv, jrnl := buildProvisioningPorts(chat)
	if objProv == nil || fsProv == nil {
		t.Skip("musr_e2e requires the Garage admin client and object-store resolver to compose")
	}

	telegram := newFakeTelegramMint()

	deps := agui.OnboardingDeps{
		Capabilities:  chat.identity,
		Profiles:      onboarding.NewProfileStore(pool),
		Authula:       authulaCoreAdapter{core: core},
		AuraLeg:       auraLegAdapter{pool: pool},
		Telegram:      telegram,
		BotUsername:   musrProvisionFakeBotUsername,
		Recovery:      recoverySetupAdapter{pool: pool},
		Memory:        memory,
		ObjectStore:   objProv,
		Filesystem:    fsProv,
		Journal:       jrnl,
		Sandbox:       sandboxProvisionerFor(sandboxRouter),
		MUSRIsolation: true,
	}
	return agui.NewOnboardingService(deps), &musrProvisionTestEnv{
		cfg:               cfg,
		sandboxRouter:     sandboxRouter,
		telegram:          telegram,
		memory:            memory,
		objectStore:       objProv,
		filesystem:        fsProv,
		deleteAuthulaUser: core.UserService.Delete,
	}
}
