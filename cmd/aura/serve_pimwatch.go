// serve_pimwatch.go assembles the mail and calendar watch at the composition root: the scheduler,
// the Telegram link, the profile and the PIM's OAuth grant are all here, and internal/pimwatch
// knows none of them.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/cron/handlers"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/pimwatch"
)

// pimWatchRuntime is the watch as serve boot uses it. Service exists whenever Postgres does, so
// the cockpit can always say what is wrong; the handlers' ports stay nil without a PIM to read.
type pimWatchRuntime struct {
	service  *pimwatch.Service
	watcher  handlers.PIMWatcher
	digester handlers.PIMDigester
	sweep    handlers.Sweeper
	locale   pimwatch.Locale
}

// mcpAuth is the process's one MCP authorization service, built on first use because the
// dispatcher needs it before the AG-UI server does. Nil is not a failure (newMCPAuthService).
func (c *chatEnv) mcpAuth() *mcpAuthService {
	if c.mcpAuthBuilt {
		return c.mcpAuthSvc
	}
	c.mcpAuthBuilt = true
	service, err := newMCPAuthService(c.cfg, c.pool, slog.Default())
	if err != nil {
		slog.Warn("mcp authorization unavailable", "error", err)
		return nil
	}
	c.mcpAuthSvc = service
	return service
}

// pimWatch is the process's one watch, memoized as buildDeprovisioner is: the dispatcher and the
// AG-UI server must hold the same service.
func (c *chatEnv) pimWatch() *pimWatchRuntime {
	if c.watch == nil {
		c.watch = newPIMWatchRuntime(c)
	}
	return c.watch
}

func newPIMWatchRuntime(chat *chatEnv) *pimWatchRuntime {
	if chat.pool == nil || chat.cfg == nil {
		return &pimWatchRuntime{}
	}
	store := pimwatch.NewStore(chat.pool)
	locale := pimWatchLocale{profiles: onboarding.NewProfileStore(chat.pool), deployment: tools.LocationOrUTC(chat.cfg.Timezone)}
	scheduler := cron.New(chat.pool)
	runtime := &pimWatchRuntime{locale: locale, service: &pimwatch.Service{
		Store: store, Tasks: pimWatchTasks{store: scheduler}, Locale: locale,
		Routes: pimWatchRoutes{accounts: telegram.New(chat.pool), threads: chat},
	}}
	if chat.identity != nil {
		runtime.sweep = pimwatch.Sweep{Store: store, Identities: identityRoster{store: chat.identity}}
	}
	reader := newPIMWatchClient(chat)
	if reader == nil {
		return runtime
	}
	runtime.service.PIM = reader
	runtime.watcher = &pimwatch.Watcher{Store: store, PIM: reader, Ledger: pimWatchLedger{store: scheduler}, Locale: locale}
	runtime.digester = &pimwatch.Digester{Store: store, PIM: reader, Locale: locale}
	return runtime
}

// newPIMWatchClient returns nil, a true nil, when the deployment has no sidecar or no way to fetch
// a grant: a typed-nil client in the port would read as configured.
func newPIMWatchClient(chat *chatEnv) pimwatch.PIMReader {
	auth := chat.mcpAuth()
	if auth == nil {
		return nil
	}
	client := pimwatch.NewClient(chat.cfg.CalendarMCPURL, pimWatchTokens{auth: auth})
	if client == nil {
		return nil
	}
	return client
}

// bindPIMWatch hands the service to the agent's tool, once it exists.
func bindPIMWatch(chat *chatEnv) {
	if chat.toolHandles.MailWatch == nil {
		return
	}
	if service := chat.pimWatch().service; service != nil {
		chat.toolHandles.MailWatch.Service = service
	}
}

// pimTokenSource is the seam over *mcpAuthService.
type pimTokenSource interface {
	AccessToken(ctx context.Context, name string) (string, error)
}

// pimWatchTokens tells a grant that is gone from a token that could not be fetched right now: the
// first needs the person, and the second is the next tick's to retry.
type pimWatchTokens struct {
	auth pimTokenSource
}

func (t pimWatchTokens) AccessToken(ctx context.Context, server string) (string, error) {
	token, err := t.auth.AccessToken(ctx, server)
	if err == nil {
		return token, nil
	}
	if errors.Is(err, mcp.ErrOAuthAuthorizationRequired) || refreshRefused(err) {
		return "", fmt.Errorf("%w: %s", pimwatch.ErrNotAuthorized, server)
	}
	return "", err
}

