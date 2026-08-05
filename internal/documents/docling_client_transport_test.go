package documents

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

type doclingStubResolver struct {
	ips []netip.Addr
	err error
}

func (resolver doclingStubResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return resolver.ips, resolver.err
}

func TestResolvePrivateDoclingIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ips     []netip.Addr
		want    string
		wantErr bool
	}{
		{name: "private", ips: []netip.Addr{netip.MustParseAddr("10.1.2.3")}, want: "10.1.2.3"},
		{name: "loopback", ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, want: "127.0.0.1"},
		{name: "public", ips: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, wantErr: true},
		{name: "mixed", ips: []netip.Addr{
			netip.MustParseAddr("10.1.2.3"), netip.MustParseAddr("8.8.8.8"),
		}, wantErr: true},
		{name: "link_local", ips: []netip.Addr{netip.MustParseAddr("169.254.169.254")}, wantErr: true},
		{name: "empty", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolvePrivateDoclingIP(
				context.Background(), doclingStubResolver{ips: test.ips}, "aura-docling",
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && got.String() != test.want {
				t.Fatalf("IP = %s, want %s", got, test.want)
			}
		})
	}
}

func TestDoclingClientStrictResponseAndSizeBound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantClass DoclingErrorClass
	}{
		{
			name: "unknown wire field",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writeDoclingJSON(t, writer, `{
  "task_id":"task-123","task_type":"chunk","task_status":"started",
  "task_position":null,"task_meta":null,"error_message":null,"failure":null,
  "unexpected":true
}`)
			},
			wantClass: DoclingErrorProtocol,
		},
		{
			name: "oversize content length",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.Header().Set("Content-Length", "1048577")
				_, _ = io.WriteString(writer, `{}`)
			},
			wantClass: DoclingErrorResponseTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(test.handler)
			defer server.Close()
			client, err := NewDoclingClient(testDocumentConfig(server.URL), WithDoclingHTTPClient(server.Client()))
			if err != nil {
				t.Fatalf("NewDoclingClient: %v", err)
			}
			_, err = client.Poll(context.Background(), "task-123")
			var doclingErr *DoclingError
			if !errors.As(err, &doclingErr) || doclingErr.Class != test.wantClass {
				t.Fatalf("err = %#v, want class %s", err, test.wantClass)
			}
		})
	}
}

func TestDoclingClientAuthErrorDoesNotExposeBodyOrCredential(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, `{"detail":"private payload and key `+testDoclingAPIKey+`"}`)
	}))
	defer server.Close()
	client, err := NewDoclingClient(testDocumentConfig(server.URL), WithDoclingHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewDoclingClient: %v", err)
	}
	_, err = client.SubmitFile(context.Background(), DoclingFile{
		Name: "secret.pdf", Size: 1, Reader: strings.NewReader("x"),
	})
	var doclingErr *DoclingError
	if !errors.As(err, &doclingErr) || doclingErr.Class != DoclingErrorAuthentication || doclingErr.Retryable {
		t.Fatalf("err = %#v", err)
	}
	for _, forbidden := range []string{"payload", testDoclingAPIKey, "secret.pdf", server.URL} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error exposes %q: %v", forbidden, err)
		}
	}
}

func TestDoclingClientPollingHonorsContextDeadline(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeDoclingJSON(t, writer, taskStatusJSON("pending"))
	}))
	defer server.Close()
	client, err := NewDoclingClient(testDocumentConfig(server.URL), WithDoclingHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewDoclingClient: %v", err)
	}
	client.pollPeriod = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = client.Wait(ctx, "task-123")
	var doclingErr *DoclingError
	if !errors.As(err, &doclingErr) || doclingErr.Class != DoclingErrorTimeout {
		t.Fatalf("err = %#v", err)
	}
}

func TestNewDoclingClientRejectsPublicOrIncompleteBoundary(t *testing.T) {
	t.Parallel()
	configs := []config.DocumentConfig{
		testDocumentConfig("https://docling.example.com"),
		testDocumentConfig("http://127.0.0.1:5001"),
	}
	configs[1].DoclingAPIKey = ""
	for _, candidate := range configs {
		_, err := NewDoclingClient(candidate)
		var doclingErr *DoclingError
		if !errors.As(err, &doclingErr) || doclingErr.Class != DoclingErrorConfig {
			t.Fatalf("err = %#v", err)
		}
		if strings.Contains(err.Error(), candidate.DoclingBaseURL) {
			t.Fatalf("configuration error exposes URL: %v", err)
		}
	}
}
