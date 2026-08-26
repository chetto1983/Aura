package webauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authulamodels "github.com/Authula/authula/models"
	authulaservices "github.com/Authula/authula/services"
)

// ensureAuthulaSearchPath has its own co-located table in authula_test.go.

func TestNewRejectsEmptySecret(t *testing.T) {
	_, err := New(Config{DSN: "postgres://u:p@h:5432/db", Secret: "   "})
	if err == nil || !strings.Contains(err.Error(), "AURA_AUTHULA_SECRET") {
		t.Fatalf("want secret error, got %v", err)
	}
}

func TestNewRejectsWeakSecret(t *testing.T) {
	for _, secret := range []string{
		"secret",
		"0123456789abcdef0123456789abcdef",
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
	} {
		t.Run(secret, func(t *testing.T) {
			_, err := New(Config{DSN: "postgres://u:p@h:5432/db", Secret: secret})
			if err == nil || !strings.Contains(err.Error(), "64 hex characters") {
				t.Fatalf("want weak secret error, got %v", err)
			}
		})
	}
}

func TestNewRejectsBadDSN(t *testing.T) {
	_, err := New(Config{DSN: "://%zz", Secret: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	if err == nil || !strings.Contains(err.Error(), "dsn") {
		t.Fatalf("want dsn error, got %v", err)
	}
}

func TestHardenedCookieConstants(t *testing.T) {
	if !strings.HasPrefix(SessionCookieName, "__Host-") {
		t.Errorf("session cookie %q must carry the __Host- prefix (H2)", SessionCookieName)
	}
	if !strings.HasPrefix(CSRFCookieName, "__Host-") {
		t.Errorf("csrf cookie %q must carry the __Host- prefix", CSRFCookieName)
	}
	if CSRFHeaderName != "X-AUTHULA-CSRF-TOKEN" {
		t.Errorf("csrf header = %q, want X-AUTHULA-CSRF-TOKEN", CSRFHeaderName)
	}
}

func TestRateLimitMaxDefault(t *testing.T) {
	if got := rateLimitMax(0); got != 30 {
		t.Errorf("zero rate limit max = %d, want 30", got)
	}
	if got := rateLimitMax(-1); got != 30 {
		t.Errorf("negative rate limit max = %d, want 30", got)
	}
	if got := rateLimitMax(77); got != 77 {
		t.Errorf("configured rate limit max = %d, want 77", got)
	}
}

// --- Validator.Validate --------------------------------------------------------------

// fakeToken implements authulaservices.TokenService; Hash is identity-ish for testing.
type fakeToken struct{}

func (fakeToken) Generate() (string, error)        { return "tok", nil }
func (fakeToken) Hash(t string) string             { return "h:" + t }
func (fakeToken) Encrypt(t string) (string, error) { return t, nil }
func (fakeToken) Decrypt(t string) (string, error) { return t, nil }

// fakeSession implements authulaservices.SessionService; only GetByToken is exercised.
type fakeSession struct {
	byToken map[string]*authulamodels.Session
	err     error
}

func (f fakeSession) GetByToken(_ context.Context, hashed string) (*authulamodels.Session, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byToken[hashed], nil
}
func (fakeSession) GetByID(context.Context, string) (*authulamodels.Session, error) { return nil, nil }
func (fakeSession) Create(context.Context, string, string, *string, *string, time.Duration) (*authulamodels.Session, error) {
	return nil, nil
}
func (fakeSession) GetByUserID(context.Context, string) (*authulamodels.Session, error) {
	return nil, nil
}
func (fakeSession) Update(context.Context, *authulamodels.Session) (*authulamodels.Session, error) {
	return nil, nil
}
func (fakeSession) Delete(context.Context, string) error                    { return nil }
func (fakeSession) DeleteAllByUserID(context.Context, string) error         { return nil }
func (fakeSession) DeleteAllExpired(context.Context) error                  { return nil }
func (fakeSession) GetDistinctUserIDs(context.Context) ([]string, error)    { return nil, nil }
func (fakeSession) DeleteOldestByUserID(context.Context, string, int) error { return nil }

type fakeCore struct{ cs *authulaservices.CoreServices }

func (f fakeCore) CoreServices() *authulaservices.CoreServices { return f.cs }

type fakeResolver struct {
	byUser map[string]string
	err    error
}

func (f fakeResolver) ResolveIdentityID(_ context.Context, uid string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	id, ok := f.byUser[uid]
	if !ok {
		return "", ErrLinkNotFound
	}
	return id, nil
}

func reqWithCookie(value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if value != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: value})
	}
	return r
}

