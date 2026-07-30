package web

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
)

// A peer that refuses a client by resetting the HTTP/2 stream must surface as
// http_error(peer_reset_stream), NOT as timeout. Production regression: six
// web_fetch calls to br-automation.com reported `timeout` after burning the full
// 10s deadline, while the peer had actually reset the stream in 0.15s because it
// disliked the User-Agent. The misclassification hid the cause for weeks.
func TestClassifyTransportErr_PeerResetStreamIsNotRetryableAndNotTimeout(t *testing.T) {
	// The shape net/http's bundled http2 produces for an RST_STREAM from the peer.
	streamErr := &url.Error{
		Op:  "Get",
		URL: "https://example.invalid/",
		Err: errors.New("stream error: stream ID 1; INTERNAL_ERROR; received from peer"),
	}

	got := classifyTransportErr(streamErr)

	if errors.Is(got, errRetryable) {
		t.Error("a peer stream reset is deterministic and must NOT be marked retryable")
	}
	we, ok := AsWebError(got)
	if !ok {
		t.Fatalf("want a *WebError, got %T: %v", got, got)
	}
	if we.Code != CodeHTTPError {
		t.Errorf("Code = %q, want %q", we.Code, CodeHTTPError)
	}
	if we.Reason != ReasonPeerResetStream {
		t.Errorf("Reason = %q, want %q", we.Reason, ReasonPeerResetStream)
	}
	if we.Code == CodeTimeout {
		t.Error("a stream reset must never be reported as a timeout")
	}
}

// The terminal mapper must keep the reset classification instead of collapsing it
// into the deadline branch.
func TestClassifyFetchErr_KeepsPeerResetOverTimeout(t *testing.T) {
	c := &Client{}
	reset := classifyTransportErr(errors.New("stream error: stream ID 1; INTERNAL_ERROR"))

	we, ok := AsWebError(c.classifyFetchErr(reset))
	if !ok {
		t.Fatalf("want a *WebError")
	}
	if we.Reason != ReasonPeerResetStream {
		t.Errorf("Reason = %q, want %q", we.Reason, ReasonPeerResetStream)
	}
}

// A genuine deadline still reports timeout — the reset branch must not swallow it.
func TestClassifyFetchErr_RealDeadlineStillTimeout(t *testing.T) {
	c := &Client{}
	we, ok := AsWebError(c.classifyFetchErr(fmt.Errorf("dial: %w", context.DeadlineExceeded)))
	if !ok {
		t.Fatalf("want a *WebError")
	}
	if we.Code != CodeTimeout {
		t.Errorf("Code = %q, want %q", we.Code, CodeTimeout)
	}
}

func TestPeerResetStream_Markers(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"internal error", errors.New("stream error: stream ID 3; INTERNAL_ERROR"), true},
		{"protocol error", errors.New("stream error: stream ID 1; PROTOCOL_ERROR"), true},
		{"refused stream", errors.New("http2: REFUSED_STREAM"), true},
		{"goaway", errors.New("http2: server sent GOAWAY and closed the connection"), true},
		{"nil", nil, false},
		{"plain refusal", errors.New("dial tcp 1.2.3.4:443: connect: connection refused"), false},
		{"deadline", context.DeadlineExceeded, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := peerResetStream(tc.err); got != tc.want {
				t.Errorf("peerResetStream(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