// refreshRefused is a token endpoint saying no: a revoked or expired grant, not an outage.
func refreshRefused(err error) bool {
	refused, ok := errors.AsType[*oauth2.RetrieveError](err)
	if !ok || refused.Response == nil {
		return false
	}
	status := refused.Response.StatusCode
	return status >= http.StatusBadRequest && status < http.StatusInternalServerError
}

// pimWatchLocale reads an identity's language and zone at use, so a profile change needs no
// restart. A profile that cannot be read writes English in the deployment's zone.
type pimWatchLocale struct {
	profiles   pimProfiles
	deployment *time.Location
}

// pimProfiles is the seam over *onboarding.ProfileStore.
type pimProfiles interface {
	Load(ctx context.Context, identityID string) (onboarding.Answers, bool, error)
	Timezone(ctx context.Context, identityID string) string
}

func (l pimWatchLocale) Language(ctx context.Context, identityID string) pimwatch.Lang {
	answers, found, err := l.profiles.Load(ctx, identityID)
	if err != nil || !found {
		return pimwatch.LangEN
	}
	return pimwatch.ResolveLang(answers.Lang)
}

func (l pimWatchLocale) Location(ctx context.Context, identityID string) *time.Location {
	if zone := strings.TrimSpace(l.profiles.Timezone(ctx, identityID)); zone != "" {
		return tools.LocationOrUTC(zone)
	}
	return l.deployment
}

// pimWatchLedger answers from the run ledger. A run with no row never completed.
type pimWatchLedger struct {
	store interface {
		GetRun(ctx context.Context, id string) (cron.Run, error)
	}
}

