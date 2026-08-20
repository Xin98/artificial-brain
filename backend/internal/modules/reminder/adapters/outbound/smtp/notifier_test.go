package smtp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

var _ ports.EmailNotifier = (*Notifier)(nil)

// smtpServer is a scripted local listener speaking just enough SMTP for the
// standard-library client: greeting, EHLO, optional AUTH, MAIL/RCPT/DATA.
type smtpServer struct {
	addr          string
	rcptReply     string
	dotReply      string
	advertiseAuth bool

	mu       sync.Mutex
	data     strings.Builder
	commands []string
	authLine string
}

func startSMTPServer(t *testing.T, rcptReply string, advertiseAuth bool) *smtpServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := &smtpServer{
		addr:          ln.Addr().String(),
		rcptReply:     rcptReply,
		dotReply:      "250 2.0.0 OK queued as test-queued-1",
		advertiseAuth: advertiseAuth,
	}
	t.Cleanup(func() { ln.Close() })
	go server.serve(ln)
	return server
}

func (s *smtpServer) serve(ln net.Listener) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	fmt.Fprintf(conn, "220 artificial-brain test ESMTP\r\n")
	reader := bufio.NewReader(conn)
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(trimmed)
		if inData {
			if trimmed == "." {
				inData = false
				fmt.Fprintf(conn, "%s\r\n", s.dotReply)
				continue
			}
			s.mu.Lock()
			s.data.WriteString(line)
			s.mu.Unlock()
			continue
		}
		s.mu.Lock()
		s.commands = append(s.commands, trimmed)
		s.mu.Unlock()
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			if s.advertiseAuth {
				fmt.Fprintf(conn, "250-artificial-brain\r\n250-AUTH PLAIN\r\n250 OK\r\n")
			} else {
				fmt.Fprintf(conn, "250 artificial-brain\r\n")
			}
		case strings.HasPrefix(upper, "HELO"):
			fmt.Fprintf(conn, "250 artificial-brain\r\n")
		case strings.HasPrefix(upper, "AUTH"):
			s.mu.Lock()
			s.authLine = trimmed
			s.mu.Unlock()
			fmt.Fprintf(conn, "235 2.7.0 Authentication successful\r\n")
		case strings.HasPrefix(upper, "MAIL"):
			fmt.Fprintf(conn, "250 2.1.0 OK\r\n")
		case strings.HasPrefix(upper, "RCPT"):
			fmt.Fprintf(conn, "%s\r\n", s.rcptReply)
		case strings.HasPrefix(upper, "DATA"):
			fmt.Fprintf(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
			inData = true
		case strings.HasPrefix(upper, "RSET"), strings.HasPrefix(upper, "NOOP"):
			fmt.Fprintf(conn, "250 2.0.0 OK\r\n")
		case strings.HasPrefix(upper, "QUIT"):
			fmt.Fprintf(conn, "221 2.0.0 Bye\r\n")
			return
		default:
			fmt.Fprintf(conn, "502 5.5.2 Command not recognized\r\n")
		}
	}
}

func (s *smtpServer) body() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.String()
}

func (s *smtpServer) auth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authLine
}

func serverConfig(t *testing.T, server *smtpServer) Config {
	t.Helper()
	host, port, err := net.SplitHostPort(server.addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", server.addr, err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", port, err)
	}
	return Config{Host: host, Port: portNumber, From: "brain@example.com", Timeout: 5 * time.Second}
}

func emailMessage() dto.ReminderMessage {
	return dto.ReminderMessage{
		To:             "alice@example.com",
		TodoID:         "todo-123",
		Title:          "提交周报",
		ScheduledAtUTC: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC),
	}
}

