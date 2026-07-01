// Package app implements the identity use cases: passwordless login (OTP),
// session creation, current-user resolution, and logout. The transport and
// storage details live behind ports.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/identity/domain"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
)

// Errors surfaced to the HTTP adapter.
var (
	ErrInvalidCode = errors.New("identity: invalid or expired code")
	ErrNoSession   = errors.New("identity: session not found or expired")
)

const maxOTPAttempts = 5

// Challenge is a pending one-time-code login challenge.
type Challenge struct {
	ID        string
	Email     string
	CodeHash  string
	ExpiresAt time.Time
	Consumed  bool
	Attempts  int
}

// DirectoryUser is the account behind an email (a student or an admin).
type DirectoryUser struct {
	Role        authn.Role
	SubjectType authn.SubjectType
	SubjectID   string
	Email       string
	FullName    string
}

// SessionRecord is a persisted session.
type SessionRecord struct {
	ID          string
	SubjectType authn.SubjectType
	SubjectID   string
	Role        authn.Role
	CSRFToken   string
	ExpiresAt   time.Time
}

// Ports.
type (
	// ChallengeStore persists OTP login challenges.
	ChallengeStore interface {
		Create(ctx context.Context, email, codeHash string, expiresAt time.Time) error
		LatestActive(ctx context.Context, email string, now time.Time) (*Challenge, bool, error)
		IncrementAttempts(ctx context.Context, id string) error
		Consume(ctx context.Context, id string, now time.Time) error
	}
	// Directory resolves accounts by email or by subject reference.
	Directory interface {
		FindByEmail(ctx context.Context, email string) (*DirectoryUser, bool, error)
		FindBySubject(ctx context.Context, subjectType authn.SubjectType, subjectID string) (*DirectoryUser, bool, error)
	}
	// SessionStore persists sessions.
	SessionStore interface {
		Create(ctx context.Context, s SessionRecord) error
		Get(ctx context.Context, id string) (*SessionRecord, bool, error)
		Delete(ctx context.Context, id string) error
	}
	// Mailer delivers the login code.
	Mailer interface {
		SendLoginCode(ctx context.Context, email, code string) error
	}
	// Clock abstracts time for testability.
	Clock interface{ Now() time.Time }
	// Codes generates and hashes one-time codes.
	// Generate, NewSessionID, and NewCSRFToken return errors so that a
	// crypto/rand failure is surfaced as an internal server error rather than
	// silently degrading to predictable output ("000000", all-zero hex).
	// Hash (SHA-256) is deterministic and never returns an error.
	Codes interface {
		Generate() (string, error)
		Hash(code string) string
		NewSessionID() (string, error)
		NewCSRFToken() (string, error)
	}
)

// Config carries identity policy settings.
type Config struct {
	AllowedDomains []string
	OTPTTL         time.Duration
	SessionTTL     time.Duration
}

// AuthService orchestrates the identity use cases.
type AuthService struct {
	cfg        Config
	challenges ChallengeStore
	directory  Directory
	sessions   SessionStore
	mailer     Mailer
	clock      Clock
	codes      Codes
}

// NewAuthService wires the service with its ports.
func NewAuthService(cfg Config, ch ChallengeStore, dir Directory, ss SessionStore, m Mailer, clock Clock, codes Codes) *AuthService {
	return &AuthService{cfg: cfg, challenges: ch, directory: dir, sessions: ss, mailer: m, clock: clock, codes: codes}
}

// StartResult is returned to the caller after starting a login.
type StartResult struct {
	ExpiresInSeconds int
}

// StartLogin issues an OTP to an eligible account. To avoid account
// enumeration it always reports success; a code is only created and sent when
// the email maps to a real, allowed account.
func (s *AuthService) StartLogin(ctx context.Context, rawEmail string) (StartResult, error) {
	email := domain.NormalizeEmail(rawEmail)
	res := StartResult{ExpiresInSeconds: int(s.cfg.OTPTTL.Seconds())}

	if !domain.IsAllowedDomain(email, s.cfg.AllowedDomains) {
		return res, nil
	}
	user, ok, err := s.directory.FindByEmail(ctx, email)
	if err != nil {
		return StartResult{}, err
	}
	if !ok || user == nil {
		return res, nil
	}

	// Fix C (TRD §21): if there is already a live, unconsumed challenge for this
	// email, reuse it instead of minting a fresh one. Without this guard an
	// attacker can call /start repeatedly to reset the per-challenge attempt cap
	// (maxOTPAttempts), effectively bypassing brute-force protection. Reusing
	// the existing challenge keeps the same code and the same attempt counter.
	existing, hasActive, err := s.challenges.LatestActive(ctx, email, s.clock.Now())
	if err != nil {
		return StartResult{}, err
	}
	if hasActive && existing != nil {
		// An unexpired, unconsumed challenge already exists — return success
		// without creating a new one or sending another email.
		return res, nil
	}

	code, err := s.codes.Generate()
	if err != nil {
		return StartResult{}, err
	}
	if err := s.challenges.Create(ctx, email, s.codes.Hash(code), s.clock.Now().Add(s.cfg.OTPTTL)); err != nil {
		return StartResult{}, err
	}
	if err := s.mailer.SendLoginCode(ctx, email, code); err != nil {
		return StartResult{}, err
	}
	return res, nil
}

