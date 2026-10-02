package openai_compat

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDirectPlanAdmissionErrorRetainsDetailAndRequestID(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(`{"detail":"Enable plan usage in ChatGPT Settings"}`))}
	response.Header.Set("x-request-id", "req-plan")
	err := newHTTPError(response)
	if !strings.Contains(err.Error(), "Enable plan usage") || err.RequestID != "req-plan" {
		t.Fatalf("direct admission reason or support reference lost: %v", err)
	}
	response.Header.Set("x-request-id", strings.Repeat("a", 200)+"\n injected")
	if got := responseRequestID(response); len(got) > 128 || strings.Contains(got, "\n") {
		t.Fatal("request identifier is not bounded to one line")
	}
}
