package chatgptplan

import (
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
)

// bodyLedger counts the response bodies a client was handed and how many were closed.
type bodyLedger struct {
	base           http.RoundTripper
	opened, closed atomic.Int32
}

func (l *bodyLedger) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := l.base.RoundTrip(r)
	if err == nil {
		l.opened.Add(1)
		res.Body = &ledgerBody{ReadCloser: res.Body, ledger: l}
	}
	return res, err
}

type ledgerBody struct {
	io.ReadCloser
	ledger *bodyLedger
	once   atomic.Bool
}

func (b *ledgerBody) Close() error {
	if b.once.CompareAndSwap(false, true) {
		b.ledger.closed.Add(1)
	}
	return b.ReadCloser.Close()
}

// revoke reads the discovery document and then posts to the revocation endpoint. Every
// body it receives must be closed: an open one keeps the client's timeout goroutine alive
// for 30 s, which goleak caught on master on 2026-10-03.
func TestRevokeClosesEveryResponseBody(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
	}{{"revoked", 0}, {"server failure retried", 503}, {"client failure", 400}} {
		t.Run(test.name, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			f.connect(t, s, "alice")
			f.revokeCode = test.code
			ledger := &bodyLedger{base: http.DefaultTransport}
			s.client.Transport = ledger

			_ = s.Disconnect(ownerContext("alice"), "alice")

			if opened, closed := ledger.opened.Load(), ledger.closed.Load(); opened == 0 || opened != closed {
				t.Fatalf("response bodies opened %d, closed %d", opened, closed)
			}
		})
	}
}

// A revocation endpoint that drops the connection makes the client return no response at
// all; Disconnect must report the revocation unconfirmed instead of panicking.
func TestDisconnectWithUnreachableRevocationEndpoint(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	f.connect(t, s, "alice")
	f.revokeDrop = true

	if err := s.Disconnect(ownerContext("alice"), "alice"); !errors.Is(err, ErrRevocationUnconfirmed) {
		t.Fatalf("disconnect=%v, want %v", err, ErrRevocationUnconfirmed)
	}
}
