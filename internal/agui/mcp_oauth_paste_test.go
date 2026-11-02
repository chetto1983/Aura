package agui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func (f *fakeAuthProvider) StartLoopbackAuthorization(_ context.Context, owner, _ string) (mcp.Flow, error) {
	f.sawOwner, f.sawOrigin = owner, "loopback"
	return f.flow, f.startErr
}

func (f *fakeAuthProvider) PasteAuthorization(owner, _, address string) (mcp.Flow, error) {
	f.sawOwner, f.completeAt = owner, address
	return f.flow, f.flowErr
}

func TestMCPPasteRoute(t *testing.T) {
	for _, tc := range []struct {
		name, identity string
		err            error
		status         int
	}{
		{"accepted", "owner", nil, http.StatusOK},
		{"mismatch", "owner", mcp.ErrPasteMismatch, http.StatusConflict},
		{"spent", "owner", mcp.ErrFlowNotWaiting, http.StatusConflict},
		{"missing", "owner", mcp.ErrNoSuchFlow, http.StatusNotFound},
		{"anonymous", "", nil, http.StatusUnauthorized},
		{"internal", "owner", errors.New("private details"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &fakeAuthProvider{flow: mcp.Flow{ID: "flow-1", Status: mcp.FlowApproved}, flowErr: tc.err}
			mux := http.NewServeMux()
			serverWithAuth(p).registerMCPAuthorizationRoutes(mux)
			r := authRequest(http.MethodPost, "/api/governance/mcp/authorization/flow/flow-1/paste", tc.identity)
			r.Body = io.NopCloser(strings.NewReader(`{"address":"http://127.0.0.1:43121/cb?state=secret-state&code=secret-code"}`))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if w.Code == http.StatusOK {
				var body struct {
					FlowID string `json:"flowId"`
					Status string `json:"status"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.FlowID != "flow-1" || body.Status != mcp.FlowApproved {
					t.Fatalf("flow response = %+v, %v", body, err)
				}
			}
			if strings.Contains(w.Body.String(), "secret-") || strings.Contains(w.Body.String(), "private details") {
				t.Fatalf("sensitive input echoed: %s", w.Body.String())
			}
			if tc.identity != "" && p.sawOwner != tc.identity {
				t.Fatalf("owner = %q", p.sawOwner)
			}
		})
	}
}

func TestMCPStartViaPasteUsesTheLoopbackProvider(t *testing.T) {
	p := &fakeAuthProvider{flow: mcp.Flow{ID: "flow-1"}}
	r := authRequest(http.MethodPost, "/api/governance/mcp/canva/authorization", "owner")
	r.Body = io.NopCloser(strings.NewReader(`{"via":"paste"}`))
	w := httptest.NewRecorder()
	serverWithAuth(p).handleMCPAuthorizationStart(w, r)
	if w.Code != http.StatusOK || p.sawOrigin != "loopback" || p.sawOwner != "owner" {
		t.Fatalf("start = %d, %q, %q", w.Code, p.sawOrigin, p.sawOwner)
	}
}
