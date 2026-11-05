package agui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/pimwatch"
)

// pim_watch_api.go serves the mail and calendar watch to the cockpit (spec 2026-10-31 §5.2): the
// switch on an account's card, the day's decisions read live by id, corrections, rules, labels and
// the digest's settings. The watch is the authenticated principal's own, so no route takes an
// identity. Each write is a thin call into the one service the mail_watch tool also uses.

// mailWatchBodyMaxBytes bounds a request body: a label's name or a rule's key.
const mailWatchBodyMaxBytes = 4 << 10

// mailWatchService is the seam over *pimwatch.Service, declared at the consumer per D-A2-02.
type mailWatchService interface {
	Status(ctx context.Context, identityID string) (pimwatch.Status, error)
	Enable(ctx context.Context, identityID, accountID string) error
	Disable(ctx context.Context, identityID, accountID string) error
	RemoveAccount(ctx context.Context, identityID, accountID string) error
	Decisions(ctx context.Context, identityID, accountID, day string, discarded bool) (pimwatch.DayDecisions, error)
	Correct(ctx context.Context, identityID string, c pimwatch.Correction) (pimwatch.Verdict, *pimwatch.WrittenRule, error)
	Rules(ctx context.Context, identityID string) ([]pimwatch.Rule, error)
	AddRule(ctx context.Context, identityID string, in pimwatch.RuleInput) (pimwatch.WrittenRule, error)
	RemoveRule(ctx context.Context, identityID, ruleID string) error
	Labels(ctx context.Context, identityID string) ([]pimwatch.Label, error)
	AddLabel(ctx context.Context, identityID string, in pimwatch.LabelInput) (pimwatch.Label, error)
	UpdateLabel(ctx context.Context, identityID, labelID string, in pimwatch.LabelPatch) (pimwatch.Label, error)
	RemoveLabel(ctx context.Context, identityID, labelID string) error
	SaveSettings(ctx context.Context, identityID string, digestHour, dailyCap int) (pimwatch.Settings, error)
}

// SetMailWatch wires the watch. Until set, the routes answer 503.
func (s *Server) SetMailWatch(service mailWatchService) { s.mailWatch = service }

func (s *Server) registerMailWatchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/mail-watch", s.mailWatchRoute(s.handleMailWatchStatus))
	mux.HandleFunc("PUT /api/mail-watch/settings", s.mailWatchRoute(s.handleMailWatchSettings))
	mux.HandleFunc("POST /api/mail-watch/accounts/{id}/enable", s.mailWatchRoute(s.handleMailWatchEnable))
	mux.HandleFunc("POST /api/mail-watch/accounts/{id}/disable", s.mailWatchRoute(s.handleMailWatchDisable))
	mux.HandleFunc("GET /api/mail-watch/accounts/{id}/decisions", s.mailWatchRoute(s.handleMailWatchDecisions))
	mux.HandleFunc("POST /api/mail-watch/accounts/{id}/decisions/correct", s.mailWatchRoute(s.handleMailWatchCorrect))
	mux.HandleFunc("GET /api/mail-watch/rules", s.mailWatchRoute(s.handleMailWatchRules))
	mux.HandleFunc("POST /api/mail-watch/rules", s.mailWatchRoute(s.handleMailWatchAddRule))
	mux.HandleFunc("DELETE /api/mail-watch/rules/{id}", s.mailWatchRoute(s.handleMailWatchRemoveRule))
	mux.HandleFunc("GET /api/mail-watch/labels", s.mailWatchRoute(s.handleMailWatchLabels))
	mux.HandleFunc("POST /api/mail-watch/labels", s.mailWatchRoute(s.handleMailWatchAddLabel))
	mux.HandleFunc("PUT /api/mail-watch/labels/{id}", s.mailWatchRoute(s.handleMailWatchUpdateLabel))
	mux.HandleFunc("DELETE /api/mail-watch/labels/{id}", s.mailWatchRoute(s.handleMailWatchRemoveLabel))
}

// mailWatchHandler is a route with the service checked and the caller's identity resolved.
type mailWatchHandler func(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string)

func (s *Server) mailWatchRoute(h mailWatchHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.mailWatch == nil {
			writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "mail_watch_unavailable"})
			return
		}
		h(w, r, s.mailWatch, scopedIdentityID(r.Context()))
	}
}

