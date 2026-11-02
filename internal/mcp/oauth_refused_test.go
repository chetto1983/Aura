package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/chetto1983/aura/internal/identityctx"
)

type refusalTokenSource struct{ err error }

func (*fakeStore) MarkRefused(context.Context, string) error              { return nil }
func (*rotatingGrantStore) MarkRefused(context.Context, string) error     { return nil }
func (*identityRecordingStore) MarkRefused(context.Context, string) error { return nil }

func (s refusalTokenSource) Token() (*oauth2.Token, error) { return nil, s.err }

type refusalStore struct {
	fakeStore
	refused []string
	err     error
}

func (s *refusalStore) MarkRefused(_ context.Context, name string) error {
	s.refused = append(s.refused, name)
	return s.err
}

func TestARefreshAnsweredInvalidGrantMarksTheGrantRefused(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrapped=%v", wrapped), func(t *testing.T) {
			var failure error = &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
			if wrapped {
				failure = fmt.Errorf("refresh: %w", failure)
			}
			store := &refusalStore{}
			source := persistingTokenSource{ctx: identityctx.WithIdentityID(t.Context(), t.Name()),
				inner: refusalTokenSource{failure}, store: store, logger: slog.Default(), name: "canva"}
			token, err := source.Token()
			if token != nil || !errors.Is(err, failure) {
				t.Fatalf("Token = %v, %v; want original refresh error", token, err)
			}
			if !slices.Equal(store.refused, []string{"canva"}) {
				t.Fatalf("refused = %v, want canva", store.refused)
			}
		})
	}
}

func TestATransientRefreshFailureLeavesTheGrantAlone(t *testing.T) {
	for _, failure := range []error{
		&oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable"}},
		errors.New("network unreachable"),
	} {
		store := &refusalStore{}
		source := persistingTokenSource{ctx: t.Context(), inner: refusalTokenSource{failure}, store: store,
			logger: slog.Default(), name: "canva"}
		if _, err := source.Token(); !errors.Is(err, failure) {
			t.Fatalf("Token error = %v, want %v", err, failure)
		}
		if len(store.refused) != 0 {
			t.Fatalf("transient failure marked refused: %v", store.refused)
		}
	}
}

func TestARefusalStorageFailurePreservesTheTokenError(t *testing.T) {
	failure := &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
	store := &refusalStore{err: errors.New("refusal storage unavailable")}
	var logs bytes.Buffer
	source := persistingTokenSource{ctx: t.Context(), inner: refusalTokenSource{failure}, store: store,
		logger: slog.New(slog.NewTextHandler(&logs, nil)), name: "canva"}
	if _, err := source.Token(); err != failure {
		t.Fatalf("Token error = %v, want original invalid_grant", err)
	}
	if !strings.Contains(logs.String(), "refusal storage unavailable") {
		t.Fatalf("missing persistence warning: %s", logs.String())
	}
}
