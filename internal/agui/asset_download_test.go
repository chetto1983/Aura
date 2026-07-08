package agui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"go.uber.org/goleak"
)

// TestAssetDownload_Owner proves the happy path: an authenticated owner streams the object body
// with the forced, stored-XSS-safe headers. The stored asset carries a hostile sniffed MIMEType
// (image/svg+xml) and a unicode FileName, so this single case doubles as the T-XSS proof (the
// serve Content-Type is octet-stream regardless of the sniffed mime) and the T-HdrInj proof (the
// filename rides a well-formed RFC-6266 filename*=UTF-8” param).
func TestAssetDownload_Owner(t *testing.T) {
	t.Parallel()
	data := []byte("the stored object bytes \x00\x01\x02")
	fake := &fakeAssetService{
		openResp: io.NopCloser(bytes.NewReader(data)),
		openAsset: assets.Asset{
			ID:         "asset-1",
			IdentityID: assetAPIIdentityID,
			FileName:   "relazióne — 报告.pdf",
			MIMEType:   "image/svg+xml", // hostile sniffed mime — MUST NOT reach the serve header
			SizeBytes:  int64(len(data)),
		},
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(fake)

	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/download", nil), assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, data) {
		t.Fatalf("body = %q, want %q", got, data)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream (sniffed mime must not be trusted)", ct)
	}
	if ns := rec.Header().Get("X-Content-Type-Options"); ns != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", ns)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("Content-Disposition = %q, want attachment; prefix", cd)
	}
	if !strings.Contains(cd, "filename*=UTF-8''") {
		t.Fatalf("Content-Disposition = %q, want a filename*=UTF-8'' extended param", cd)
	}
	if cl := rec.Header().Get("Content-Length"); cl != strconv.Itoa(len(data)) {
		t.Fatalf("Content-Length = %q, want %d", cl, len(data))
	}
	if fake.openID != "asset-1" || fake.openIdentityID != assetAPIIdentityID {
		t.Fatalf("OpenForIdentity(id=%q, identity=%q), want asset-1 + bound identity", fake.openID, fake.openIdentityID)
	}
}

// TestAssetDownload_NonOwner is the T-IDOR mitigation proof: OpenForIdentity's ownership gate is
// folded into a WHERE id AND identity_id lookup (37A-01), so a not-owned id yields the SAME generic
// "not found" error as a truly non-existent id. The handler collapses that error to 404 —
// existence-hiding, never 403 and never 200 (D-12). Both cases produce an identical response, so an
// attacker cannot distinguish "exists but not yours" from "does not exist".
func TestAssetDownload_NonOwner(t *testing.T) {
	t.Parallel()
	// The single generic error the real WHERE-scoped lookup returns for both non-owned and absent.
	notFound := errors.New("asset not found")

	serve := func(id string) *httptest.ResponseRecorder {
		fake := &fakeAssetService{openErr: notFound}
		s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
		s.SetAssetService(fake)
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/"+id+"/download", nil), assetAPIIdentityID)
		rec := httptest.NewRecorder()
		s.Mux().ServeHTTP(rec, req)
		return rec
	}

	nonOwner := serve("someone-elses-asset")
	absent := serve("no-such-asset")

	if nonOwner.Code != http.StatusNotFound {
		t.Fatalf("non-owner status = %d, want 404 (existence-hiding — never 403/200)", nonOwner.Code)
	}
	if nonOwner.Code == http.StatusForbidden {
		t.Fatalf("non-owner returned 403 — that confirms existence; D-12 requires 404")
	}
	if nonOwner.Code != absent.Code || nonOwner.Body.String() != absent.Body.String() {
		t.Fatalf("non-owner (%d/%q) and absent (%d/%q) responses differ — existence leaked",
			nonOwner.Code, nonOwner.Body.String(), absent.Code, absent.Body.String())
	}
}

// TestAssetDownload_Unauth proves the route adds no unauthenticated surface: mounted inside
// registerAssetRoutes it inherits RequireAuth whole-origin, so a request with no session cookie is
// rejected 401 and never reaches the asset service (T-Unauth). The bare Server.Mux() serve in the
// owner test proves the route is actually registered (not a 404), so this 401 is the gate, not a miss.
func TestAssetDownload_Unauth(t *testing.T) {
	t.Parallel()
	deps := testDeps("operator-secret")
	fake := &fakeAssetService{}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(fake)
	gated := RequireAuth(s.Mux(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/download", nil)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no session)", rec.Code)
	}
	if fake.openID != "" {
		t.Fatalf("OpenForIdentity was reached without a session: id=%q", fake.openID)
	}
}

// ctxBlockingReadCloser models a Garage body whose Read blocks until the request context is
// cancelled (a client disconnect), then unblocks with the context error — exactly the D-09
// ctx-scoped stream contract. Close records that it ran so the test can assert the handler's
// defer rc.Close() fired.
type ctxBlockingReadCloser struct {
	ctx    context.Context
	once   sync.Once
	closed chan struct{}
}

func (b *ctxBlockingReadCloser) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *ctxBlockingReadCloser) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// TestAssetDownload_ClientDisconnect is the T-DoS mitigation proof: a client disconnect cancels
// the request context, the ctx-scoped read unblocks, io.Copy returns, and the deferred rc.Close()
// runs — with no leaked goroutine (goleak). This guards against an unbounded stream pinning a
// goroutine after the client is gone.
func TestAssetDownload_ClientDisconnect(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	brc := &ctxBlockingReadCloser{ctx: ctx, closed: make(chan struct{})}
	fake := &fakeAssetService{
		openResp:  brc,
		openAsset: assets.Asset{ID: "asset-1", IdentityID: assetAPIIdentityID, FileName: "big.bin", SizeBytes: 1 << 20},
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(fake)

	req := withPrincipal(
		httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/download", nil).WithContext(ctx),
		assetAPIIdentityID,
	)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Mux().ServeHTTP(rec, req)
	}()

	cancel() // simulate the client disconnect that cancels the request context

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after client disconnect — io.Copy stayed blocked (goroutine leak)")
	}
	select {
	case <-brc.closed:
	default:
		t.Fatal("rc.Close() did not run after client disconnect")
	}
}
