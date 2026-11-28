package anthropic

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

// The TLS failures here are the ones crypto/tls itself builds, over real connections: the
// classifier matches their types, so a hand-built error would prove only the hand.

// alertingServer answers the first TLS ClientHello with one plaintext fatal alert record,
// the way a peer reports a record it could not authenticate.
func alertingServer(t *testing.T, alert byte) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = conn.Read(make([]byte, 16<<10))
		_, _ = conn.Write([]byte{0x15, 0x03, 0x03, 0x00, 0x02, 0x02, alert})
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	return listener.Addr().String()
}

func quietClient(hc *http.Client, baseURL string) *Client {
	client := newClient(testConfig(), staticKey(houseKey), hc, baseURL)
	client.logger = func() *slog.Logger { return slog.New(slog.DiscardHandler) }
	return client
}

// Measured on the lab VM 2026-11-28 13:22:14: `remote error: tls: bad record MAC` on a
// stream open was not retried, and the turn ended with no answer. A record that fails its
// integrity check is a corrupted connection, and a new one is the remedy; a handshake the
// peer refuses is not.
func TestTLSRecordCorruptionIsRetryable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		alert     byte
		message   string
		retryable bool
	}{
		{"bad record MAC", 20, "remote error: tls: bad record MAC", true},
		{"handshake failure", 40, "remote error: tls: handshake failure", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := quietClient(newHTTPClient(5), "https://"+alertingServer(t, tc.alert))
			_, err := client.Stream(context.Background(), userRequest("hi"))
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("stream open error = %v, want %q", err, tc.message)
			}
			if got := agent.IsTransientLLMError(err); got != tc.retryable {
				t.Fatalf("IsTransientLLMError(%v) = %v, want %v", err, got, tc.retryable)
			}
		})
	}
}

// Behind an HTTPS proxy net/http wraps the alert from the proxy's handshake in an outer
// *net.OpError{Op: "proxyconnect"}, so the classifier has to look past the first one.
func TestTLSRecordCorruptionBehindAnHTTPSProxyIsRetryable(t *testing.T) {
	proxy := &url.URL{Scheme: "https", Host: alertingServer(t, 20)}
	hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	_, err := quietClient(hc, "https://api.anthropic.invalid").Stream(context.Background(), userRequest("hi"))
	if err == nil || !strings.Contains(err.Error(), "proxyconnect tcp: remote error: tls: bad record MAC") {
		t.Fatalf("stream open error = %v, want the proxy's bad record MAC", err)
	}
	if !agent.IsTransientLLMError(err) {
		t.Fatalf("IsTransientLLMError(%v) = false, want true", err)
	}
}

// corruptingProxy relays TCP between the client and target, record by record from the
// server side, and flips a byte of the first application-data record it relays once
// corrupt is set: the client's AEAD check then fails on a connection that was healthy.
func corruptingProxy(t *testing.T, target string, corrupt *atomic.Bool) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		client, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = client.Close() }()
		server, err := net.Dial("tcp", target)
		if err != nil {
			return
		}
		defer func() { _ = server.Close() }()
		wg.Go(func() {
			_, _ = io.Copy(server, client)
			_ = server.Close()
		})
		header := make([]byte, 5)
		for {
			if _, err := io.ReadFull(server, header); err != nil {
				return
			}
			payload := make([]byte, binary.BigEndian.Uint16(header[3:]))
			if _, err := io.ReadFull(server, payload); err != nil {
				return
			}
			if header[0] == 0x17 && len(payload) > 0 && corrupt.CompareAndSwap(true, false) {
				payload[0] ^= 0xff
			}
			if _, err := client.Write(append(header, payload...)); err != nil {
				return
			}
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})
	return listener.Addr().String()
}

// The same alert raised by this side, mid-stream: a record of the response body arrives
// corrupted after the first text delta, and crypto/tls reports `local error`.
func TestTLSRecordCorruptedMidStreamIsRetryable(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeEvents(w, append([]sseEvent{messageStart(10, 0, 0, "[]"), blockStart(0, `{"type":"text","text":""}`)}, delta(0, "text_delta", "text", "Ciao"))...)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeEvents(w, blockStop(0), messageDelta("end_turn", 5), messageStop())
	}))
	defer server.Close()
	var corrupt atomic.Bool
	addr := corruptingProxy(t, server.Listener.Addr().String(), &corrupt)
	trusted := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	hc := &http.Client{Transport: &http.Transport{TLSClientConfig: trusted, DisableKeepAlives: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := quietClient(hc, "https://"+addr).Stream(ctx, userRequest("hi"))
	if err != nil {
		t.Fatal(err)
	}
	var last llm.Chunk
	released := false
	for chunk := range stream {
		if chunk.Text != "" && !released {
			corrupt.Store(true)
			close(release)
			released = true
		}
		last = chunk
	}
	if !released {
		close(release)
	}
	err = last.Err
	if err == nil || !strings.Contains(err.Error(), "local error: tls: bad record MAC") {
		t.Fatalf("stream end error = %v, want this side's bad record MAC", err)
	}
	if !agent.IsTransientLLMError(err) {
		t.Fatalf("IsTransientLLMError(%v) = false, want true", err)
	}
}