// writeMailWatchError keeps the wire to fixed words. A refusal answers its fixed code, which the
// cockpit translates, beside the sentence that names what to fix, and a PIM failure answers its
// fixed code. Anything else stays in the log, sanitized, because it can name the database or the
// sidecar.
func writeMailWatchError(w http.ResponseWriter, err error) {
	pimErr, isPIM := errors.AsType[*pimwatch.PIMError](err)
	refusal, isRefusal := errors.AsType[*pimwatch.InvalidError](err)
	switch {
	case errors.Is(err, pimwatch.ErrUnavailable):
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "mail_watch_unavailable", "reason": pimwatch.UnavailableReason})
	case isRefusal:
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": refusal.Code, "message": refusal.Message})
	case errors.Is(err, pimwatch.ErrNotFound):
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case isPIM:
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": pimErr.Code})
	default:
		slog.Warn("agui: mail watch", "err", sanitizeErr(err))
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "mail_watch_failed"})
	}
}

// mailWatchTime is a time for the wire, null when it never happened.
func mailWatchTime(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	return &at
}

type mailWatchSettingsJSON struct {
	DigestHour      int        `json:"digest_hour"`
	DailyAlertCap   int        `json:"daily_alert_cap"`
	IntervalMinutes int        `json:"interval_minutes"`
	LastDigestAt    *time.Time `json:"last_digest_at"`
}

func mailWatchSettings(settings pimwatch.Settings) mailWatchSettingsJSON {
	return mailWatchSettingsJSON{
		DigestHour: settings.DigestHour, DailyAlertCap: settings.DailyAlertCap,
		IntervalMinutes: settings.IntervalMinutes, LastDigestAt: mailWatchTime(settings.LastDigestAt),
	}
}

type mailWatchResourceJSON struct {
	Resource     string     `json:"resource"`
	Status       string     `json:"status"`
	LastError    string     `json:"last_error,omitempty"`
	FailingSince *time.Time `json:"failing_since"`
	AdvancedAt   *time.Time `json:"advanced_at"`
}

type mailWatchAccountJSON struct {
	AccountID    string                  `json:"account_id"`
	Paused       bool                    `json:"paused"`
	PausedReason string                  `json:"paused_reason,omitempty"`
	NextRunAt    *time.Time              `json:"next_run_at"`
	AlertsToday  int                     `json:"alerts_today"`
	Resources    []mailWatchResourceJSON `json:"resources"`
}

type mailWatchStatusJSON struct {
	Configured bool                   `json:"configured"`
	Reason     string                 `json:"reason,omitempty"`
	Settings   mailWatchSettingsJSON  `json:"settings"`
	Accounts   []mailWatchAccountJSON `json:"accounts"`
}

func (s *Server) handleMailWatchStatus(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	status, err := service.Status(r.Context(), identityID)
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	out := mailWatchStatusJSON{
		Configured: status.Configured, Settings: mailWatchSettings(status.Settings),
		Accounts: make([]mailWatchAccountJSON, 0, len(status.Accounts)),
	}
	if !status.Configured {
		out.Reason = pimwatch.UnavailableReason
	}
	for _, account := range status.Accounts {
		resources := make([]mailWatchResourceJSON, 0, len(account.Resources))
		for _, resource := range account.Resources {
			resources = append(resources, mailWatchResourceJSON{
				Resource: resource.Resource, Status: resource.Status, LastError: resource.LastError,
				FailingSince: mailWatchTime(resource.FailingSince), AdvancedAt: mailWatchTime(resource.AdvancedAt),
			})
		}
		out.Accounts = append(out.Accounts, mailWatchAccountJSON{
			AccountID: account.AccountID, Paused: account.Paused, PausedReason: account.PausedReason,
			NextRunAt: mailWatchTime(account.NextRunAt), AlertsToday: account.AlertsToday, Resources: resources,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleMailWatchSettings(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	var body struct {
		DigestHour    *int `json:"digest_hour"`
		DailyAlertCap *int `json:"daily_alert_cap"`
	}
	if !decodeMailWatchBody(w, r, &body) {
		return
	}
	if body.DigestHour == nil || body.DailyAlertCap == nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "digest_hour and daily_alert_cap are required"})
		return
	}
	settings, err := service.SaveSettings(r.Context(), identityID, *body.DigestHour, *body.DailyAlertCap)
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, mailWatchSettings(settings))
}

func decodeMailWatchBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := strictDecodeJSON(w, r, dst, decodeOpts{maxBytes: mailWatchBodyMaxBytes}); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return false
	}
	return true
}

func (s *Server) handleMailWatchEnable(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	if err := service.Enable(r.Context(), identityID, r.PathValue("id")); err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"enabled": true})
}

