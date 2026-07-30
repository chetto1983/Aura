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
	// ErrPasswordResetDenied is returned for invalid or unauthorized reset attempts.
	ErrPasswordResetDenied = errors.New("password reset denied")
	// ErrPasswordResetSamePassword is returned when the new password equals the current one,
	// so the caller can tell the user to choose a different password (non-sensitive: the user
	// already knows the password they just typed).
	ErrPasswordResetSamePassword = errors.New("password reset new password matches current")
	errPasswordResetInvalid      = errors.New("password reset invalid request")
	errPasswordResetUnavailable  = errors.New("password reset unavailable")
)

// RecoveryRecord is the private recovery profile needed to start or verify a reset.
type RecoveryRecord struct {
	IdentityID        string
	Email             string
	AuthulaUserID     string
	Question          string
	AnswerHash        string
	AnswerHashVersion string
	TelegramUserID    int64
}

// PasswordResetChallenge is the stored Telegram-code challenge metadata.
type PasswordResetChallenge struct {
	IdentityID    string
	CodeHash      string
	ExpiresAt     time.Time
	RequestIPHash string
	UserAgentHash string
}

// RecoveryEvent is an audit event for password recovery activity.
type RecoveryEvent struct {
	Event         string
	IdentityID    string
	EmailHash     string
	RequestIPHash string
	UserAgentHash string
	At            time.Time
}

// PasswordResetStore persists recovery setup, challenges, reset tokens, and audit events.
type PasswordResetStore interface {
	// LookupByEmail adapters must map not-found, no usable recovery row, and missing
	// Telegram link to ErrPasswordResetDenied so callers cannot enumerate accounts.
	LookupByEmail(ctx context.Context, email string) (RecoveryRecord, error)
	StartChallenge(ctx context.Context, challenge PasswordResetChallenge) error
	// PeekChallenge validates that code matches the identity's live challenge WITHOUT
	// consuming it, so the security question can be revealed only after the caller proves
	// Telegram-code possession. Adapters must map not-found / mismatch / expired /
	// over-attempt to ErrPasswordResetDenied and rate-limit wrong codes.
	PeekChallenge(ctx context.Context, identityID, code string) error
	VerifyChallenge(ctx context.Context, identityID, code string) (string, error)
	ClaimResetTokenHash(ctx context.Context, tokenHash string) (ResetTokenClaim, error)
	ResolveResetTokenHash(ctx context.Context, tokenHash string) (string, error)
	ConsumeResetTokenHash(ctx context.Context, tokenHash string) (string, error)
	RecordChallengeAttempt(ctx context.Context, identityID string) error
	RecordRecoveryEvent(ctx context.Context, event RecoveryEvent) error
}

// ResetTokenClaim is a leased reset token that must be consumed or released.
type ResetTokenClaim interface {
	IdentityID() string
	Consume(ctx context.Context) (string, error)
	Release(ctx context.Context) error
}

// RecoveryMessenger delivers reset codes to an already-linked recovery channel.
type RecoveryMessenger interface {
	SendRecoveryCode(ctx context.Context, identityID, code string) error
}

// PasswordResetter updates the underlying Authula password and invalidates sessions.
type PasswordResetter interface {
	SetPassword(ctx context.Context, identityID, password string) error
}

// PasswordResetDeps collects concrete adapters for the password reset service.
type PasswordResetDeps struct {
	Store     PasswordResetStore
	Messenger RecoveryMessenger
	Resetter  PasswordResetter
	Clock     func() time.Time
	// TokenPepper is the HKDF-derived HMAC key for reset-token DB lookups.
	TokenPepper []byte
}

// PasswordResetService implements Telegram-code plus recovery-answer password reset.
type PasswordResetService struct {
	store     PasswordResetStore
	messenger RecoveryMessenger
	resetter  PasswordResetter
	clock     func() time.Time
	pepper    []byte
}

// PasswordResetStartRequest is the public request to start a reset challenge.
type PasswordResetStartRequest struct {
	Email     string `json:"email"`
	RequestIP string `json:"-"`
	UserAgent string `json:"-"`
}

// PasswordResetStartResponse is the neutral response returned for reset starts.
type PasswordResetStartResponse struct {
	Status string `json:"status"`
}

// PasswordResetVerifyRequest verifies a Telegram code and recovery answer.
type PasswordResetVerifyRequest struct {
	Email     string `json:"email"`
	Code      string `json:"code"`
	Answer    string `json:"answer"`
	RequestIP string `json:"-"`
	UserAgent string `json:"-"`
}