func (l pimWatchLedger) RunCompleted(ctx context.Context, runID string) (bool, error) {
	run, err := l.store.GetRun(ctx, runID)
	if errors.Is(err, cron.ErrTaskNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return run.Status == "completed", nil
}

// pimTaskStore is the part of *cron.Store the watch's tasks use.
type pimTaskStore interface {
	ListLiveTasksOfKind(ctx context.Context, identityID string, kind cron.TaskKind) ([]cron.Task, error)
	CreateTask(ctx context.Context, p cron.CreateTaskParams) (cron.Task, error)
	UpdateTask(ctx context.Context, id string, p cron.UpdateTaskParams) error
	ResumeTask(ctx context.Context, id string, now time.Time) (time.Time, error)
	CancelTask(ctx context.Context, id string) error
}

// pimWatchTasks is the scheduler as the watch sees it.
type pimWatchTasks struct {
	store pimTaskStore
}

func (t pimWatchTasks) Watches(ctx context.Context, identityID string) ([]pimwatch.WatchTask, error) {
	return t.live(ctx, identityID, cron.KindPIMWatch)
}

func (t pimWatchTasks) Digests(ctx context.Context, identityID string) ([]pimwatch.WatchTask, error) {
	return t.live(ctx, identityID, cron.KindPIMDigest)
}

func (t pimWatchTasks) live(ctx context.Context, identityID string, kind cron.TaskKind) ([]pimwatch.WatchTask, error) {
	tasks, err := t.store.ListLiveTasksOfKind(ctx, identityID, kind)
	if err != nil {
		return nil, err
	}
	out := make([]pimwatch.WatchTask, 0, len(tasks))
	for _, task := range tasks {
		var payload pimwatch.TaskPayload
		// A task the cockpit edited out of shape still shows, so it can be turned off.
		_ = json.Unmarshal(task.Payload, &payload)
		out = append(out, pimwatch.WatchTask{
			ID: task.ID, AccountID: payload.AccountID, Paused: task.Status == "paused",
			PausedReason: task.PausedReason, NextRunAt: task.NextRunAt,
		})
	}
	return out, nil
}

func (t pimWatchTasks) CreateWatch(ctx context.Context, identityID string, spec pimwatch.WatchSpec) error {
	schedule, err := cron.ParseSchedule(string(cron.KindEvery), "", spec.EveryMinutes, time.Time{}, spec.Zone)
	if err != nil {
		return err
	}
	return t.create(ctx, cron.KindPIMWatch, identityID, schedule, pimwatch.TaskPayload{AccountID: spec.AccountID, Text: spec.Title}, spec.Delivery)
}

func (t pimWatchTasks) CreateDigest(ctx context.Context, identityID string, spec pimwatch.DigestSpec) error {
	schedule, err := digestSchedule(spec)
	if err != nil {
		return err
	}
	return t.create(ctx, cron.KindPIMDigest, identityID, schedule, pimwatch.TaskPayload{Text: spec.Title}, spec.Delivery)
}

func digestSchedule(spec pimwatch.DigestSpec) (cron.ScheduleSpec, error) {
	return cron.ParseSchedule(string(cron.KindCron), fmt.Sprintf("0 %d * * *", spec.Hour), 0, time.Time{}, spec.Zone)
}

// create inserts a task. The database keeps one live watch per account and one digest per
// identity, so losing that race to a second click means the task is there.
func (t pimWatchTasks) create(ctx context.Context, kind cron.TaskKind, identityID string, schedule cron.ScheduleSpec, payload pimwatch.TaskPayload, delivery pimwatch.Delivery) error {
	next, err := cron.NextRunAt(schedule, time.Now().UTC())
	if err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = t.store.CreateTask(ctx, cron.CreateTaskParams{
		Kind: kind, Spec: schedule, Payload: raw, NextRunAt: next, NotifyRoute: delivery.Route, IdentityID: identityID,
		OriginConversationID: delivery.OriginConversationID, TelegramRecipients: delivery.TelegramRecipients,
	})
	if db.IsUniqueViolation(err) {
		return nil
	}
	return err
}

func (t pimWatchTasks) RescheduleDigest(ctx context.Context, taskID string, spec pimwatch.DigestSpec) error {
	schedule, err := digestSchedule(spec)
	if err != nil {
		return err
	}
	next, err := cron.NextRunAt(schedule, time.Now().UTC())
	if err != nil {
		return err
	}
	raw, err := json.Marshal(pimwatch.TaskPayload{Text: spec.Title})
	if err != nil {
		return err
	}
	return t.store.UpdateTask(ctx, taskID, cron.UpdateTaskParams{
		Spec: schedule, NextRunAt: next, Payload: raw, NotifyRoute: spec.Delivery.Route,
		OriginConversationID: spec.Delivery.OriginConversationID, TelegramRecipients: spec.Delivery.TelegramRecipients,
	})
}

func (t pimWatchTasks) Resume(ctx context.Context, taskID string) error {
	_, err := t.store.ResumeTask(ctx, taskID, time.Now().UTC())
	return err
}

func (t pimWatchTasks) Cancel(ctx context.Context, taskID string) error {
	return t.store.CancelTask(ctx, taskID)
}

// pimTelegramAccounts is the seam over *telegram.Store.
type pimTelegramAccounts interface {
	GetAccountByIdentity(ctx context.Context, identityID string) (telegram.Account, error)
}

// pimThreads brings a channel thread's conversation into being.
type pimThreads interface {
	EnsureThread(ctx context.Context, conversationID string) error
}

// EnsureThread creates the Telegram thread's conversation under its deterministic id. The first
// message would have, and until it does homeTask would read a missing row as a deleted origin.
func (c *chatEnv) EnsureThread(ctx context.Context, conversationID string) error {
	return c.run.EnsureConversation(ctx, conversationID)
}

// pimWatchRoutes finds where an identity's watch reports: its linked Telegram thread, else a
// conversation the task opens on its first run, with the cockpit as the delivery.
type pimWatchRoutes struct {
	accounts pimTelegramAccounts
	threads  pimThreads
}

func (r pimWatchRoutes) DeliveryFor(ctx context.Context, identityID string) (pimwatch.Delivery, error) {
	account, err := r.accounts.GetAccountByIdentity(ctx, identityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pimwatch.Delivery{Route: string(cron.RouteStdout)}, nil
	}
	if err != nil {
		return pimwatch.Delivery{}, err
	}
	thread := telegram.ThreadConversationID(account.TelegramUserID)
	if err := r.threads.EnsureThread(identityctx.WithIdentityID(ctx, identityID), thread); err != nil {
		return pimwatch.Delivery{}, err
	}
	return pimwatch.Delivery{
		Route: string(cron.RouteTelegram), OriginConversationID: thread,
		TelegramRecipients: []int64{account.TelegramUserID},
	}, nil
}