func (s *Server) handleMailWatchDisable(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	if err := service.Disable(r.Context(), identityID, r.PathValue("id")); err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"enabled": false})
}

// mailWatchDecisionJSON is one line of the decisions view. From, FromName and Subject are a
// stranger's text, read live and kept nowhere, for the cockpit to render as text.
type mailWatchDecisionJSON struct {
	AccountID  string     `json:"account_id"`
	Resource   string     `json:"resource"`
	ItemID     string     `json:"item_id"`
	CalendarID string     `json:"calendar_id,omitempty"`
	From       string     `json:"from"`
	FromName   string     `json:"from_name"`
	Subject    string     `json:"subject"`
	Start      *time.Time `json:"start"`
	AllDay     bool       `json:"all_day,omitempty"`
	Gone       bool       `json:"gone"`
	AuthFailed bool       `json:"auth_failed"`
	Tier       string     `json:"tier"`
	Change     string     `json:"change,omitempty"`
	LabelID    string     `json:"label_id,omitempty"`
	Label      string     `json:"label,omitempty"`
	DecidedBy  string     `json:"decided_by"`
	DecidedAt  time.Time  `json:"decided_at"`
	// CentroidTier is the urgency centroid's tier where it differs from the ladder's, in shadow.
	CentroidTier string `json:"centroid_tier,omitempty"`
	Discarded    bool   `json:"discarded,omitempty"`
}

// mailWatchDayJSON is the decisions of the day it names. Today is the identity's today, which the
// browser may not share, so the cockpit heads the day with this one. Discarded counts the day's
// discarded mail, whichever list Decisions is.
type mailWatchDayJSON struct {
	Day       string                  `json:"day"`
	Decisions []mailWatchDecisionJSON `json:"decisions"`
	Discarded int                     `json:"discarded"`
}

