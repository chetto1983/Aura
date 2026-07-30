package agui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStrictDecodeJSON(t *testing.T) {
	type payload struct {
		Value string `json:"value"`
	}

	tests := []struct {
		name        string
		body        string
		contentType string
		opts        decodeOpts
		wantErr     bool
		wantValue   string
	}{
		{
			name:        "valid JSON",
			body:        `{"value":"ok"}`,
			contentType: "application/json",
			wantValue:   "ok",
		},
		{
			name:        "valid JSON with media type parameter",
			body:        `{"value":"ok"}`,
			contentType: "application/json; charset=utf-8",
			wantValue:   "ok",
		},
		{
			name:      "missing content type is tolerated",
			body:      `{"value":"ok"}`,
			wantValue: "ok",
		},
		{
			name:        "wrong content type",
			body:        `{"value":"ok"}`,
			contentType: "text/plain",
			wantErr:     true,
		},
		{
			name:        "unknown field",
			body:        `{"value":"ok","unexpected":true}`,
			contentType: "application/json",
			wantErr:     true,
		},
		{
			name:        "trailing JSON value",
			body:        `{"value":"ok"}{"value":"smuggled"}`,
			contentType: "application/json",
			wantErr:     true,
		},
		{
			name:        "trailing whitespace is allowed",
			body:        "{\"value\":\"ok\"}\n\t ",
			contentType: "application/json",
			wantValue:   "ok",
		},
		{
			name:        "empty body denied by default",
			contentType: "application/json",
			wantErr:     true,
		},
		{
			name:        "empty body allowed explicitly",
			contentType: "application/json",
			opts:        decodeOpts{allowEmpty: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()
			var got payload
			err := strictDecodeJSON(rec, req, &got, tc.opts)
			if tc.wantErr && err == nil {
				t.Fatal("strictDecodeJSON() error = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("strictDecodeJSON() error = %v, want nil", err)
			}
			if !tc.wantErr && got.Value != tc.wantValue {
				t.Fatalf("decoded value = %q, want %q", got.Value, tc.wantValue)
			}
		})
	}
}

func TestStrictDecodeJSONBodyBoundary(t *testing.T) {
	const maxBytes = 64
	prefix := `{"value":"`
	suffix := `"}`
	exact := prefix + strings.Repeat("x", maxBytes-len(prefix)-len(suffix)) + suffix
	if len(exact) != maxBytes {
		t.Fatalf("fixture length = %d, want %d", len(exact), maxBytes)
	}

	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "exactly at cap", body: exact},
		{name: "one byte over cap", body: exact + " ", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			var got struct {
				Value string `json:"value"`
			}
			err := strictDecodeJSON(rec, req, &got, decodeOpts{maxBytes: maxBytes})
			if tc.wantErr && err == nil {
				t.Fatal("strictDecodeJSON() error = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("strictDecodeJSON() error = %v, want nil", err)
			}
		})
	}
}
