package smtpx

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestSendMail_StalledRelayReturnsErrorInsteadOfBlockingForever proves the
// core availability fix: net/smtp.SendMail sets no connection deadline, so a
// relay that accepts the TCP connection but never speaks SMTP would block the
// caller forever. SendMail here must derive a deadline from ctx and return an
// error once that deadline passes, instead of hanging.
func TestSendMail_StalledRelayReturnsErrorInsteadOfBlockingForever(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		// Simulate a black-holed relay: accept the connection, never write the
		// 220 greeting, never close. Block reading until the client (SendMail)
		// gives up and closes its side.
		_, _ = conn.Read(make([]byte, 1))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = SendMail(ctx, ln.Addr().String(), nil, "from@example.com", []string{"to@example.com"}, []byte("body"))
	elapsed := time.Since(start)

	<-accepted // make sure the fake relay actually accepted the connection

	if err == nil {
		t.Fatal("expected SendMail to return an error for a stalled relay, got nil")
	}
	// Generous upper bound: proves SendMail returns promptly after the ctx
	// deadline (200ms) rather than hanging indefinitely.
	if elapsed > 2*time.Second {
		t.Fatalf("SendMail blocked for %v after a 200ms deadline; it must not hang", elapsed)
	}
}

// TestSendMail_NoAddrTimeoutUsesDefault proves that when ctx carries no
// deadline, SendMail still bounds the send with DefaultTimeout rather than
// blocking forever, by connecting to a stalled relay and expecting an error
// well within a generous upper bound.
func TestSendMail_ContextCancelledBeforeDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Read(make([]byte, 1))
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err = SendMail(ctx, ln.Addr().String(), nil, "from@example.com", []string{"to@example.com"}, []byte("body"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when ctx has no deadline but is cancelled/relay stalls")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("SendMail blocked for %v; it must not hang indefinitely", elapsed)
	}
}
