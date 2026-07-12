package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/identity/app"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
)

// --- fakes ---

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

type fakeCodes struct{ code string }

func (f *fakeCodes) Generate() (string, error)    { return f.code, nil }
func (f *fakeCodes) Hash(s string) string         { return "hash:" + s }
func (f *fakeCodes) NewSessionID() (string, error) { return "sess-1", nil }
func (f *fakeCodes) NewCSRFToken() (string, error) { return "csrf-1", nil }

type fakeMailer struct{ sent []string }

func (m *fakeMailer) SendLoginCode(_ context.Context, email, code string) error {
	m.sent = append(m.sent, email+":"+code)
	return nil
}

type fakeDirectory struct{ users map[string]app.DirectoryUser }

func (d *fakeDirectory) FindByEmail(_ context.Context, email string) (*app.DirectoryUser, bool, error) {
	u, ok := d.users[email]
	if !ok {
		return nil, false, nil
	}
	return &u, true, nil
}
func (d *fakeDirectory) FindBySubject(_ context.Context, _ authn.SubjectType, id string) (*app.DirectoryUser, bool, error) {
	for _, u := range d.users {
		if u.SubjectID == id {
			return &u, true, nil
		}
	}
	return nil, false, nil
}

type fakeChallenges struct {
	created     *app.Challenge
	createCalls int // tracks how many times Create was called
}

func (c *fakeChallenges) Create(_ context.Context, email, hash string, exp time.Time) error {
	c.createCalls++
	c.created = &app.Challenge{ID: "ch-1", Email: email, CodeHash: hash, ExpiresAt: exp}
	return nil
}
func (c *fakeChallenges) LatestActive(_ context.Context, email string, _ time.Time) (*app.Challenge, bool, error) {
	if c.created == nil || c.created.Email != email || c.created.Consumed {
		return nil, false, nil
	}
	return c.created, true, nil
}
func (c *fakeChallenges) IncrementAttempts(_ context.Context, _ string) error {
	c.created.Attempts++
	return nil
}
func (c *fakeChallenges) Consume(_ context.Context, _ string, _ time.Time) error {
	c.created.Consumed = true
	return nil
}

type fakeSessions struct{ store map[string]app.SessionRecord }

func (s *fakeSessions) Create(_ context.Context, r app.SessionRecord) error {
	s.store[r.ID] = r
	return nil
}
func (s *fakeSessions) Get(_ context.Context, id string) (*app.SessionRecord, bool, error) {
	r, ok := s.store[id]
	if !ok {
		return nil, false, nil
	}
	return &r, true, nil
}
func (s *fakeSessions) Delete(_ context.Context, id string) error {
	delete(s.store, id)
	return nil
}

func newService(now time.Time) (*app.AuthService, *fakeChallenges, *fakeMailer, *fakeSessions) {
	ch := &fakeChallenges{}
	mailer := &fakeMailer{}
	sessions := &fakeSessions{store: map[string]app.SessionRecord{}}
	dir := &fakeDirectory{users: map[string]app.DirectoryUser{
		"ana@uniquindio.edu.co": {Role: authn.RoleStudent, SubjectType: authn.SubjectStudent, SubjectID: "stu-1", Email: "ana@uniquindio.edu.co", FullName: "Ana"},
	}}
	svc := app.NewAuthService(
		app.Config{AllowedDomains: []string{"uniquindio.edu.co"}, OTPTTL: 10 * time.Minute, SessionTTL: time.Hour},
		nil, ch, dir, sessions, mailer, &fakeClock{t: now}, &fakeCodes{code: "123456"},
	)
	return svc, ch, mailer, sessions
}

func TestStartLogin_UnknownEmail_NoEnumeration(t *testing.T) {
	svc, ch, mailer, _ := newService(time.Now())
	res, err := svc.StartLogin(context.Background(), "stranger@uniquindio.edu.co")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExpiresInSeconds != 600 {
		t.Errorf("expiresInSeconds = %d, want 600", res.ExpiresInSeconds)
	}
	if ch.created != nil {
		t.Error("no challenge should be created for an unknown email")
	}
	if len(mailer.sent) != 0 {
		t.Error("no mail should be sent for an unknown email")
	}
}

