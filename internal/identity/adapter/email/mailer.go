// Package email implements the identity Mailer port. It sends login codes over
// SMTP (Mailpit in local development) and, in development, also logs the code so
// the flow can be exercised without opening the mailbox.
package email

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/uniquindio/profundiza-uq/internal/platform/smtpx"
)

// Mailer sends transactional email via SMTP.
type Mailer struct {
	addr    string // host:port, e.g. "localhost:1025"
	from    string
	devMode bool
	logger  *slog.Logger
}

// NewMailer builds a Mailer. When devMode is true the code is also logged.
func NewMailer(addr, from string, devMode bool, logger *slog.Logger) *Mailer {
	return &Mailer{addr: addr, from: from, devMode: devMode, logger: logger}
}

// SendLoginCode delivers the one-time code to the recipient.
func (m *Mailer) SendLoginCode(ctx context.Context, email, code string) error {
	subject := "Your Profundiza UQ sign-in code"
	body := fmt.Sprintf("Your one-time sign-in code is: %s\n\nIt expires shortly. If you did not request it, ignore this email.", code)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s", m.from, email, subject, body)

	if m.devMode {
		// Safe only in development: surfaces the code for local testing.
		m.logger.InfoContext(ctx, "login_code_issued_dev", slog.String("email", email), slog.String("code", code))
	}

	if m.addr == "" {
		return nil // no SMTP configured; dev log above is enough
	}
	// smtpx.SendMail (not net/smtp.SendMail) enforces a hard timeout derived
	// from ctx (or a sane default) so a stalled/black-holed relay cannot block
	// this call — and therefore the HTTP handler goroutine — forever.
	if err := smtpx.SendMail(ctx, m.addr, nil, m.from, []string{email}, []byte(msg)); err != nil {
		// In development the code is already surfaced in the log, so a missing
		// local SMTP server must not block the login flow. In other
		// environments a delivery failure is a real, surfaced error.
		if m.devMode {
			m.logger.WarnContext(ctx, "login_code_smtp_unavailable_dev", slog.String("email", email), slog.Any("error", err))
			return nil
		}
		m.logger.ErrorContext(ctx, "login_code_send_failed", slog.String("email", email), slog.Any("error", err))
		return err
	}
	return nil
}
