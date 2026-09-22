package remotetunnel

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	cf "github.com/chetto1983/aura/internal/cloudflareapi"
)

type memoryState struct {
	state       State
	failAdvance error
	advances    int
	loadError   error
}

func (m *memoryState) Load(context.Context) (State, error) { return m.state, m.loadError }
func (m *memoryState) SaveDesired(_ context.Context, g int64, d Desired, by string) (State, error) {
	if g != m.state.Generation {
		return State{}, ErrStaleGeneration
	}
	m.state.Desired = d
	m.state.Generation++
	m.state.UpdatedBy = by
	m.state.Phase = PhaseValidating
	if !d.Enabled {
		m.state.Phase = PhaseDisabled
	}
	return m.state, nil
}
func (m *memoryState) Advance(_ context.Context, s State) (State, error) {
	m.advances++
	if m.failAdvance != nil {
		return State{}, m.failAdvance
	}
	if s.Generation != m.state.Generation {
		return State{}, ErrStaleGeneration
	}
	m.state = s
	return s, nil
}

type testMembers struct {
	emails, admins []string
	err            error
}

func (m testMembers) ActiveEmails(context.Context) ([]string, error)      { return m.emails, m.err }
func (m testMembers) ActiveAdminEmails(context.Context) ([]string, error) { return m.admins, m.err }

type testCredentials struct {
	saved cf.Secret
	err   error
}

func (s *testCredentials) Save(_ context.Context, t cf.Secret) error {
	if s.err != nil {
		return s.err
	}
	s.saved = t
	return nil
}

type testProjection struct {
	t           *testing.T
	credentials *testCredentials
	applied     []ProjectionState
	err         error
}

func (p *testProjection) Apply(_ context.Context, s ProjectionState) error {
	if s.Enabled && (s.Token.Reveal() == "" || p.credentials.saved != s.Token) {
		p.t.Fatal("projection before durable credential")
	}
	p.applied = append(p.applied, s)
	return p.err
}

