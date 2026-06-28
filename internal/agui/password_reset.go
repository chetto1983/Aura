package agui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"time"
)

const passwordResetChallengeTTL = 10 * time.Minute

var (
	ErrPasswordResetDenied      = errors.New("password reset denied")
	errPasswordResetInvalid     = errors.New("password reset invalid request")
	errPasswordResetUnavailable = errors.New("password reset unavailable")
)

type RecoveryRecord struct {
	IdentityID        string
	Email             string
	AuthulaUserID     string
	Question          string
	AnswerHash        string
	AnswerHashVersion string
	TelegramUserID    int64
}

type PasswordResetChallenge struct {
	IdentityID    string
	CodeHash      string
	ExpiresAt     time.Time
	RequestIPHash string
	UserAgentHash string
}

type RecoveryEvent struct {
	Event         string
	IdentityID    string
	EmailHash     string
	RequestIPHash string
	UserAgentHash string
	At            time.Time
}

type PasswordResetStore interface {
	// LookupByEmail adapters must map not-found, no usable recovery row, and missing
	// Telegram link to ErrPasswordResetDenied so callers cannot enumerate accounts.
	LookupByEmail(ctx context.Context, email string) (RecoveryRecord, error)
	StartChallenge(ctx context.Context, challenge PasswordResetChallenge) error
	VerifyChallenge(ctx context.Context, identityID, code string) (string, error)
	ConsumeResetTokenHash(ctx context.Context, tokenHash string) (string, error)
	RecordChallengeAttempt(ctx context.Context, identityID string) error
	RecordRecoveryEvent(ctx context.Context, event RecoveryEvent) error
}

type RecoveryMessenger interface {
	SendRecoveryCode(ctx context.Context, identityID, code string) error
}

type PasswordResetter interface {
	SetPassword(ctx context.Context, identityID, password string) error
}

type PasswordResetDeps struct {
	Store     PasswordResetStore
	Messenger RecoveryMessenger
	Resetter  PasswordResetter
	Clock     func() time.Time
}

type PasswordResetService struct {
	store     PasswordResetStore
	messenger RecoveryMessenger
	resetter  PasswordResetter
	clock     func() time.Time
}

type PasswordResetStartRequest struct {
	Email     string `json:"email"`
	RequestIP string `json:"-"`
	UserAgent string `json:"-"`
}

type PasswordResetStartResponse struct {
	Status string `json:"status"`
}

type PasswordResetVerifyRequest struct {
	Email     string `json:"email"`
	Code      string `json:"code"`
	Answer    string `json:"answer"`
	RequestIP string `json:"-"`
	UserAgent string `json:"-"`
}

type PasswordResetVerifyResponse struct {
	ResetToken string `json:"resetToken"`
}

type PasswordResetCompleteRequest struct {
	ResetToken string `json:"resetToken"`
	Password   string `json:"password"`
	RequestIP  string `json:"-"`
	UserAgent  string `json:"-"`
}

type PasswordResetCompleteResponse struct {
	Status string `json:"status"`
}

func NewPasswordResetService(d PasswordResetDeps) *PasswordResetService {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	return &PasswordResetService{
		store:     d.Store,
		messenger: d.Messenger,
		resetter:  d.Resetter,
		clock:     clock,
	}
}

func (s *PasswordResetService) Start(ctx context.Context, in PasswordResetStartRequest) (PasswordResetStartResponse, error) {
	ok := PasswordResetStartResponse{Status: "ok"}
	if !s.readyForStart() {
		return ok, errPasswordResetUnavailable
	}
	email := strings.TrimSpace(in.Email)
	if email == "" {
		_ = s.recordEvent(ctx, passwordResetEvent("reset_start_denied", RecoveryRecord{}, email, in, s.now()))
		return ok, nil
	}

	record, err := s.store.LookupByEmail(ctx, email)
	if err != nil || !record.canRecover() {
		_ = s.recordEvent(ctx, passwordResetEvent("reset_start_denied", RecoveryRecord{}, email, in, s.now()))
		if err == nil || errors.Is(err, ErrPasswordResetDenied) {
			return ok, nil
		}
		return ok, errPasswordResetUnavailable
	}

	code, err := newRecoveryCode()
	if err != nil {
		return ok, errPasswordResetUnavailable
	}
	codeHash, err := HashOpaqueSecret(code)
	if err != nil {
		return ok, errPasswordResetUnavailable
	}
	if err := s.store.StartChallenge(ctx, PasswordResetChallenge{
		IdentityID:    record.IdentityID,
		CodeHash:      codeHash,
		ExpiresAt:     s.now().Add(passwordResetChallengeTTL),
		RequestIPHash: hashRequestValue(in.RequestIP),
		UserAgentHash: hashRequestValue(in.UserAgent),
	}); err != nil {
		return ok, errPasswordResetUnavailable
	}
	if err := s.messenger.SendRecoveryCode(ctx, record.IdentityID, code); err != nil {
		return ok, errPasswordResetUnavailable
	}
	if err := s.recordEvent(ctx, passwordResetEvent("reset_start", record, email, in, s.now())); err != nil {
		return ok, errPasswordResetUnavailable
	}
	return ok, nil
}