func TestSendHappyPathRecordsBody(t *testing.T) {
	server := startSMTPServer(t, "250 2.1.5 OK", false)
	notifier := New(serverConfig(t, server))

	result, err := notifier.Send(context.Background(), emailMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !strings.HasPrefix(result.ProviderMessageID, "smtp-") {
		t.Fatalf("ProviderMessageID = %q, want smtp- prefix", result.ProviderMessageID)
	}
	body := server.body()
	if !strings.Contains(body, "《提交周报》") {
		t.Fatalf("recorded body %q does not contain the 《title》", body)
	}
	if !strings.Contains(body, "From: brain@example.com") || !strings.Contains(body, "To: alice@example.com") {
		t.Fatalf("recorded body %q missing From/To headers", body)
	}
	if !strings.Contains(body, "Subject: 工作台提醒") {
		t.Fatalf("recorded body %q missing the reminder subject", body)
	}
	if !strings.Contains(body, "Date: ") {
		t.Fatalf("recorded body %q missing the Date header", body)
	}
	if !strings.Contains(body, "2026-08-20T08:00:00Z") {
		t.Fatalf("recorded body %q does not contain the UTC instant", body)
	}
}

func TestSendRcpt550IsPermanentWithCode(t *testing.T) {
	server := startSMTPServer(t, "550 5.1.1 no such user", false)
	notifier := New(serverConfig(t, server))

	_, err := notifier.Send(context.Background(), emailMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want permanent refusal")
	}
	if !errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want errors.Is(err, ports.ErrPermanent)", err)
	}
	var permanent *ports.PermanentError
	if !errors.As(err, &permanent) || permanent.Code != "550" {
		t.Fatalf("Send() error = %v, want PermanentError with code 550", err)
	}
	if body := server.body(); body != "" {
		t.Fatalf("recorded body = %q, want no DATA after refused RCPT", body)
	}
}

func TestSendRcpt452IsTransient(t *testing.T) {
	server := startSMTPServer(t, "452 4.5.3 too many recipients", false)
	notifier := New(serverConfig(t, server))

	_, err := notifier.Send(context.Background(), emailMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want transient failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient, not permanent", err)
	}
}

func TestSendData554IsPermanent(t *testing.T) {
	// A server that accepts the recipient but rejects the message content at
	// the end of DATA still classifies as permanent via the 5xx dot reply.
	server := startSMTPServer(t, "250 2.1.5 OK", false)
	server.dotReply = "554 5.6.0 message rejected"
	notifier := New(serverConfig(t, server))

	_, err := notifier.Send(context.Background(), emailMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want permanent content refusal")
	}
	var permanent *ports.PermanentError
	if !errors.As(err, &permanent) || permanent.Code != "554" {
		t.Fatalf("Send() error = %v, want PermanentError with code 554", err)
	}
}

func TestSendDialRefusedIsTransient(t *testing.T) {
	// Grab a port and immediately free it: dialing it refuses the connection.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", addr, err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", port, err)
	}
	notifier := New(Config{Host: host, Port: portNumber, From: "brain@example.com", Timeout: 2 * time.Second})

	_, err = notifier.Send(context.Background(), emailMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want dial refusal")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient dial refusal", err)
	}
}

func TestSendHonorsContextDeadline(t *testing.T) {
	// The listener greets and then stalls: the client must hit the context
	// deadline rather than hang.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "220 stall ESMTP\r\n")
		time.Sleep(3 * time.Second)
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", port, err)
	}
	notifier := New(Config{Host: host, Port: portNumber, From: "brain@example.com", Timeout: 10 * time.Second})

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(150*time.Millisecond))
	defer cancel()
	start := time.Now()
	_, err = notifier.Send(ctx, emailMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want deadline failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient deadline failure", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Send() took %v, want the ~150ms context deadline honored", elapsed)
	}
}

func TestSendUsesPlainAuthWhenUsernameSet(t *testing.T) {
	server := startSMTPServer(t, "250 2.1.5 OK", true)
	cfg := serverConfig(t, server)
	cfg.Username = "brain-user"
	cfg.Password = "secret-password"
	notifier := New(cfg)

	if _, err := notifier.Send(context.Background(), emailMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	authLine := server.auth()
	if !strings.HasPrefix(authLine, "AUTH PLAIN ") {
		t.Fatalf("AUTH command = %q, want AUTH PLAIN with initial response", authLine)
	}
	if strings.Contains(authLine, "secret-password") {
		t.Fatalf("AUTH command %q leaks the raw password", authLine)
	}
}

func TestSendInjectableDialSurfacesError(t *testing.T) {
	dialErr := errors.New("injected dial failure")
	notifier := New(Config{Host: "smtp.invalid", Port: 2525, From: "brain@example.com", Timeout: time.Second})
	notifier.dial = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		return nil, dialErr
	}

	_, err := notifier.Send(context.Background(), emailMessage())
	if !errors.Is(err, dialErr) {
		t.Fatalf("Send() error = %v, want the injected dial error", err)
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient dial failure", err)
	}
}