// PasswordResetVerifyResponse returns the short-lived reset token after verification.
type PasswordResetVerifyResponse struct {
	ResetToken string `json:"resetToken"`
}

// PasswordResetQuestionRequest reveals the security question after proving code possession.
type PasswordResetQuestionRequest struct {
	Email     string `json:"email"`
	Code      string `json:"code"`
	RequestIP string `json:"-"`
	UserAgent string `json:"-"`
}

// PasswordResetQuestionResponse returns the security question once the code is proven.
type PasswordResetQuestionResponse struct {
	Question string `json:"question"`
}

// PasswordResetCompleteRequest applies a new password with a verified reset token.
type PasswordResetCompleteRequest struct {
	ResetToken string `json:"resetToken"`
	Password   string `json:"password"`
	RequestIP  string `json:"-"`
	UserAgent  string `json:"-"`
}

// PasswordResetCompleteResponse reports a completed password reset.
type PasswordResetCompleteResponse struct {
	Status string `json:"status"`
}

// NewPasswordResetService constructs a password reset service from narrow adapters.
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
		pepper:    append([]byte(nil), d.TokenPepper...),
	}
}

// Start creates a neutral reset challenge and sends the code via Telegram when possible.
func (s *PasswordResetService) Start(ctx context.Context, in PasswordResetStartRequest) (PasswordResetStartResponse, error) {
	ok := PasswordResetStartResponse{Status: "ok"}
	if !s.readyForStart() {
		return ok, errPasswordResetUnavailable
	}
	email := strings.TrimSpace(in.Email)
	if email == "" {
		_ = s.recordEvent(ctx, recoveryAuditEvent("reset_start_denied", RecoveryRecord{}, email, in.RequestIP, in.UserAgent, s.now()))
		return ok, nil
	}

	record, err := s.store.LookupByEmail(ctx, email)
	if err != nil || !record.canRecover() {
		_ = s.recordEvent(ctx, recoveryAuditEvent("reset_start_denied", RecoveryRecord{}, email, in.RequestIP, in.UserAgent, s.now()))
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
		if errors.Is(err, ErrPasswordResetDenied) {
			_ = s.recordEvent(ctx, recoveryAuditEvent("reset_start_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
			return ok, nil
		}
		return ok, errPasswordResetUnavailable
	}
	if err := s.messenger.SendRecoveryCode(ctx, record.IdentityID, code); err != nil {
		_ = s.recordEvent(ctx, recoveryAuditEvent("reset_start_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
		return ok, nil
	}
	// Public start responses stay neutral even if the append-only audit sink is
	// temporarily unavailable; backend observability can be added here without
	// changing the user-visible flow.
	_ = s.recordEvent(ctx, recoveryAuditEvent("reset_start", record, email, in.RequestIP, in.UserAgent, s.now()))
	return ok, nil
}

// Verify checks the Telegram code and recovery answer, returning a reset token.
func (s *PasswordResetService) Verify(ctx context.Context, in PasswordResetVerifyRequest) (PasswordResetVerifyResponse, error) {
	if !s.readyForVerify() {
		return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
	}
	email := strings.TrimSpace(in.Email)
	record, err := s.store.LookupByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrPasswordResetDenied) {
			_ = s.recordEvent(ctx, recoveryAuditEvent("reset_verify_denied", RecoveryRecord{}, email, in.RequestIP, in.UserAgent, s.now()))
			return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
	}
	if !record.canRecover() {
		_ = s.recordEvent(ctx, recoveryAuditEvent("reset_verify_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
		return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
	}
	if record.AnswerHashVersion != recoveryAnswerHashVersion ||
		!(RecoveryHasher{}).VerifyAnswer(in.Answer, record.AnswerHash) {
		if err := s.store.RecordChallengeAttempt(ctx, record.IdentityID); err != nil {
			if errors.Is(err, ErrPasswordResetDenied) {
				_ = s.recordEvent(ctx, recoveryAuditEvent("reset_verify_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
				return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
			}
			return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
		}
		_ = s.recordEvent(ctx, recoveryAuditEvent("reset_verify_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
		return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
	}
	token, err := s.store.VerifyChallenge(ctx, record.IdentityID, in.Code)
	if err != nil {
		if errors.Is(err, ErrPasswordResetDenied) {
			_ = s.recordEvent(ctx, recoveryAuditEvent("reset_verify_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
			return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
	}
	if token == "" {
		_ = s.recordEvent(ctx, recoveryAuditEvent("reset_verify_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
		return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
	}
	if err := s.recordEvent(ctx, recoveryAuditEvent("reset_verify", record, email, in.RequestIP, in.UserAgent, s.now())); err != nil {
		return PasswordResetVerifyResponse{}, errPasswordResetUnavailable
	}
	return PasswordResetVerifyResponse{ResetToken: token}, nil
}

// Question reveals the security question only after the caller proves possession of a live
// Telegram code, so the browser never learns the question (or that an account exists) without
// it. All failure paths return the neutral ErrPasswordResetDenied.
func (s *PasswordResetService) Question(ctx context.Context, in PasswordResetQuestionRequest) (PasswordResetQuestionResponse, error) {
	if !s.readyForVerify() {
		return PasswordResetQuestionResponse{}, errPasswordResetUnavailable
	}
	email := strings.TrimSpace(in.Email)
	record, err := s.store.LookupByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrPasswordResetDenied) {
			_ = s.recordEvent(ctx, recoveryAuditEvent("reset_question_denied", RecoveryRecord{}, email, in.RequestIP, in.UserAgent, s.now()))
			return PasswordResetQuestionResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetQuestionResponse{}, errPasswordResetUnavailable
	}
	if !record.canRecover() || record.Question == "" {
		_ = s.recordEvent(ctx, recoveryAuditEvent("reset_question_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
		return PasswordResetQuestionResponse{}, ErrPasswordResetDenied
	}
	if err := s.store.PeekChallenge(ctx, record.IdentityID, in.Code); err != nil {
		if errors.Is(err, ErrPasswordResetDenied) {
			_ = s.recordEvent(ctx, recoveryAuditEvent("reset_question_denied", record, email, in.RequestIP, in.UserAgent, s.now()))
			return PasswordResetQuestionResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetQuestionResponse{}, errPasswordResetUnavailable
	}
	_ = s.recordEvent(ctx, recoveryAuditEvent("reset_question", record, email, in.RequestIP, in.UserAgent, s.now()))
	return PasswordResetQuestionResponse{Question: record.Question}, nil
}

// Complete consumes a reset token and updates the Authula password.
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
	tokenHash := HashLookupToken(in.ResetToken, s.pepper)
	claim, err := s.store.ClaimResetTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrPasswordResetDenied) {
			return PasswordResetCompleteResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetCompleteResponse{}, errPasswordResetUnavailable
	}
	if claim == nil {
		return PasswordResetCompleteResponse{}, ErrPasswordResetDenied
	}
	identityID := claim.IdentityID()
	if identityID == "" {
		_ = claim.Release(ctx)
		return PasswordResetCompleteResponse{}, ErrPasswordResetDenied
	}
	if err := s.resetter.SetPassword(ctx, identityID, in.Password); err != nil {
		// Release (not consume) so the user can retry with a different password on the same
		// reset token instead of restarting the whole Telegram challenge.
		_ = claim.Release(ctx)
		if errors.Is(err, ErrPasswordResetSamePassword) {
			return PasswordResetCompleteResponse{}, ErrPasswordResetSamePassword
		}
		return PasswordResetCompleteResponse{}, errPasswordResetUnavailable
	}
	consumedIdentityID, err := claim.Consume(ctx)
	if err != nil {
		_ = claim.Release(ctx)
		if errors.Is(err, ErrPasswordResetDenied) {
			return PasswordResetCompleteResponse{}, ErrPasswordResetDenied
		}
		return PasswordResetCompleteResponse{}, errPasswordResetUnavailable
	}
	if consumedIdentityID != identityID {
		_ = claim.Release(ctx)
		return PasswordResetCompleteResponse{}, ErrPasswordResetDenied
	}
	event := RecoveryEvent{
		Event:         "reset_complete",
		IdentityID:    identityID,
		RequestIPHash: hashRequestValue(in.RequestIP),
		UserAgentHash: hashRequestValue(in.UserAgent),
		At:            s.now(),
	}
	_ = s.recordEvent(ctx, event)
	return PasswordResetCompleteResponse{Status: "password_updated"}, nil
}

func (s *PasswordResetService) readyForStart() bool {
	return s != nil && s.store != nil && s.messenger != nil
}

func (s *PasswordResetService) readyForVerify() bool {
	return s != nil && s.store != nil
}

func (s *PasswordResetService) readyForComplete() bool {
	return s != nil && s.store != nil && s.resetter != nil && len(s.pepper) > 0
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

func recoveryAuditEvent(event string, record RecoveryRecord, email, requestIP, userAgent string, at time.Time) RecoveryEvent {
	return RecoveryEvent{
		Event:         event,
		IdentityID:    record.IdentityID,
		EmailHash:     hashRequestValue(email),
		RequestIPHash: hashRequestValue(requestIP),
		UserAgentHash: hashRequestValue(userAgent),
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
