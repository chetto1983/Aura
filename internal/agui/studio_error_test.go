package agui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/mediagen"
)

// The Studio's refusals, kept apart from the handler tests: the status table is the contract a
// cockpit client branches on, and it is read as a table rather than hunted for among the
// handlers. It shares studio_api_test.go's fake backend and helpers.

func TestStudioMapsRefusals(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		// Every code in the plan's binding table, so a mapping cannot regress unseen: the four
		// that were here covered three of the four statuses and left five codes unpinned.
		{"unsupported", &mediagen.Error{Code: "unsupported", Message: "That model takes no end frame."}, http.StatusUnprocessableEntity, "unsupported"},
		{"model rejected", &mediagen.Error{Code: "model_rejected", Message: "The model refused the request."}, http.StatusUnprocessableEntity, "model_rejected"},
		{"asset not found", &mediagen.Error{Code: "asset_not_found", Message: "That image is not yours."}, http.StatusUnprocessableEntity, "asset_not_found"},
		{"too large", &mediagen.Error{Code: "too_large", Message: "That image is too large."}, http.StatusUnprocessableEntity, "too_large"},
		{"content blocked", &mediagen.Error{Code: "content_blocked", Message: "The provider blocked the prompt."}, http.StatusUnprocessableEntity, "content_blocked"},
		{"no key", &mediagen.Error{Code: "no_key", Message: "No OpenRouter key."}, http.StatusConflict, "no_key"},
		{"no credit", &mediagen.Error{Code: "no_credit", Message: "Out of credit."}, http.StatusPaymentRequired, "no_credit"},
		{"job failed", &mediagen.Error{Code: "job_failed", Message: "The job failed."}, http.StatusBadGateway, "job_failed"},
		{"outcome unknown", &mediagen.Error{Code: "outcome_unknown", Message: "The outcome is unknown."}, http.StatusBadGateway, "outcome_unknown"},
		{"wrong modality", assets.ErrWrongModality, http.StatusUnprocessableEntity, "unsupported"},
		{"local route", ErrCatalogLocalRoute, http.StatusConflict, "local_route"},
		{"no row", pgx.ErrNoRows, http.StatusNotFound, "not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeStudioBackend{videoErr: tc.err}
			rec := serveStudio(t, studioServer(t, backend), http.MethodPost, "/api/studio/videos", `{"model":"m","prompt":"p"}`)
			if rec.Code != tc.status {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body.String(), tc.status)
			}
			var out struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode body %q: %v", rec.Body.String(), err)
			}
			if out.Code != tc.code {
				t.Fatalf("code = %q, want %q", out.Code, tc.code)
			}
			if out.Error == "" {
				t.Fatalf("refusal carried no message: %q", rec.Body.String())
			}
		})
	}

	t.Run("a plain failure says nothing about itself", func(t *testing.T) {
		backend := &fakeStudioBackend{videoErr: errors.New("dial tcp 10.0.0.9:5432: connection refused, password=hunter2")}
		rec := serveStudio(t, studioServer(t, backend), http.MethodPost, "/api/studio/videos", `{"model":"m","prompt":"p"}`)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		body := rec.Body.String()
		for _, leaked := range []string{"hunter2", "10.0.0.9", "connection refused", "dial tcp"} {
			if strings.Contains(body, leaked) {
				t.Fatalf("body echoed the cause %q: %s", leaked, body)
			}
		}
	})
}