func (s *Server) handleMailWatchDecisions(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	query := r.URL.Query()
	day, err := service.Decisions(r.Context(), identityID, r.PathValue("id"), query.Get("day"), query.Get("discarded") == "1")
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	out := mailWatchDayJSON{Day: day.Day, Decisions: make([]mailWatchDecisionJSON, 0, len(day.Lines)), Discarded: day.Discarded}
	for _, line := range day.Lines {
		out.Decisions = append(out.Decisions, mailWatchDecisionJSON{
			AccountID: line.AccountID, Resource: line.Resource, ItemID: line.ItemID, CalendarID: line.CalendarID,
			From: line.From, FromName: line.FromName, Subject: line.Subject,
			Start: mailWatchTime(line.Start), AllDay: line.AllDay, Gone: line.Gone, AuthFailed: line.AuthFailed,
			Tier: string(line.Tier), Change: string(line.Change), LabelID: line.LabelID, Label: line.Label,
			DecidedBy: string(line.By), DecidedAt: line.DecidedAt, CentroidTier: string(line.Centroid),
			Discarded: line.Discarded,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleMailWatchCorrect(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	var body struct {
		Resource string  `json:"resource"`
		ItemID   string  `json:"item_id"`
		Tier     string  `json:"tier"`
		LabelID  *string `json:"label_id"`
	}
	if !decodeMailWatchBody(w, r, &body) {
		return
	}
	correction := pimwatch.Correction{
		AccountID: r.PathValue("id"), Resource: body.Resource, ItemID: body.ItemID,
		Tier: pimwatch.Tier(body.Tier), LabelID: body.LabelID,
	}
	if correction.Resource == "" {
		correction.Resource = pimwatch.ResourceMail
	}
	verdict, rule, err := service.Correct(r.Context(), identityID, correction)
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	out := mailWatchCorrectedJSON{Tier: string(verdict.Tier), LabelID: verdict.LabelID, Discarded: verdict.Discarded}
	if rule != nil {
		learned := mailWatchRule(rule.Rule)
		out.Rule, out.RuleCreated = &learned, rule.Created
	}
	writeJSON(w, out)
}

// mailWatchCorrectedJSON is a verdict after a correction. Rule is set when this correction taught
// one, so the cockpit can say so, and RuleCreated when that rule is new: only then can it be
// undone, because one merged into a rule on file holds the person's earlier words.
type mailWatchCorrectedJSON struct {
	Tier        string             `json:"tier"`
	LabelID     string             `json:"label_id,omitempty"`
	Discarded   bool               `json:"discarded,omitempty"`
	Rule        *mailWatchRuleJSON `json:"rule,omitempty"`
	RuleCreated bool               `json:"rule_created,omitempty"`
}

type mailWatchRuleJSON struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	KeyKind   string    `json:"key_kind"`
	Tier      string    `json:"tier,omitempty"`
	LabelID   string    `json:"label_id,omitempty"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

// mailWatchSavedRuleJSON is a rule the person wrote. RuleCreated says its key had none: only then can
// the cockpit undo it by removing it, because one that replaced another holds the person's earlier
// words.
type mailWatchSavedRuleJSON struct {
	mailWatchRuleJSON
	RuleCreated bool `json:"rule_created,omitempty"`
}

func mailWatchRule(rule pimwatch.Rule) mailWatchRuleJSON {
	return mailWatchRuleJSON{
		ID: rule.ID, Key: rule.Key, KeyKind: string(rule.Kind), Tier: string(rule.Tier),
		LabelID: rule.LabelID, Source: rule.Source, CreatedAt: rule.CreatedAt,
	}
}

func (s *Server) handleMailWatchRules(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	rules, err := service.Rules(r.Context(), identityID)
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	out := make([]mailWatchRuleJSON, 0, len(rules))
	for _, rule := range rules {
		out = append(out, mailWatchRule(rule))
	}
	writeJSON(w, out)
}

func (s *Server) handleMailWatchAddRule(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	var body struct {
		Key     string `json:"key"`
		Tier    string `json:"tier"`
		LabelID string `json:"label_id"`
	}
	if !decodeMailWatchBody(w, r, &body) {
		return
	}
	rule, err := service.AddRule(r.Context(), identityID, pimwatch.RuleInput{Key: body.Key, Tier: pimwatch.Tier(body.Tier), LabelID: body.LabelID})
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, mailWatchSavedRuleJSON{mailWatchRuleJSON: mailWatchRule(rule.Rule), RuleCreated: rule.Created})
}

func (s *Server) handleMailWatchRemoveRule(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	if err := service.RemoveRule(r.Context(), identityID, r.PathValue("id")); err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"removed": true})
}

type mailWatchLabelJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Tier        string `json:"tier,omitempty"`
	Position    int    `json:"position"`
	Description string `json:"description"`
	Discard     bool   `json:"discard"`
}

func mailWatchLabel(label pimwatch.Label) mailWatchLabelJSON {
	return mailWatchLabelJSON{
		ID: label.ID, Name: label.Name, Tier: string(label.Tier), Position: label.Position,
		Description: label.Description, Discard: label.Discard,
	}
}

func (s *Server) handleMailWatchLabels(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	labels, err := service.Labels(r.Context(), identityID)
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	out := make([]mailWatchLabelJSON, 0, len(labels))
	for _, label := range labels {
		out = append(out, mailWatchLabel(label))
	}
	writeJSON(w, out)
}

// mailWatchLabelBody is a new label: its tier may be empty, and so may its description.
type mailWatchLabelBody struct {
	Name        string `json:"name"`
	Tier        string `json:"tier"`
	Description string `json:"description"`
	Discard     bool   `json:"discard"`
}

// mailWatchLabelPatchBody is a label's PUT: a field the page does not send keeps its value.
type mailWatchLabelPatchBody struct {
	Name        *string `json:"name"`
	Tier        *string `json:"tier"`
	Description *string `json:"description"`
	Discard     *bool   `json:"discard"`
}

func (s *Server) handleMailWatchAddLabel(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	var body mailWatchLabelBody
	if !decodeMailWatchBody(w, r, &body) {
		return
	}
	label, err := service.AddLabel(r.Context(), identityID, pimwatch.LabelInput{
		Name: body.Name, Tier: pimwatch.Tier(body.Tier), Description: body.Description, Discard: body.Discard,
	})
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, mailWatchLabel(label))
}

func (s *Server) handleMailWatchUpdateLabel(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	var body mailWatchLabelPatchBody
	if !decodeMailWatchBody(w, r, &body) {
		return
	}
	patch := pimwatch.LabelPatch{Name: body.Name, Description: body.Description, Discard: body.Discard}
	if body.Tier != nil {
		tier := pimwatch.Tier(*body.Tier)
		patch.Tier = &tier
	}
	label, err := service.UpdateLabel(r.Context(), identityID, r.PathValue("id"), patch)
	if err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, mailWatchLabel(label))
}

func (s *Server) handleMailWatchRemoveLabel(w http.ResponseWriter, r *http.Request, service mailWatchService, identityID string) {
	if err := service.RemoveLabel(r.Context(), identityID, r.PathValue("id")); err != nil {
		writeMailWatchError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"removed": true})
}
