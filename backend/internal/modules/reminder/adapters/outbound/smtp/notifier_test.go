package smtp

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

var _ ports.EmailNotifier = (*Notifier)(nil)

// selfSignedTLS returns a TLS config carrying a fresh self-signed
// certificate valid for the loopback IP, plus the parsed certificate so
// tests can add it as the client's sole trust anchor. Production keeps the
// system roots; only the injected test seams trust this certificate.
func selfSignedTLS(t *testing.T) (*tls.Config, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "artificial-brain test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(crand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, cert
}

// smtpServer is a scripted local listener speaking just enough SMTP for the
// standard-library client: greeting, EHLO, optional AUTH, optional STARTTLS
// upgrade, and optional implicit TLS from the first byte (port 465 style).
type smtpServer struct {
	addr          string
	rcptReply     string
	dotReply      string
	advertiseAuth bool
	startTLS      bool
	implicitTLS   bool
	tlsConfig     *tls.Config
	cert          *x509.Certificate

	mu       sync.Mutex
	data     strings.Builder
	commands []string
	authLine string
}

func startSMTPServer(t *testing.T, rcptReply string, advertiseAuth bool) *smtpServer {
	t.Helper()
	server := newSMTPServer(rcptReply, advertiseAuth)
	server.listenAndServe(t)
	return server
}

func startTLSSMTPServer(t *testing.T, implicitTLS, startTLS bool) *smtpServer {
	t.Helper()
	server := newSMTPServer("250 2.1.5 OK", true)
	server.startTLS = startTLS
	server.implicitTLS = implicitTLS
	server.tlsConfig, server.cert = selfSignedTLS(t)
	server.listenAndServe(t)
	return server
}

func newSMTPServer(rcptReply string, advertiseAuth bool) *smtpServer {
	return &smtpServer{
		rcptReply:     rcptReply,
		dotReply:      "250 2.0.0 OK queued as test-queued-1",
		advertiseAuth: advertiseAuth,
	}
}

func (s *smtpServer) listenAndServe(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	s.addr = ln.Addr().String()
	t.Cleanup(func() { ln.Close() })
	go s.serve(ln)
}

func (s *smtpServer) serve(ln net.Listener) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	if s.implicitTLS {
		tlsConn := tls.Server(conn, s.tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		conn = tlsConn
	}
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
			switch {
			case s.advertiseAuth && s.startTLS:
				fmt.Fprintf(conn, "250-artificial-brain\r\n250-AUTH PLAIN\r\n250-STARTTLS\r\n250 OK\r\n")
			case s.advertiseAuth:
				fmt.Fprintf(conn, "250-artificial-brain\r\n250-AUTH PLAIN\r\n250 OK\r\n")
			case s.startTLS:
				fmt.Fprintf(conn, "250-artificial-brain\r\n250-STARTTLS\r\n250 OK\r\n")
			default:
				fmt.Fprintf(conn, "250 artificial-brain\r\n")
			}
		case strings.HasPrefix(upper, "HELO"):
			fmt.Fprintf(conn, "250 artificial-brain\r\n")
		case strings.HasPrefix(upper, "STARTTLS"):
			if !s.startTLS {
				fmt.Fprintf(conn, "502 5.5.2 command not implemented\r\n")
				continue
			}
			fmt.Fprintf(conn, "220 Ready to start TLS\r\n")
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			reader = bufio.NewReader(conn)
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

func (s *smtpServer) commandList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

// caPool returns a trust pool holding only the server's test certificate.
func (s *smtpServer) caPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.cert)
	return pool
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

func TestSendGreeting554IsPermanentWithCode(t *testing.T) {
	// A server that refuses the connection in its greeting must classify as a
	// permanent 554, not a retried transport failure.
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
		fmt.Fprintf(conn, "554 service unavailable\r\n")
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", port, err)
	}
	notifier := New(Config{Host: host, Port: portNumber, From: "brain@example.com", Timeout: 5 * time.Second})

	_, err = notifier.Send(context.Background(), emailMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want permanent greeting refusal")
	}
	if !errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want errors.Is(err, ports.ErrPermanent)", err)
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