// VerifyLogin checks the OTP and, on success, creates a session and returns the
// principal plus the session id to set as a cookie.
func (s *AuthService) VerifyLogin(ctx context.Context, rawEmail, code string) (*authn.Principal, string, error) {
	email := domain.NormalizeEmail(rawEmail)
	now := s.clock.Now()

	challenge, ok, err := s.challenges.LatestActive(ctx, email, now)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", ErrInvalidCode
	}
	if challenge.Attempts >= maxOTPAttempts {
		return nil, "", ErrInvalidCode
	}
	if s.codes.Hash(code) != challenge.CodeHash {
		_ = s.challenges.IncrementAttempts(ctx, challenge.ID)
		return nil, "", ErrInvalidCode
	}

	user, ok, err := s.directory.FindByEmail(ctx, email)
	if err != nil {
		return nil, "", err
	}
	if !ok || user == nil {
		return nil, "", ErrInvalidCode
	}

	if err := s.challenges.Consume(ctx, challenge.ID, now); err != nil {
		return nil, "", err
	}

	sessionID, err := s.codes.NewSessionID()
	if err != nil {
		return nil, "", err
	}
	csrfToken, err := s.codes.NewCSRFToken()
	if err != nil {
		return nil, "", err
	}
	sess := SessionRecord{
		ID:          sessionID,
		SubjectType: user.SubjectType,
		SubjectID:   user.SubjectID,
		Role:        user.Role,
		CSRFToken:   csrfToken,
		ExpiresAt:   now.Add(s.cfg.SessionTTL),
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, "", err
	}

	return principalFrom(sess, user.Email, user.FullName), sess.ID, nil
}

// Authenticate resolves a session id into a principal (implements
// authn.Authenticator).
func (s *AuthService) Authenticate(ctx context.Context, sessionID string) (*authn.Principal, error) {
	sess, ok, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if !ok || sess.ExpiresAt.Before(s.clock.Now()) {
		return nil, ErrNoSession
	}

	// Fix A (TRD §21): re-validate that the underlying account is still ACTIVE
	// on every authenticated request. Without this check a deactivated admin
	// retains full API access for the remainder of the session TTL (~12 h).
	//
	// We call directory.FindBySubject here rather than adding a JOIN to
	// SessionRepo.Get so that (a) the session store stays a dumb storage layer
	// and (b) the status check is exercisable with unit-test fakes without a
	// real database. The cost is one extra query per request, which is
	// acceptable for this application's traffic profile.
	_, active, err := s.directory.FindBySubject(ctx, sess.SubjectType, sess.SubjectID)
	if err != nil {
		return nil, err
	}
	if !active {
		// User has been deactivated since the session was created.
		return nil, ErrNoSession
	}

	// The email/name are not stored on the session; resolve lazily only when
	// needed by /me, so keep them empty here. The principal still carries the
	// identifiers and role required for authorization.
	return principalFrom(*sess, "", ""), nil
}

// Logout deletes the session.
func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	return s.sessions.Delete(ctx, sessionID)
}

// CurrentUser resolves the full account behind a principal for the /me endpoint.
func (s *AuthService) CurrentUser(ctx context.Context, p *authn.Principal) (*DirectoryUser, error) {
	user, ok, err := s.directory.FindBySubject(ctx, p.SubjectType, p.SubjectID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoSession
	}
	return user, nil
}

func principalFrom(s SessionRecord, email, fullName string) *authn.Principal {
	return &authn.Principal{
		SessionID:   s.ID,
		Role:        s.Role,
		SubjectType: s.SubjectType,
		SubjectID:   s.SubjectID,
		Email:       email,
		FullName:    fullName,
		CSRFToken:   s.CSRFToken,
	}
}
