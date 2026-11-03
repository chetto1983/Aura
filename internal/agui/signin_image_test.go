package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

type signInImageProviderTest struct{ pulls int }

func (*signInImageProviderTest) ImageState(context.Context) (SignInImageState, error) {
	return SignInImageState{Pulling: true, Done: 50, Total: 100}, nil
}
func (p *signInImageProviderTest) PullImage(context.Context) (SignInImageState, error) {
	p.pulls++
	return SignInImageState{Pulling: true}, nil
}

func TestSignInImageRoutesKeepDownloadBehindTheOwnerAndConfiguredImage(t *testing.T) {
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	p := &signInImageProviderTest{}
	s.SetSignInImages(p)
	for _, tc := range []struct {
		method, body, owner string
		want                int
	}{{"GET", "", "alice", 200}, {"POST", "{}", "alice", 200}, {"POST", `{"image":"attacker.example/anything"}`, "alice", 400}, {"POST", "{}", "", 401}} {
		r := httptest.NewRequest(tc.method, "https://aura.test/api/governance/sign-in/image", strings.NewReader(tc.body))
		r = r.WithContext(identityctx.WithIdentityID(r.Context(), tc.owner))
		r.Header.Set("Idempotency-Key", "image-fixture-"+tc.owner+tc.body)
		rr := httptest.NewRecorder()
		s.Mux().ServeHTTP(rr, r)
		if rr.Code != tc.want {
			t.Fatalf("%+v status=%d body=%s", tc, rr.Code, rr.Body)
		}
		if tc.method == http.MethodGet && !strings.Contains(rr.Body.String(), `"done":50`) {
			t.Fatalf("missing progress: %s", rr.Body)
		}
	}
	if p.pulls != 1 {
		t.Fatalf("pulls=%d", p.pulls)
	}
}
