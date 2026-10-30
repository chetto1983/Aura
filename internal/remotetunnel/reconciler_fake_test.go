package remotetunnel

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"

	cf "github.com/chetto1983/aura/internal/cloudflareapi"
)

type memoryState struct {
	state       State
	failAdvance error
	advances    int
	loadError   error
	log         []State
}

func (m *memoryState) Load(context.Context) (State, error) { return m.state, m.loadError }
func (m *memoryState) SaveDesired(_ context.Context, g int64, d Desired, by string) (State, error) {
	if g != m.state.Generation {
		return State{}, ErrStaleGeneration
	}
	if m.state.Desired.ZoneName != d.ZoneName || m.state.Desired.PublicLabel != d.PublicLabel {
		m.state.AcceptedHostname = ""
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
	m.log = append(m.log, s)
	return s, nil
}

func (m *memoryState) AddAbandoned(_ context.Context, list []AbandonedResource) error {
	m.state.Abandoned = append(m.state.Abandoned, list...)
	return nil
}

func (m *memoryState) DismissAbandoned(context.Context) error {
	m.state.Abandoned = nil
	return nil
}

type testMembers struct {
	emails, admins []string
	err            error
}

func (m testMembers) ActiveEmails(context.Context) ([]string, error)      { return m.emails, m.err }
func (m testMembers) ActiveAdminEmails(context.Context) ([]string, error) { return m.admins, m.err }

type testCredentials struct {
	saved cf.Secret
	saves []cf.Secret
	err   error
	// events is shared with the projection, to assert which of the two came first.
	events *[]string
}

func (s *testCredentials) Save(_ context.Context, t cf.Secret) error {
	if s.err != nil {
		return s.err
	}
	s.saved = t
	s.saves = append(s.saves, t)
	if s.events != nil {
		*s.events = append(*s.events, "save:"+string(t))
	}
	return nil
}

type testProjection struct {
	t           *testing.T
	credentials *testCredentials
	applied     []ProjectionState
	err         error
	// deletes reads how many Cloudflare deletions had happened at each Apply.
	deletes        func() int
	deletesAtApply []int
	events         *[]string
}

func (p *testProjection) Apply(_ context.Context, s ProjectionState) error {
	if s.Enabled && (s.Token.Reveal() == "" || p.credentials.saved != s.Token) {
		p.t.Fatal("projection before durable credential")
	}
	p.applied = append(p.applied, s)
	if p.events != nil {
		*p.events = append(*p.events, fmt.Sprintf("apply:%v", s.Enabled))
	}
	if p.deletes != nil {
		p.deletesAtApply = append(p.deletesAtApply, p.deletes())
	}
	return p.err
}

// enabled is the projections that started the connector; a pass without a tunnel also records the
// off projection that stops a stale one.
func (p *testProjection) enabled() []ProjectionState {
	var out []ProjectionState
	for _, s := range p.applied {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

type harness struct {
	t           *testing.T
	store       *memoryState
	cloud       *fakeCloud
	members     testMembers
	projection  *testProjection
	credentials *testCredentials
	ns          *fakeNS
	r           *Reconciler
	events      []string
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, store: &memoryState{state: State{Generation: 1, Phase: PhaseValidating, Desired: Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}, Resources: Resources{AccountID: "account"}}}, members: testMembers{emails: []string{"admin@example.com"}, admins: []string{"admin@example.com"}}, credentials: &testCredentials{}, ns: &fakeNS{hosts: []string{"dns.register.it."}}}
	h.projection = &testProjection{t: t, credentials: h.credentials, deletes: func() int { return h.cloud.deletes }}
	h.credentials.events, h.projection.events = &h.events, &h.events
	h.cloud = &fakeCloud{t: t, store: h.store, zone: cf.Zone{Name: "example.com", Status: "active", Account: cf.Account{ID: "account"}}, apps: map[string]cf.AccessApplication{}, policies: map[string]cf.AccessPolicy{}, dns: map[string]cf.DNSRecord{}}
	h.r = h.newReconciler()
	return h
}
func (h *harness) newReconciler() *Reconciler {
	return New(h.store, h.cloud, h.members, h.projection, slog.New(slog.NewTextHandler(io.Discard, nil)), WithCredentials(h.credentials), WithNameServers(h.ns))
}

// fakeNS answers every lookup with hosts, or with err; tests never reach real DNS.
type fakeNS struct {
	hosts []string
	err   error
	asked []string
}

func (f *fakeNS) LookupNS(_ context.Context, name string) ([]*net.NS, error) {
	f.asked = append(f.asked, name)
	var out []*net.NS
	for _, h := range f.hosts {
		out = append(out, &net.NS{Host: h})
	}
	return out, f.err
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
	emptyToken                                          bool
	blind                                               bool
	postureReadErr, postureRemoveErr                    error
	dnsReadErr, dnsRemoveErr                            error
	failPublish                                         bool
	policyReadErr, policyRemoveErr                      error
	appReadErr, appRemoveErr                            error
	postureConfirmErr                                   error
	deletesAtPublish                                    int
	otpAccount                                          string
	publicDNSRemoveErr                                  error
	// accountIDs are the accounts the token sees; empty means just "account".
	accountIDs []string
	// nextZone replaces zone the moment ListZones is asked for its name.
	nextZone                       cf.Zone
	zoneCreates                    int
	deleteAccounts, createAccounts []string
	// allowDetached lets the zone and the OTP provider outlive their local references.
	allowDetached bool
	// tunnelDeleteErr is what DeleteTunnel answers before anything is deleted.
	tunnelDeleteErr error
	// blindAfterLists makes ListAccounts lose every account after that many calls.
	blindAfterLists, listCalls int
}

func (f *fakeCloud) call() error {
	f.calls++
	if f.failureAt > 0 && f.calls == f.failureAt {
		return transientError()
	}
	if !f.allowUnpersisted {
		s := f.store.state.Resources
		pairs := [][2]string{{f.zone.ID, s.ZoneID}, {f.tunnel.ID, s.TunnelID}, {f.otp.ID, s.OTPProviderID}, {f.posture.ID, s.GatewayPostureID}, {f.apps["public-app"].ID, s.PublicAppID}, {f.apps["warp-app"].ID, s.WARPAppID}, {f.policies["public-policy"].ID, s.PublicPolicyID}, {f.policies["warp-policy"].ID, s.WARPPolicyID}, {f.dns["public-dns"].ID, s.PublicDNSID}, {f.dns["warp-dns"].ID, s.WARPDNSID}}
		if f.allowDetached {
			pairs = pairs[1:2]
			pairs = append(pairs, [2]string{f.posture.ID, s.GatewayPostureID}, [2]string{f.apps["public-app"].ID, s.PublicAppID}, [2]string{f.policies["public-policy"].ID, s.PublicPolicyID}, [2]string{f.dns["public-dns"].ID, s.PublicDNSID})
		}
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
	f.listCalls++
	if f.blind || f.blindAfterLists > 0 && f.listCalls > f.blindAfterLists {
		return nil, f.call()
	}
	ids := f.accountIDs
	if len(ids) == 0 {
		ids = []string{"account"}
	}
	accounts := []cf.Account{}
	for _, id := range ids {
		accounts = append(accounts, cf.Account{ID: id})
	}
	return accounts, f.call()
}
func (f *fakeCloud) ListZones(_ context.Context, account, name string) ([]cf.Zone, error) {
	if err := f.call(); err != nil {
		return nil, err
	}
	f.createAccounts = append(f.createAccounts, "zone:"+account)
	if f.nextZone.ID != "" && f.nextZone.Name == name {
		f.zone, f.nextZone = f.nextZone, cf.Zone{}
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
	f.zoneCreates++
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
func (f *fakeCloud) CreateTunnel(_ context.Context, account, name string) (cf.Tunnel, error) {
	if err := f.call(); err != nil {
		return cf.Tunnel{}, err
	}
	f.createAccounts = append(f.createAccounts, "tunnel:"+account)
	f.creates++
	f.tunnel = cf.Tunnel{ID: "tunnel", Name: name, RemoteConfig: true, Status: "healthy"}
	return f.tunnel, nil
}
func (f *fakeCloud) DeleteTunnel(_ context.Context, account, _ string) error {
	if f.tunnelDeleteErr != nil {
		return f.tunnelDeleteErr
	}
	if err := f.remove(); err != nil {
		return err
	}
	f.deleteAccounts = append(f.deleteAccounts, "tunnel:"+account)
	if !f.keepDeleted {
		f.tunnel = cf.Tunnel{}
	}
	return nil
}
func (f *fakeCloud) TunnelToken(context.Context, string, string) (cf.Secret, error) {
	if f.emptyToken {
		return "", f.call()
	}
	return cf.Secret("tunnel-secret"), f.call()
}
func (f *fakeCloud) PutTunnelConfig(_ context.Context, _, _ string, c cf.TunnelConfig) error {
	if err := f.call(); err != nil {
		return err
	}
	if f.apps["public-app"].ID == "" || f.policies["public-policy"].ID == "" {
		f.t.Fatal("ingress before Access")
	}
	f.deletesAtPublish = f.deletes
	if f.failPublish {
		return transientError()
	}
	if len(c.Ingress) != 2 || c.Ingress[1].Service != "http_status:404" || c.Ingress[1].Hostname != "" || c.Ingress[0].Service != "http://caddy:8080" {
		f.t.Fatal("invalid ingress")
	}
	f.ingress = c
	return nil
}
func (f *fakeCloud) ListIdentityProviders(context.Context, string) ([]cf.IdentityProvider, error) {
	return []cf.IdentityProvider{f.otp}, f.call()
}
func (f *fakeCloud) EnsureOTPProvider(_ context.Context, account string) (cf.IdentityProvider, error) {
	if err := f.call(); err != nil {
		return cf.IdentityProvider{}, err
	}
	if f.otp.ID == "" || f.otpAccount != account {
		f.creates++
		f.otpAccount = account
		f.otp = cf.IdentityProvider{ID: "otp", Type: "onetimepin"}
		if account != "account" {
			f.otp.ID = "otp-" + account
		}
		f.createAccounts = append(f.createAccounts, "otp:"+account)
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
	if id == "warp-app" && f.appReadErr != nil {
		return cf.AccessApplication{}, f.appReadErr
	}
	a, ok := f.apps[id]
	if !ok {
		return a, absent()
	}
	return a, nil
}
func (f *fakeCloud) PutApplication(_ context.Context, account, id string, a cf.AccessApplication) (cf.AccessApplication, error) {
	if err := f.call(); err != nil {
		return a, err
	}
	if id == "" {
		f.creates++
		f.createAccounts = append(f.createAccounts, "app:"+account)
		id = "public-app"
	}
	a.ID = id
	f.apps[id] = a
	return a, nil
}
func (f *fakeCloud) DeleteApplication(_ context.Context, account, id string) error {
	if id == "warp-app" && f.appRemoveErr != nil {
		return f.appRemoveErr
	}
	if err := f.remove(); err != nil {
		return err
	}
	f.deleteAccounts = append(f.deleteAccounts, "app:"+account)
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
	if id == "warp-policy" && f.policyReadErr != nil {
		return cf.AccessPolicy{}, f.policyReadErr
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
	}
	p.ID = id
	f.policies[id] = p
	if f.ambiguousPolicy {
		f.allowUnpersisted = true
		return cf.AccessPolicy{}, transientError()
	}
	return p, nil
}
func (f *fakeCloud) DeletePolicy(_ context.Context, account, _, id string) error {
	if id == "warp-policy" && f.policyRemoveErr != nil {
		return f.policyRemoveErr
	}
	if err := f.remove(); err != nil {
		return err
	}
	f.deleteAccounts = append(f.deleteAccounts, "policy:"+account)
	if !f.keepDeleted {
		delete(f.policies, id)
	}
	return nil
}
func (f *fakeCloud) GetPosture(context.Context, string, string) (cf.Posture, error) {
	if err := f.call(); err != nil {
		return cf.Posture{}, err
	}
	if f.postureReadErr != nil {
		return cf.Posture{}, f.postureReadErr
	}
	if f.posture.ID == "" && f.postureConfirmErr != nil {
		return cf.Posture{}, f.postureConfirmErr
	}
	if f.posture.ID == "" {
		return cf.Posture{}, absent()
	}
	return f.posture, nil
}
func (f *fakeCloud) DeletePosture(context.Context, string, string, string) error {
	if f.postureRemoveErr != nil {
		return f.postureRemoveErr
	}
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
	if id == "warp-dns" && f.dnsReadErr != nil {
		return cf.DNSRecord{}, f.dnsReadErr
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
	if len(f.ingress.Ingress) != 2 {
		f.t.Fatal("DNS before protected ingress")
	}
	if id == "" {
		f.creates++
		id = "public-dns"
	}
	d := cf.DNSRecord{ID: id, Name: host, Type: "CNAME", Content: tunnel + ".cfargotunnel.com", Proxied: true, TTL: 1, Comment: owner}
	f.dns[id] = d
	return d, nil
}
func (f *fakeCloud) DeleteDNS(_ context.Context, _, id, _ string) error {
	if id == "public-dns" && f.publicDNSRemoveErr != nil {
		return f.publicDNSRemoveErr
	}
	if id == "warp-dns" && f.dnsRemoveErr != nil {
		return f.dnsRemoveErr
	}
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
