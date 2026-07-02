// Package email implements the notification Sender port over SMTP (Mailpit in
// local development).
package email

import (
	"context"
	"fmt"

	"github.com/uniquindio/profundiza-uq/internal/platform/smtpx"
)

// SMTPSender sends notification emails over SMTP.
type SMTPSender struct {
	addr string
	from string
}

// NewSMTPSender builds an SMTPSender. An empty addr makes Send a no-op success
// (useful when no SMTP is configured).
func NewSMTPSender(addr, from string) *SMTPSender {
	return &SMTPSender{addr: addr, from: from}
}

// Send delivers a plain-text email. The send is bounded by a hard timeout
// derived from ctx (or a sane default) so a stalled/black-holed relay returns
// an error instead of blocking the caller forever.
func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	if s.addr == "" {
		return nil
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		s.from, to, subject, body)
	return smtpx.SendMail(ctx, s.addr, nil, s.from, []string{to}, []byte(msg))
}