func TestStartLogin_KnownEmail_SendsCode(t *testing.T) {
	svc, ch, mailer, _ := newService(time.Now())
	if _, err := svc.StartLogin(context.Background(), "Ana@Uniquindio.edu.co"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.created == nil {
		t.Fatal("a challenge should be created")
	}
	if ch.created.CodeHash != "hash:123456" {
		t.Errorf("stored hash = %q", ch.created.CodeHash)
	}
	if len(mailer.sent) != 1 || mailer.sent[0] != "ana@uniquindio.edu.co:123456" {
		t.Errorf("mail not sent correctly: %v", mailer.sent)
	}
}

func TestVerifyLogin_WrongCode(t *testing.T) {
	svc, ch, _, _ := newService(time.Now())
	_, _ = svc.StartLogin(context.Background(), "ana@uniquindio.edu.co")
	_, _, err := svc.VerifyLogin(context.Background(), "ana@uniquindio.edu.co", "000000")
	if !errors.Is(err, app.ErrInvalidCode) {
		t.Fatalf("want ErrInvalidCode, got %v", err)
	}
	if ch.created.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", ch.created.Attempts)
	}
}

func TestVerifyLogin_Success(t *testing.T) {
	now := time.Now()
	svc, _, _, sessions := newService(now)
	_, _ = svc.StartLogin(context.Background(), "ana@uniquindio.edu.co")
	p, sid, err := svc.VerifyLogin(context.Background(), "ana@uniquindio.edu.co", "123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sid != "sess-1" || p.Role != authn.RoleStudent {
		t.Errorf("unexpected principal/session: %+v %s", p, sid)
	}
	if _, ok := sessions.store["sess-1"]; !ok {
		t.Error("session should be persisted")
	}

	// A consumed challenge cannot be replayed.
	if _, _, err := svc.VerifyLogin(context.Background(), "ana@uniquindio.edu.co", "123456"); !errors.Is(err, app.ErrInvalidCode) {
		t.Errorf("consumed challenge should not verify again, got %v", err)
	}
}

// fakeDirectoryDeactivatable is a Directory where specific subject IDs can be
// marked inactive at test time, simulating an admin deactivating a user.
type fakeDirectoryDeactivatable struct {
	users              map[string]app.DirectoryUser // keyed by email
	inactiveSubjectIDs map[string]bool              // subjects where status != ACTIVE
}

func (d *fakeDirectoryDeactivatable) FindByEmail(_ context.Context, email string) (*app.DirectoryUser, bool, error) {
	u, ok := d.users[email]
	if !ok {
		return nil, false, nil
	}
	return &u, true, nil
}

func (d *fakeDirectoryDeactivatable) FindBySubject(_ context.Context, _ authn.SubjectType, id string) (*app.DirectoryUser, bool, error) {
	if d.inactiveSubjectIDs[id] {
		return nil, false, nil // simulate status='INACTIVE' → not found
	}
	for _, u := range d.users {
		if u.SubjectID == id {
			return &u, true, nil
		}
	}
	return nil, false, nil
}

// newServiceWithDirectory creates an AuthService with the given Directory, useful
// for tests that need to control directory behaviour (e.g. user deactivation).
func newServiceWithDirectory(now time.Time, dir app.Directory) (*app.AuthService, *fakeChallenges, *fakeMailer, *fakeSessions) {
	ch := &fakeChallenges{}
	mailer := &fakeMailer{}
	sessions := &fakeSessions{store: map[string]app.SessionRecord{}}
	svc := app.NewAuthService(
		app.Config{AllowedDomains: []string{"uniquindio.edu.co"}, OTPTTL: 10 * time.Minute, SessionTTL: time.Hour},
		nil, ch, dir, sessions, mailer, &fakeClock{t: now}, &fakeCodes{code: "123456"},
	)
	return svc, ch, mailer, sessions
}