type harness struct {
	t           *testing.T
	store       *memoryState
	cloud       *fakeCloud
	members     testMembers
	projection  *testProjection
	credentials *testCredentials
	r           *Reconciler
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, store: &memoryState{state: State{Generation: 1, Phase: PhaseValidating, Desired: Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura", WARPLabel: "aura-warp"}, Resources: Resources{AccountID: "account"}}}, members: testMembers{emails: []string{"admin@example.com"}, admins: []string{"admin@example.com"}}, credentials: &testCredentials{}}
	h.projection = &testProjection{t: t, credentials: h.credentials}
	h.cloud = &fakeCloud{t: t, store: h.store, zone: cf.Zone{Name: "example.com", Status: "active", Account: cf.Account{ID: "account"}}, apps: map[string]cf.AccessApplication{}, policies: map[string]cf.AccessPolicy{}, dns: map[string]cf.DNSRecord{}}
	h.r = h.newReconciler()
	return h
}
func (h *harness) newReconciler() *Reconciler {
	return New(h.store, h.cloud, h.members, h.projection, slog.New(slog.NewTextHandler(io.Discard, nil)), WithCredentials(h.credentials))
}
func transientError() error { return &cf.APIError{Status: 503, Retryable: true} }
func absent() error         { return &cf.APIError{Status: 404} }

type fakeCloud struct {
	t                                                   *testing.T
	store                                               *memoryState
	calls, creates, deletes, stopAfter, failDeleteAfter int
	failure                                             error
	failureAt                                           int
	zone                                                cf.Zone
	tunnel                                              cf.Tunnel
	otp                                                 cf.IdentityProvider
	posture                                             cf.Posture
	apps                                                map[string]cf.AccessApplication
	policies                                            map[string]cf.AccessPolicy
	dns                                                 map[string]cf.DNSRecord
	ingress                                             cf.TunnelConfig
	ambiguousPolicy                                     bool
	allowUnpersisted                                    bool
	keepDeleted                                         bool
}

func (f *fakeCloud) call() error {
	f.calls++
	if f.failureAt > 0 && f.calls == f.failureAt {
		return transientError()
	}
	if !f.allowUnpersisted {
		s := f.store.state.Resources
		pairs := [][2]string{{f.zone.ID, s.ZoneID}, {f.tunnel.ID, s.TunnelID}, {f.otp.ID, s.OTPProviderID}, {f.posture.ID, s.GatewayPostureID}, {f.apps["public-app"].ID, s.PublicAppID}, {f.apps["warp-app"].ID, s.WARPAppID}, {f.policies["public-policy"].ID, s.PublicPolicyID}, {f.policies["warp-policy"].ID, s.WARPPolicyID}, {f.dns["public-dns"].ID, s.PublicDNSID}, {f.dns["warp-dns"].ID, s.WARPDNSID}}
		for _, p := range pairs {
			if p[0] != "" && p[0] != p[1] {
				f.t.Fatalf("next API call before resource %s persisted", p[0])
			}
		}
	}
	if f.failure != nil {
		return f.failure
	}
	if f.stopAfter > 0 && f.creates == f.stopAfter {
		return transientError()
	}
	return nil
}
func (f *fakeCloud) remove() error {
	if f.failDeleteAfter > 0 && f.deletes == f.failDeleteAfter {
		return transientError()
	}
	f.deletes++
	return nil
}
func (f *fakeCloud) VerifyToken(context.Context) (cf.TokenVerification, error) {
	return cf.TokenVerification{Status: "active"}, f.call()
}
func (f *fakeCloud) ListAccounts(context.Context) ([]cf.Account, error) {
	return []cf.Account{{ID: "account"}}, f.call()
}
func (f *fakeCloud) ListZones(context.Context, string, string) ([]cf.Zone, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	if f.zone.ID != "" {
		return []cf.Zone{f.zone}, nil
	}
	return nil, nil
}
func (f *fakeCloud) GetZone(context.Context, string) (cf.Zone, error) { return f.zone, f.call() }
func (f *fakeCloud) CreateZone(context.Context, string, string) (cf.Zone, error) {
	if err := f.call(); err != nil {
		return cf.Zone{}, err
	}
	f.creates++
	f.zone.ID = "zone"
	return f.zone, nil
}
func (f *fakeCloud) ListTunnels(context.Context, string) ([]cf.Tunnel, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	if f.tunnel.ID != "" {
		return []cf.Tunnel{f.tunnel}, nil
	}
	return nil, nil
}
func (f *fakeCloud) GetTunnel(context.Context, string, string) (cf.Tunnel, error) {
	if err := f.call(); err != nil {
		return cf.Tunnel{}, err
	}
	if f.tunnel.ID == "" {
		return cf.Tunnel{}, absent()
	}
	return f.tunnel, nil
}
func (f *fakeCloud) CreateTunnel(_ context.Context, _, name string) (cf.Tunnel, error) {
	if err := f.call(); err != nil {
		return cf.Tunnel{}, err
	}
	f.creates++
	f.tunnel = cf.Tunnel{ID: "tunnel", Name: name, RemoteConfig: true, Status: "healthy"}
	return f.tunnel, nil
}
func (f *fakeCloud) DeleteTunnel(context.Context, string, string) error {
	if err := f.remove(); err != nil {
		return err
	}
	if !f.keepDeleted {
		f.tunnel = cf.Tunnel{}
	}
	return nil
}
func (f *fakeCloud) TunnelToken(context.Context, string, string) (cf.Secret, error) {
	return cf.Secret("tunnel-secret"), f.call()
}
func (f *fakeCloud) PutTunnelConfig(_ context.Context, _, _ string, c cf.TunnelConfig) error {
	if err := f.call(); err != nil {
		return err
	}
	if len(f.apps) != 2 || len(f.policies) != 2 || f.posture.Type != "gateway" {
		f.t.Fatal("ingress before Access and Gateway")
	}
	if len(c.Ingress) != 3 || c.Ingress[2].Service != "http_status:404" || c.Ingress[2].Hostname != "" {
		f.t.Fatal("invalid ingress")
	}
	for _, rule := range c.Ingress[:2] {
		if rule.Service != "http://caddy:8080" {
			f.t.Fatal("wrong origin")
		}
	}
	f.ingress = c
	return nil
}
func (f *fakeCloud) ListIdentityProviders(context.Context, string) ([]cf.IdentityProvider, error) {
	return []cf.IdentityProvider{f.otp}, f.call()
}
func (f *fakeCloud) EnsureOTPProvider(context.Context, string) (cf.IdentityProvider, error) {
	if err := f.call(); err != nil {
		return cf.IdentityProvider{}, err
	}
	if f.otp.ID == "" {
		f.creates++
		f.otp = cf.IdentityProvider{ID: "otp", Type: "onetimepin"}
	}
	return f.otp, nil
}
func (f *fakeCloud) ListApplications(context.Context, string) ([]cf.AccessApplication, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	out := []cf.AccessApplication{}
	for _, a := range f.apps {
		out = append(out, a)
	}
	return out, nil
}
func (f *fakeCloud) GetApplication(_ context.Context, _, id string) (cf.AccessApplication, error) {
	if err := f.call(); err != nil {
		return cf.AccessApplication{}, err
	}
	a, ok := f.apps[id]
	if !ok {
		return a, absent()
	}
	return a, nil
}
func (f *fakeCloud) PutApplication(_ context.Context, _, id string, a cf.AccessApplication) (cf.AccessApplication, error) {
	if err := f.call(); err != nil {
		return a, err
	}
	if id == "" {
		f.creates++
		id = "public-app"
		if a.Domain == "aura-warp.example.com" {
			id = "warp-app"
		}
	}
	a.ID = id
	f.apps[id] = a
	return a, nil
}
func (f *fakeCloud) DeleteApplication(_ context.Context, _, id string) error {
	if err := f.remove(); err != nil {
		return err
	}
	if !f.keepDeleted {
		delete(f.apps, id)
	}
	return nil
}
func (f *fakeCloud) ListPolicies(_ context.Context, _, app string) ([]cf.AccessPolicy, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	id := "public-policy"
	if app == "warp-app" {
		id = "warp-policy"
	}
	if p, ok := f.policies[id]; ok {
		return []cf.AccessPolicy{p}, nil
	}
	return nil, nil
}
func (f *fakeCloud) GetPolicy(_ context.Context, _, _, id string) (cf.AccessPolicy, error) {
	if err := f.call(); err != nil {
		return cf.AccessPolicy{}, err
	}
	p, ok := f.policies[id]
	if !ok {
		return p, absent()
	}
	return p, nil
}
func (f *fakeCloud) PutPolicy(_ context.Context, _, app, id string, p cf.AccessPolicy) (cf.AccessPolicy, error) {
	if err := f.call(); err != nil {
		return p, err
	}
	if id == "" {
		f.creates++
		id = "public-policy"
		if app == "warp-app" {
			id = "warp-policy"
		}
	}
	if app == "warp-app" && (len(p.Require) != 2 || p.Require[1].DevicePosture.IntegrationUID != f.posture.ID) {
		f.t.Fatal("missing organization enrollment")
	}
	p.ID = id
	f.policies[id] = p
	if f.ambiguousPolicy {
		f.allowUnpersisted = true
		return cf.AccessPolicy{}, transientError()
	}
	return p, nil
}
func (f *fakeCloud) DeletePolicy(_ context.Context, _, _, id string) error {
	if err := f.remove(); err != nil {
		return err
	}
	if !f.keepDeleted {
		delete(f.policies, id)
	}
	return nil
}
func (f *fakeCloud) ListPosture(context.Context, string) ([]cf.Posture, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	if f.posture.ID != "" {
		return []cf.Posture{f.posture}, nil
	}
	return nil, nil
}
func (f *fakeCloud) GetPosture(context.Context, string, string) (cf.Posture, error) {
	if err := f.call(); err != nil {
		return cf.Posture{}, err
	}
	if f.posture.ID == "" {
		return cf.Posture{}, absent()
	}
	return f.posture, nil
}
func (f *fakeCloud) EnsureGatewayPosture(_ context.Context, _, _, name string) (cf.Posture, error) {
	if err := f.call(); err != nil {
		return cf.Posture{}, err
	}
	f.creates++
	f.posture = cf.Posture{ID: "gateway", Name: name, Type: "gateway"}
	return f.posture, nil
}
func (f *fakeCloud) DeletePosture(context.Context, string, string, string) error {
	if err := f.remove(); err != nil {
		return err
	}
	if !f.keepDeleted {
		f.posture = cf.Posture{}
	}
	return nil
}
func (f *fakeCloud) ListDNS(_ context.Context, _, host string) ([]cf.DNSRecord, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	out := []cf.DNSRecord{}
	for _, d := range f.dns {
		if d.Name == host {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakeCloud) GetDNS(_ context.Context, _, id string) (cf.DNSRecord, error) {
	if err := f.call(); err != nil {
		return cf.DNSRecord{}, err
	}
	d, ok := f.dns[id]
	if !ok {
		return d, absent()
	}
	return d, nil
}
func (f *fakeCloud) EnsureCNAME(_ context.Context, _, id, host, tunnel, owner string) (cf.DNSRecord, error) {
	if err := f.call(); err != nil {
		return cf.DNSRecord{}, err
	}
	if len(f.ingress.Ingress) != 3 {
		f.t.Fatal("DNS before protected ingress")
	}
	if id == "" {
		f.creates++
		id = "public-dns"
		if host == "aura-warp.example.com" {
			id = "warp-dns"
		}
	}
	d := cf.DNSRecord{ID: id, Name: host, Type: "CNAME", Content: tunnel + ".cfargotunnel.com", Proxied: true, TTL: 1, Comment: owner}
	f.dns[id] = d
	return d, nil
}
func (f *fakeCloud) DeleteDNS(_ context.Context, _, id, _ string) error {
	if err := f.remove(); err != nil {
		return err
	}
	if !f.keepDeleted {
		delete(f.dns, id)
	}
	return nil
}

func (h *harness) assertResources(t *testing.T) {
	t.Helper()
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(fmt.Errorf("provision fixture: %w", err))
	}
}
