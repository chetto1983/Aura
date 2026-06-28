package multimodal

import (
	"net/http"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestStatusError(t *testing.T) {
	e := &StatusError{Endpoint: "stt", StatusCode: 503}
	if got, want := e.Error(), "multimodal sidecar stt: HTTP 503"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestSetBearerOnlyWhenKey(t *testing.T) {
	t.Run("empty key sets no header", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "http://x", nil)
		setBearer(req, "")
		if got := req.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty for local route", got)
		}
	})
	t.Run("non-empty key sets bearer", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "http://x", nil)
		setBearer(req, "shared-key")
		if got, want := req.Header.Get("Authorization"), "Bearer shared-key"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
	})
}

func TestTimeoutContextDefault(t *testing.T) {
	ctx, cancel := TimeoutContext(t.Context(), 0)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("TimeoutContext(0) must carry a deadline (defaultTimeoutSec)")
	}
}