// TestSendStartTlsUpgradeAndAuth drives the real STARTTLS path against a
// listener with a self-signed certificate: the STARTTLS command is issued
// before AUTH, the handshake upgrades the connection, and the standard PLAIN
// auth then transmits the credentials. The permanent/transient classification
// is unaffected by the upgrade.
func TestSendStartTlsUpgradeAndAuth(t *testing.T) {
	server := startTLSSMTPServer(t, false, true)
	cfg := serverConfig(t, server)
	cfg.Username = "brain-user"
	cfg.Password = "secret-password"
	notifier := New(cfg)
	// Trust only the server's self-signed test certificate for the upgrade.
	notifier.startTLS = func(client *smtp.Client, config *tls.Config) error {
		config.RootCAs = server.caPool()
		return client.StartTLS(config)
	}

	result, err := notifier.Send(context.Background(), emailMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !strings.HasPrefix(result.ProviderMessageID, "smtp-") {
		t.Fatalf("ProviderMessageID = %q, want smtp- prefix", result.ProviderMessageID)
	}
	commands := server.commandList()
	startTLSIndex, authIndex := -1, -1
	for i, command := range commands {
		upper := strings.ToUpper(command)
		switch {
		case strings.HasPrefix(upper, "STARTTLS") && startTLSIndex == -1:
			startTLSIndex = i
		case strings.HasPrefix(upper, "AUTH") && authIndex == -1:
			authIndex = i
		}
	}
	if startTLSIndex == -1 {
		t.Fatalf("STARTTLS never issued, commands = %v", commands)
	}
	if authIndex == -1 || authIndex < startTLSIndex {
		t.Fatalf("AUTH must follow STARTTLS, commands = %v", commands)
	}
	authLine := server.auth()
	if !strings.HasPrefix(authLine, "AUTH PLAIN ") {
		t.Fatalf("AUTH command = %q, want AUTH PLAIN with initial response", authLine)
	}
	if strings.Contains(authLine, "secret-password") {
		t.Fatalf("AUTH command %q leaks the raw password", authLine)
	}
	if body := server.body(); !strings.Contains(body, "《提交周报》") {
		t.Fatalf("recorded body %q does not contain the 《title》 after the TLS upgrade", body)
	}
}

// TestSendImplicitTLSOverPort465 drives the real 465 path against a listener
// encrypted from the first byte: the implicit-TLS dial is used, no STARTTLS
// command is ever issued, and the local PLAIN auth transmits the credentials
// over the encrypted connection. The dial seam redirects port 465 to the
// ephemeral listener port while preserving the real TLS dial behavior.
func TestSendImplicitTLSOverPort465(t *testing.T) {
	server := startTLSSMTPServer(t, true, false)
	cfg := serverConfig(t, server)
	cfg.Port = 465
	cfg.Username = "brain-user"
	cfg.Password = "secret-password"
	notifier := New(cfg)
	var plainDials, tlsDials int
	notifier.dial = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		plainDials++
		return net.DialTimeout(network, addr, timeout)
	}
	notifier.dialTLS = func(_, _ string, timeout time.Duration) (net.Conn, error) {
		tlsDials++
		dialer := &net.Dialer{Timeout: timeout}
		// Redirect port 465 to the ephemeral listener and trust only the
		// server's self-signed test certificate.
		return tls.DialWithDialer(dialer, "tcp", server.addr, &tls.Config{
			ServerName: cfg.Host,
			RootCAs:    server.caPool(),
		})
	}

	result, err := notifier.Send(context.Background(), emailMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !strings.HasPrefix(result.ProviderMessageID, "smtp-") {
		t.Fatalf("ProviderMessageID = %q, want smtp- prefix", result.ProviderMessageID)
	}
	if plainDials != 0 || tlsDials != 1 {
		t.Fatalf("dials = %d plain, %d tls; want 0 plain, 1 tls on port 465", plainDials, tlsDials)
	}
	for _, command := range server.commandList() {
		if strings.HasPrefix(strings.ToUpper(command), "STARTTLS") {
			t.Fatalf("implicit-TLS conversation issued STARTTLS: %v", server.commandList())
		}
	}
	authLine := server.auth()
	if !strings.HasPrefix(authLine, "AUTH PLAIN ") {
		t.Fatalf("AUTH command = %q, want AUTH PLAIN with initial response", authLine)
	}
	if strings.Contains(authLine, "secret-password") {
		t.Fatalf("AUTH command %q leaks the raw password", authLine)
	}
	if body := server.body(); !strings.Contains(body, "《提交周报》") {
		t.Fatalf("recorded body %q does not contain the 《title》 over implicit TLS", body)
	}
}

// TestSendStartTlsHandshakeFailureIsTransient pins that a failed STARTTLS
// upgrade (the server breaks the handshake) is retried, not dead-lettered.
func TestSendStartTlsHandshakeFailureIsTransient(t *testing.T) {
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
		fmt.Fprintf(conn, "220 artificial-brain test ESMTP\r\n")
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(strings.ToUpper(strings.TrimRight(line, "\r\n")), "EHLO") {
				fmt.Fprintf(conn, "250-artificial-brain\r\n250-STARTTLS\r\n250 OK\r\n")
				continue
			}
			if strings.HasPrefix(strings.ToUpper(strings.TrimRight(line, "\r\n")), "STARTTLS") {
				fmt.Fprintf(conn, "220 Ready to start TLS\r\n")
				// Break the handshake: a real server certificate problem.
				return
			}
		}
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", port, err)
	}
	notifier := New(Config{Host: host, Port: portNumber, From: "brain@example.com", Timeout: 5 * time.Second})

	_, err = notifier.Send(context.Background(), emailMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want STARTTLS handshake failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient STARTTLS failure", err)
	}
}
