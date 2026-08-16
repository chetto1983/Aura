package agui

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/onboarding"
)

// profile_api.go serves the operator profile as something you can come BACK to:
//
//	GET /api/profile   (RequireAuth) — the stored answers, for the editor to render
//	PUT /api/profile   (RequireAuth) — the edited answers
//
// The onboarding seed form is a one-shot: it fires once, behind a gate, and asks six
// fields. Everything else the profile holds — expertise, projects, tone, the vetoes that
// ride the always-block — had no way in at all, and none of it had a way to CHANGE. A
// profile you cannot revise is a profile that is wrong from the first time you move house.
//
// Both routes act only on the caller's own identity: there is no id in the path and no way
// to name another one.

const (
	profileFieldMaxLen = 512
	// customInstructions is prose, not a label: it goes into the always-block verbatim and a
	// sentence or two is the point, so it gets more room than a field and far less than a
	// document.
	profileInstructionsMaxLen = 2000
	profileListMaxItems       = 32
	profileListItemMaxLen     = 256
)

// ProfileEditor is the consumer-side port for the stored profile.
// *onboarding.ProfileStore satisfies it as written.
type ProfileEditor interface {
	Load(ctx context.Context, identityID string) (onboarding.Answers, bool, error)
	// Replace, never Save: the editor renders every field, so it owns the whole row.
	Replace(ctx context.Context, identityID string, a onboarding.Answers) error
}

// SetProfileEditor wires the profile read/write model. Unwired → the routes answer 503.
func (s *Server) SetProfileEditor(editor ProfileEditor) { s.profiles = editor }

// ProfileDoc is the wire shape of the whole profile. It is the full Answers rather than the
// onboarding seed's six fields: the editor exists precisely to reach the rest.
//
// Voice/proactive are pointers so "not answered" survives the round trip — the form leaves
// them unset until the operator chooses, and false would answer for them.
type ProfileDoc struct {
	Name                string   `json:"name"`
	Role                string   `json:"role"`
	Company             string   `json:"company"`
	Location            string   `json:"location"`
	Timezone            string   `json:"timezone"`
	Lang                string   `json:"lang"`
	TonePreference      string   `json:"tonePreference"`
	ResponseLength      string   `json:"responseLength"`
	CustomInstructions  string   `json:"customInstructions"`
	VoiceMode           *bool    `json:"voiceMode,omitempty"`
	CanProactiveMessage *bool    `json:"canProactiveMessage,omitempty"`
	Expertise           []string `json:"expertise"`
	Stack               []string `json:"stack"`
	Projects            []string `json:"projects"`
	Goals               []string `json:"goals"`
	Interests           []string `json:"interests"`
	People              []string `json:"people"`
	Vetoes              []string `json:"vetoes"`
}

func (s *Server) registerProfileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/profile", s.handleProfileGet)
	mux.HandleFunc("PUT /api/profile", s.handleProfilePut)
}

func (s *Server) handleProfileGet(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.profileRequester(w, r)
	if !ok {
		return
	}
	// The found flag is not used: a missing row is an empty profile, not a 404 — the editor
	// renders the same blank form either way, and profileDocOf renders the zero Answers as
	// blank fields with empty lists.
	answers, _, err := s.profiles.Load(r.Context(), identityID)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	writeJSON(w, profileDocOf(answers))
}

// handleProfilePut replaces the stored profile with the submitted one.
//
// It is a full replace, not the onboarding path's merge (ProfileStore.Save): the editor
// renders every field, so a field arriving empty means the operator CLEARED it. Merging
// here would make deletion impossible — the one edit a "never do this" list most needs.
func (s *Server) handleProfilePut(w http.ResponseWriter, r *http.Request) {
	identityID, ok := s.profileRequester(w, r)
	if !ok {
		return
	}
	var doc ProfileDoc
	if err := strictDecodeJSON(w, r, &doc, decodeOpts{}); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateProfileDoc(doc); err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	if err := s.profiles.Replace(r.Context(), identityID, answersOf(doc)); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	writeJSON(w, profileDocOf(answersOf(doc)))
}

func (s *Server) profileRequester(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.profiles == nil {
		http.Error(w, "profile store not configured", http.StatusServiceUnavailable)
		return "", false
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return identityID, true
}

func profileDocOf(a onboarding.Answers) ProfileDoc {
	return ProfileDoc{
		Name: a.Name, Role: a.Role, Company: a.Company, Location: a.Location,
		Timezone: a.Timezone, Lang: a.Lang, TonePreference: a.TonePreference,
		ResponseLength: a.ResponseLength, CustomInstructions: a.CustomInstructions,
		VoiceMode: a.VoiceMode, CanProactiveMessage: a.CanProactiveMessage,
		Expertise: nonNil(a.Expertise), Stack: nonNil(a.Stack), Projects: nonNil(a.Projects),
		Goals: nonNil(a.Goals), Interests: nonNil(a.Interests), People: nonNil(a.People),
		Vetoes: nonNil(a.Vetoes),
	}
}

func answersOf(d ProfileDoc) onboarding.Answers {
	return onboarding.Answers{
		Name: strings.TrimSpace(d.Name), Role: strings.TrimSpace(d.Role),
		Company: strings.TrimSpace(d.Company), Location: strings.TrimSpace(d.Location),
		Timezone: strings.TrimSpace(d.Timezone), Lang: strings.TrimSpace(d.Lang),
		TonePreference:     strings.TrimSpace(d.TonePreference),
		ResponseLength:     strings.TrimSpace(d.ResponseLength),
		CustomInstructions: strings.TrimSpace(d.CustomInstructions),
		VoiceMode:          d.VoiceMode, CanProactiveMessage: d.CanProactiveMessage,
		Expertise: cleanList(d.Expertise), Stack: cleanList(d.Stack),
		Projects: cleanList(d.Projects), Goals: cleanList(d.Goals),
		Interests: cleanList(d.Interests), People: cleanList(d.People),
		Vetoes: cleanList(d.Vetoes),
	}
}

// nonNil keeps a nil slice off the wire as [] rather than null, so the editor can bind it
// to a list input without a null check per field.
func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// cleanList drops the blank rows an editable list UI leaves behind. A blank entry would
// render as a trailing separator in the always-block, which the model imitates.
func cleanList(v []string) []string {
	kept := make([]string, 0, len(v))
	for _, item := range v {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return kept
}

// validateProfileDoc bounds everything before it reaches the always-block. The block is
// injected into messages[1] on EVERY turn, so an unbounded field is not a storage problem
// but a per-turn token cost the operator would pay forever.
func validateProfileDoc(d ProfileDoc) error {
	for _, value := range []string{
		d.Name, d.Role, d.Company, d.Location, d.Timezone, d.Lang,
		d.TonePreference, d.ResponseLength,
	} {
		if len(value) > profileFieldMaxLen {
			return errors.New("profile: field too long")
		}
	}
	if len(d.CustomInstructions) > profileInstructionsMaxLen {
		return errors.New("profile: custom instructions too long")
	}
	for _, list := range [][]string{
		d.Expertise, d.Stack, d.Projects, d.Goals, d.Interests, d.People, d.Vetoes,
	} {
		if len(list) > profileListMaxItems {
			return errors.New("profile: too many entries in a list")
		}
		for _, item := range list {
			if len(item) > profileListItemMaxLen {
				return errors.New("profile: list entry too long")
			}
		}
	}
	return nil
}