func validatorWith(sess fakeSession, res fakeResolver) *Validator {
	core := &authulaservices.CoreServices{TokenService: fakeToken{}, SessionService: sess}
	return NewValidator(fakeCore{cs: core}, res)
}

func TestValidate_Hit(t *testing.T) {
	const cookie = "rawtoken"
	const uid = "authula-user-1"
	const auraID = "00000000-0000-0000-0000-000000000001"
	sess := fakeSession{byToken: map[string]*authulamodels.Session{
		"h:" + cookie: {UserID: uid, ExpiresAt: time.Now().Add(time.Hour)},
	}}
	v := validatorWith(sess, fakeResolver{byUser: map[string]string{uid: auraID}})
	got, err := v.Validate(reqWithCookie(cookie))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != auraID {
		t.Errorf("identity = %q, want %q", got, auraID)
	}
}

func TestValidate_Failures(t *testing.T) {
	const cookie = "rawtoken"
	const uid = "u1"
	live := map[string]*authulamodels.Session{"h:" + cookie: {UserID: uid, ExpiresAt: time.Now().Add(time.Hour)}}

	tests := []struct {
		name string
		v    *Validator
		req  *http.Request
	}{
		{"missing cookie", validatorWith(fakeSession{byToken: live}, fakeResolver{byUser: map[string]string{uid: "x"}}), reqWithCookie("")},
		{"unknown token", validatorWith(fakeSession{byToken: map[string]*authulamodels.Session{}}, fakeResolver{}), reqWithCookie(cookie)},
		{"session service error", validatorWith(fakeSession{err: errors.New("db down")}, fakeResolver{}), reqWithCookie(cookie)},
		{"expired session", validatorWith(fakeSession{byToken: map[string]*authulamodels.Session{
			"h:" + cookie: {UserID: uid, ExpiresAt: time.Now().Add(-time.Minute)},
		}}, fakeResolver{byUser: map[string]string{uid: "x"}}), reqWithCookie(cookie)},
		{"resolver miss", validatorWith(fakeSession{byToken: live}, fakeResolver{byUser: map[string]string{}}), reqWithCookie(cookie)},
		{"resolver error", validatorWith(fakeSession{byToken: live}, fakeResolver{err: errors.New("link down")}), reqWithCookie(cookie)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := tt.v.Validate(tt.req)
			if !errors.Is(err, ErrNoSession) {
				t.Fatalf("want ErrNoSession, got id=%q err=%v", id, err)
			}
		})
	}
}

func TestValidate_NilGuards(t *testing.T) {
	var nilV *Validator
	if _, err := nilV.Validate(reqWithCookie("x")); !errors.Is(err, ErrNoSession) {
		t.Errorf("nil validator: want ErrNoSession, got %v", err)
	}
	// nil resolver
	core := &authulaservices.CoreServices{TokenService: fakeToken{}, SessionService: fakeSession{}}
	v := NewValidator(fakeCore{cs: core}, nil)
	if _, err := v.Validate(reqWithCookie("x")); !errors.Is(err, ErrNoSession) {
		t.Errorf("nil resolver: want ErrNoSession, got %v", err)
	}
	// nil core services
	v2 := NewValidator(fakeCore{cs: nil}, fakeResolver{})
	if _, err := v2.Validate(reqWithCookie("x")); !errors.Is(err, ErrNoSession) {
		t.Errorf("nil core: want ErrNoSession, got %v", err)
	}
}

func TestProviderCloseNilSafe(t *testing.T) {
	var p *Provider
	if err := p.Close(); err != nil {
		t.Errorf("nil provider Close: %v", err)
	}
	if err := (&Provider{}).Close(); err != nil {
		t.Errorf("provider without Authula Close: %v", err)
	}
	if id, err := p.OperatorUserID(context.Background()); err != nil || id != "" {
		t.Errorf("nil provider OperatorUserID: id=%q err=%v", id, err)
	}
}

// --- IdentityLinker (pool-less guards; DB path covered by integration test) ----------

func TestIdentityLinkerNilPool(t *testing.T) {
	l := NewIdentityLinker(nil)
	if _, err := l.ResolveIdentityID(context.Background(), "u"); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("nil pool resolve: want ErrLinkNotFound, got %v", err)
	}
	if err := l.LinkOperator(context.Background(), "id", "u"); err == nil {
		t.Errorf("nil pool link: want error")
	}
}