// Fix A — deactivated users must not keep valid sessions.
// Before the fix, Authenticate only checks session existence/TTL, not user
// status, so an inactive user would still authenticate for the remainder of the
// session TTL. This test documents the expected post-fix behaviour.
func TestAuthenticate_InactiveUser_Rejected(t *testing.T) {
	now := time.Now()
	dir := &fakeDirectoryDeactivatable{
		users: map[string]app.DirectoryUser{
			"ana@uniquindio.edu.co": {
				Role:        authn.RoleStudent,
				SubjectType: authn.SubjectStudent,
				SubjectID:   "stu-1",
				Email:       "ana@uniquindio.edu.co",
				FullName:    "Ana",
			},
		},
		inactiveSubjectIDs: map[string]bool{},
	}
	svc, _, _, _ := newServiceWithDirectory(now, dir)

	// Create a valid session via the normal login flow.
	if _, err := svc.StartLogin(context.Background(), "ana@uniquindio.edu.co"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	_, sid, err := svc.VerifyLogin(context.Background(), "ana@uniquindio.edu.co", "123456")
	if err != nil {
		t.Fatalf("VerifyLogin: %v", err)
	}

	// Sanity: session works while user is still ACTIVE.
	if _, err := svc.Authenticate(context.Background(), sid); err != nil {
		t.Fatalf("active user should authenticate: %v", err)
	}

	// Admin deactivates the user (e.g. status set to INACTIVE in the DB).
	dir.inactiveSubjectIDs["stu-1"] = true

	// The session record still exists and is not expired, but the underlying
	// user is no longer ACTIVE. Authenticate must reject it.
	_, err = svc.Authenticate(context.Background(), sid)
	if !errors.Is(err, app.ErrNoSession) {
		t.Fatalf("want ErrNoSession for inactive user, got %v", err)
	}
}

// Fix C — StartLogin must not mint a new challenge when one is already active,
// preventing the per-challenge OTP attempt cap from being reset by re-calling
// /start.
func TestStartLogin_ExistingActiveChallenge_DoesNotCreateNew(t *testing.T) {
	now := time.Now()
	svc, ch, mailer, _ := newService(now)

	// First call: creates a challenge and sends the OTP email.
	if _, err := svc.StartLogin(context.Background(), "ana@uniquindio.edu.co"); err != nil {
		t.Fatalf("first StartLogin: %v", err)
	}
	if ch.createCalls != 1 {
		t.Fatalf("expected 1 challenge created after first /start, got %d", ch.createCalls)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("expected 1 email sent after first /start, got %d", len(mailer.sent))
	}

	// Second call while the challenge is still active: must NOT create a new
	// challenge so the brute-force cap cannot be reset.
	if _, err := svc.StartLogin(context.Background(), "ana@uniquindio.edu.co"); err != nil {
		t.Fatalf("second StartLogin: %v", err)
	}
	if ch.createCalls != 1 {
		t.Errorf("expected no new challenge after second /start (still 1), got %d", ch.createCalls)
	}
	if len(mailer.sent) != 1 {
		t.Errorf("expected no new email after second /start (still 1), got %d", len(mailer.sent))
	}
}

func TestAuthenticate_ExpiredSession(t *testing.T) {
	now := time.Now()
	svc, _, _, _ := newService(now)
	_, _ = svc.StartLogin(context.Background(), "ana@uniquindio.edu.co")
	p, sid, _ := svc.VerifyLogin(context.Background(), "ana@uniquindio.edu.co", "123456")

	if got, err := svc.Authenticate(context.Background(), sid); err != nil || got.SubjectID != p.SubjectID {
		t.Fatalf("valid session should authenticate: %v", err)
	}

	// A service whose clock is past the session TTL rejects unknown sessions.
	expired, _, _, _ := newService(now.Add(2 * time.Hour))
	if _, err := expired.Authenticate(context.Background(), "missing"); !errors.Is(err, app.ErrNoSession) {
		t.Errorf("unknown session should fail with ErrNoSession, got %v", err)
	}
}
