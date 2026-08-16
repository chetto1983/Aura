package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/onboarding"
)

// fakeProfileEditor is an in-memory ProfileEditor. loadErr/saveErr drive the degraded
// branches; saved records exactly what the handler passed down, which is where the
// trim/clean contract is provable.
type fakeProfileEditor struct {
	stored  onboarding.Answers
	found   bool
	saved   []onboarding.Answers
	loadErr error
	saveErr error
}

func (f *fakeProfileEditor) Load(context.Context, string) (onboarding.Answers, bool, error) {
	if f.loadErr != nil {
		return onboarding.Answers{}, false, f.loadErr
	}
	return f.stored, f.found, nil
}

func (f *fakeProfileEditor) Replace(_ context.Context, _ string, a onboarding.Answers) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, a)
	return nil
}

func profileReq(t *testing.T, method string, body any, principal string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, "/api/profile", reader)
	if principal != "" {
		r = withPrincipal(r, principal)
	}
	return httptest.NewRecorder(), r
}

// An operator who never filled the form gets an empty profile, not a 404: the editor
// renders the same blank fields either way, and a 404 reads as a broken route.
func TestProfileGetMissingRowIsAnEmptyProfile(t *testing.T) {
	s := &Server{profiles: &fakeProfileEditor{}}
	rr, r := profileReq(t, http.MethodGet, nil, "op-1")

	s.handleProfileGet(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var doc ProfileDoc
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Name != "" || doc.VoiceMode != nil {
		t.Errorf("empty profile came back populated: %+v", doc)
	}
	// Lists ship as [] rather than null so the editor can bind them without a null check.
	if !strings.Contains(rr.Body.String(), `"vetoes":[]`) {
		t.Errorf("lists must serialize as [], got %s", rr.Body.String())
	}
}

func TestProfileGetReturnsStoredAnswers(t *testing.T) {
	voice := true
	editor := &fakeProfileEditor{found: true, stored: onboarding.Answers{
		Name: "Davide", Timezone: "Europe/Rome", VoiceMode: &voice,
		Vetoes: []string{"non scrivere email al mio posto"},
	}}
	s := &Server{profiles: editor}
	rr, r := profileReq(t, http.MethodGet, nil, "op-1")

	s.handleProfileGet(rr, r)

	var doc ProfileDoc
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Name != "Davide" || doc.Timezone != "Europe/Rome" {
		t.Errorf("stored answers not returned: %+v", doc)
	}
	if doc.VoiceMode == nil || !*doc.VoiceMode {
		t.Errorf("VoiceMode = %v, want true", doc.VoiceMode)
	}
	if len(doc.Vetoes) != 1 {
		t.Errorf("Vetoes = %v, want the stored one", doc.Vetoes)
	}
}

// The edit is a REPLACE: a field the operator cleared must reach the store empty. This is
// the one behaviour the onboarding path cannot provide (it merges, so nothing can ever be
// removed) and it is the reason this route exists.
func TestProfilePutClearsFieldsAndTrims(t *testing.T) {
	editor := &fakeProfileEditor{found: true, stored: onboarding.Answers{
		Name: "Davide", Company: "Aura", Vetoes: []string{"vecchio veto"},
	}}
	s := &Server{profiles: editor}
	rr, r := profileReq(t, http.MethodPut, ProfileDoc{
		Name:      "  Davide  ",
		Company:   "",
		Expertise: []string{"Go", "  ", ""},
		Vetoes:    []string{},
	}, "op-1")

	s.handleProfilePut(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if len(editor.saved) != 1 {
		t.Fatalf("saves = %d, want 1", len(editor.saved))
	}
	got := editor.saved[0]
	if got.Name != "Davide" {
		t.Errorf("Name = %q, want it trimmed", got.Name)
	}
	if got.Company != "" {
		t.Errorf("Company = %q, want the cleared value to reach the store", got.Company)
	}
	if len(got.Expertise) != 1 || got.Expertise[0] != "Go" {
		t.Errorf("Expertise = %v, want the blank rows dropped", got.Expertise)
	}
	if len(got.Vetoes) != 0 {
		t.Errorf("Vetoes = %v, want emptied", got.Vetoes)
	}
}

// Everything here rides messages[1] on every turn, so an unbounded field is a permanent
// per-turn token cost, not a storage question.
func TestProfilePutRejectsOversizedInput(t *testing.T) {
	cases := map[string]ProfileDoc{
		"field":        {Name: strings.Repeat("a", profileFieldMaxLen+1)},
		"instructions": {CustomInstructions: strings.Repeat("a", profileInstructionsMaxLen+1)},
		"list length":  {Stack: make([]string, profileListMaxItems+1)},
		"list entry":   {Goals: []string{strings.Repeat("a", profileListItemMaxLen+1)}},
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			editor := &fakeProfileEditor{}
			s := &Server{profiles: editor}
			rr, r := profileReq(t, http.MethodPut, doc, "op-1")

			s.handleProfilePut(rr, r)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rr.Code)
			}
			if len(editor.saved) != 0 {
				t.Fatalf("oversized profile persisted: %+v", editor.saved)
			}
		})
	}
}

func TestProfileRoutesDegradedPaths(t *testing.T) {
	t.Run("unwired 503", func(t *testing.T) {
		s := &Server{}
		rr, r := profileReq(t, http.MethodGet, nil, "op-1")
		s.handleProfileGet(rr, r)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rr.Code)
		}
	})

	t.Run("anonymous 401", func(t *testing.T) {
		s := &Server{profiles: &fakeProfileEditor{}}
		rr, r := profileReq(t, http.MethodPut, ProfileDoc{Name: "x"}, "")
		s.handleProfilePut(rr, r)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("malformed body 400", func(t *testing.T) {
		editor := &fakeProfileEditor{}
		s := &Server{profiles: editor}
		r := withPrincipal(httptest.NewRequest(http.MethodPut, "/api/profile", strings.NewReader("{oops")), "op-1")
		rr := httptest.NewRecorder()
		s.handleProfilePut(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rr.Code)
		}
		if len(editor.saved) != 0 {
			t.Fatal("a malformed body reached the store")
		}
	})

	t.Run("store failure 502", func(t *testing.T) {
		for name, tc := range map[string]struct {
			editor *fakeProfileEditor
			call   func(*Server, *httptest.ResponseRecorder, *http.Request)
			method string
		}{
			"load": {
				editor: &fakeProfileEditor{loadErr: errors.New("db down")},
				call:   func(s *Server, rr *httptest.ResponseRecorder, r *http.Request) { s.handleProfileGet(rr, r) },
				method: http.MethodGet,
			},
			"save": {
				editor: &fakeProfileEditor{saveErr: errors.New("db down")},
				call:   func(s *Server, rr *httptest.ResponseRecorder, r *http.Request) { s.handleProfilePut(rr, r) },
				method: http.MethodPut,
			},
		} {
			t.Run(name, func(t *testing.T) {
				s := &Server{profiles: tc.editor}
				var body any
				if tc.method == http.MethodPut {
					body = ProfileDoc{Name: "Davide"}
				}
				rr, r := profileReq(t, tc.method, body, "op-1")
				tc.call(s, rr, r)
				if rr.Code != http.StatusBadGateway {
					t.Fatalf("status = %d, want 502", rr.Code)
				}
			})
		}
	})
}
