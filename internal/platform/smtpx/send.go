// Package smtpx provides a context-aware, timeout-bounded SMTP send used by
// the identity and notification email adapters.
//
// net/smtp.SendMail dials and talks to the server with no connection or read
// deadline and takes no context.Context, so a stalled or black-holed SMTP
// relay blocks the caller forever. SendMail here is a drop-in replacement
// that mirrors the behavior of net/smtp.SendMail (EHLO/HELO, opportunistic
// STARTTLS, AUTH, MAIL/RCPT/DATA, QUIT) but enforces a hard deadline on the
// underlying connection and aborts early if ctx is cancelled.
package smtpx

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"time"
)

// DefaultTimeout bounds a send when ctx carries no deadline.
const DefaultTimeout = 10 * time.Second

// SendMail sends msg from "from" to the "to" addresses via the SMTP server at
// addr. auth may be nil. The deadline enforced on the connection is taken
// from ctx when it has one; otherwise DefaultTimeout is used. If ctx is
// cancelled before that deadline, the connection is closed immediately so a
// blocked read/write returns an error rather than waiting out the full
// deadline.
func SendMail(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(DefaultTimeout)
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return err
	}

	// Make the connection cancellable via ctx even when ctx has no deadline
	// (or a deadline later than the caller wants to wait): closing the
	// connection unblocks any in-flight Read/Write with an error.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-watchDone:
		}
	}()

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		_ = conn.Close()
		return err
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("smtpx: server does not support AUTH")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
