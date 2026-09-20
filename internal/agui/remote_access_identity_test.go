package agui

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestRemoteAccessIdentityWakeAfterProvisionCommit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "refused"}[fail], func(t *testing.T) {
			svc, token := sagaService(t, &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}, []string{"identity.create"})
			svc.recovery = &fakeRecoveryStore{}
			s := &Server{onboarding: svc}
			wakes := 0
			s.SetIdentityChanged(func() { wakes++ })
			req := provReq(nil)
			if fail {
				req.Capabilities = []string{"identity.create"}
			}
			body, _ := json.Marshal(req)
			r := withPrincipal(httptest.NewRequest("POST", "/api/onboarding/"+token+"/provision", bytes.NewReader(body)), "creator-1")
			r.SetPathValue("sessionToken", token)
			rec := httptest.NewRecorder()
			s.handleOnboardingProvision(rec, r)
			want := 1
			if fail {
				want = 0
			}
			if wakes != want {
				t.Fatalf("wakes=%d want=%d status=%d body=%s", wakes, want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRemoteAccessIdentityWakeAfterRemovalCommit(t *testing.T) {
	for _, tc := range []struct {
		name              string
		deactivate, purge error
		want              int
	}{
		{name: "success", want: 1},
		{name: "refused", deactivate: errors.New("refused")},
		{name: "purge failed", purge: errors.New("failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeIdentityRemover{deactErr: tc.deactivate, purgeErr: tc.purge}
			s := &Server{}
			s.SetIdentityRemover(fake)
			wakes := 0
			s.SetIdentityChanged(func() {
				if fake.callCount() != 2 {
					t.Error("wake before saga completed")
				}
				wakes++
			})
			s.handleRemoveIdentity(httptest.NewRecorder(), removeRequest())
			if wakes != tc.want {
				t.Fatalf("wakes=%d want=%d", wakes, tc.want)
			}
		})
	}
}
