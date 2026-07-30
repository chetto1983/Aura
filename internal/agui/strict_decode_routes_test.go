package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrivilegedRouteDecodersRejectTrailingJSON(t *testing.T) {
	const trailing = `{"action":"accept"}{"ignored":true}`
	runBody := `{"threadId":"11111111-1111-1111-1111-111111111111","messages":[]}` +
		`{"ignored":true}`

	tests := []struct {
		name string
		body string
		call func(http.ResponseWriter, *http.Request) bool
	}{
		{
			name: "agent run",
			body: runBody,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				_, err := strictDecodeRunAgentRequest(w, r)
				return err == nil
			},
		},
		{
			name: "approval resolve",
			body: trailing,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				_, err := decodeResolveBody(w, r)
				return err == nil
			},
		},
		{
			name: "asset presign",
			body: `{"thread_id":"x"}{"ignored":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				_, err := decodeAssetPresignBody(w, r)
				return err == nil
			},
		},
		{
			name: "onboarding",
			body: `{"name":"Ada"}{"ignored":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				var body OnboardingSeed
				return decodeOnboardingBody(w, r, &body) == nil
			},
		},
		{
			name: "governance MCP",
			body: trailing,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				var body map[string]any
				return decodeMCPBody(w, r, &body)
			},
		},
		{
			name: "governance skills",
			body: trailing,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				var body map[string]any
				return decodeSkillsBody(w, r, &body)
			},
		},
		{
			name: "governance scheduler",
			body: `{"schedule_kind":"at"}{"ignored":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				_, err := decodeSchedulerEditBody(w, r)
				return err == nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.call(httptest.NewRecorder(), req) {
				t.Fatal("trailing JSON was accepted")
			}
		})
	}
}

func TestPrivilegedRouteDecodersRejectUnknownFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		call func(http.ResponseWriter, *http.Request) bool
	}{
		{
			name: "agent run",
			body: `{"threadId":"11111111-1111-1111-1111-111111111111","messages":[],"unknown":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				_, err := strictDecodeRunAgentRequest(w, r)
				return err == nil
			},
		},
		{
			name: "approval resolve",
			body: `{"action":"accept","unknown":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				_, err := decodeResolveBody(w, r)
				return err == nil
			},
		},
		{
			name: "asset presign",
			body: `{"thread_id":"x","unknown":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				_, err := decodeAssetPresignBody(w, r)
				return err == nil
			},
		},
		{
			name: "onboarding",
			body: `{"name":"Ada","unknown":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				var body OnboardingSeed
				return decodeOnboardingBody(w, r, &body) == nil
			},
		},
		{
			name: "governance MCP",
			body: `{"class":"trusted_local","unknown":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				var body struct {
					Class string `json:"class"`
				}
				return decodeMCPBody(w, r, &body)
			},
		},
		{
			name: "governance skills",
			body: `{"source":"x","unknown":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				var body struct {
					Source string `json:"source"`
				}
				return decodeSkillsBody(w, r, &body)
			},
		},
		{
			name: "governance scheduler",
			body: `{"schedule_kind":"at","unknown":true}`,
			call: func(w http.ResponseWriter, r *http.Request) bool {
				_, err := decodeSchedulerEditBody(w, r)
				return err == nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.call(httptest.NewRecorder(), req) {
				t.Fatal("unknown field was accepted")
			}
		})
	}
}

func TestGovernanceRouteDecodersAllowEmptyBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request) bool
	}{
		{
			name: "MCP",
			call: func(w http.ResponseWriter, r *http.Request) bool {
				var body struct{}
				return decodeMCPBody(w, r, &body)
			},
		},
		{
			name: "skills",
			call: func(w http.ResponseWriter, r *http.Request) bool {
				var body struct{}
				return decodeSkillsBody(w, r, &body)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Header.Set("Content-Type", "application/json")
			if !tc.call(httptest.NewRecorder(), req) {
				t.Fatal("empty verb-only body was rejected")
			}
		})
	}
}