func (s *PasswordResetService) Verify(ctx context.Context, in PasswordResetVerifyRequest) (PasswordResetVerifyResponse, error) {
	if !s.readyForVerify() {
		return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
	}
	email := strings.TrimSpace(in.Email)
	record, err := s.store.LookupByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrPasswordResetDenied) {
			_ = s.recordEvent(ctx, passwordResetVerifyEvent("reset_verify_denied", RecoveryRecord{}, email, in, s.now()))
			return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
	}
	if !record.canRecover() {
		_ = s.recordEvent(ctx, passwordResetVerifyEvent("reset_verify_denied", record, email, in, s.now()))
		return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
	}
	if record.AnswerHashVersion != recoveryAnswerHashVersion ||
		!(RecoveryHasher{}).VerifyAnswer(in.Answer, record.AnswerHash) {
		_ = s.store.RecordChallengeAttempt(ctx, record.IdentityID)
		_ = s.recordEvent(ctx, passwordResetVerifyEvent("reset_verify_denied", record, email, in, s.now()))
		return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
	}
	token, err := s.store.VerifyChallenge(ctx, record.IdentityID, in.Code)
	if err != nil {
		if errors.Is(err, ErrPasswordResetDenied) {
			_ = s.recordEvent(ctx, passwordResetVerifyEvent("reset_verify_denied", record, email, in, s.now()))
			return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
	}
	if token == "" {
		_ = s.recordEvent(ctx, passwordResetVerifyEvent("reset_verify_denied", record, email, in, s.now()))
		return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
	}
	if err := s.recordEvent(ctx, passwordResetVerifyEvent("reset_verify", record, email, in, s.now())); err != nil {
		return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
	}
	return PasswordResetVerifyResponse{ResetToken: token}, nil
}

func (s *PasswordResetService) Complete(ctx context.Context, in PasswordResetCompleteRequest) (PasswordResetCompleteResponse, error) {
	if !s.readyForComplete() {
		return PasswordResetCompleteResponse{}, errPasswordResetUnavailable
	}
	if len(in.Password) < 8 {
		return PasswordResetCompleteResponse{}, errPasswordResetInvalid
	}
	if strings.TrimSpace(in.ResetToken) == "" {
		return PasswordResetCompleteResponse{}, ErrPasswordResetDenied
	}
	identityID, err := s.store.ConsumeResetTokenHash(ctx, HashLookupToken(in.ResetToken))
	if err != nil {
		if errors.Is(err, ErrPasswordResetDenied) {
			return PasswordResetCompleteResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetCompleteResponse{}, errPasswordResetUnavailable
	}
	if identityID == "" {
		return PasswordResetCompleteResponse{}, ErrPasswordResetDenied
	}
	if err := s.resetter.SetPassword(ctx, identityID, in.Password); err != nil {
		return PasswordResetCompleteResponse{}, errPasswordResetUnavailable
	}
	event := RecoveryEvent{
		Event:         "reset_complete",
		IdentityID:    identityID,
		RequestIPHash: hashRequestValue(in.RequestIP),
		UserAgentHash: hashRequestValue(in.UserAgent),
		At:            s.now(),
	}
	if err := s.recordEvent(ctx, event); err != nil {
		return PasswordResetCompleteResponse{}, errPasswordResetUnavailable
	}
	return PasswordResetCompleteResponse{Status: "password_updated"}, nil
}

func (s *PasswordResetService) readyForStart() bool {
	return s != nil && s.store != nil && s.messenger != nil
}

func (s *PasswordResetService) readyForVerify() bool {
	return s != nil && s.store != nil
}

func (s *PasswordResetService) readyForComplete() bool {
	return s != nil && s.store != nil && s.resetter != nil
}

func (s *PasswordResetService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func (s *PasswordResetService) recordEvent(ctx context.Context, event RecoveryEvent) error {
	if s == nil || s.store == nil {
		return errPasswordResetUnavailable
	}
	return s.store.RecordRecoveryEvent(ctx, event)
}

func (r RecoveryRecord) canRecover() bool {
	return r.IdentityID != "" && r.AnswerHash != "" && r.TelegramUserID != 0
}

func passwordResetEvent(event string, record RecoveryRecord, email string, in PasswordResetStartRequest, at time.Time) RecoveryEvent {
	return RecoveryEvent{
		Event:         event,
		IdentityID:    record.IdentityID,
		EmailHash:     hashRequestValue(email),
		RequestIPHash: hashRequestValue(in.RequestIP),
		UserAgentHash: hashRequestValue(in.UserAgent),
		At:            at,
	}
}

func passwordResetVerifyEvent(event string, record RecoveryRecord, email string, in PasswordResetVerifyRequest, at time.Time) RecoveryEvent {
	return RecoveryEvent{
		Event:         event,
		IdentityID:    record.IdentityID,
		EmailHash:     hashRequestValue(email),
		RequestIPHash: hashRequestValue(in.RequestIP),
		UserAgentHash: hashRequestValue(in.UserAgent),
		At:            at,
	}
}

func newRecoveryCode() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func hashRequestValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil && strings.TrimSpace(host) != "" {
		value = strings.TrimSpace(host)
	}
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
