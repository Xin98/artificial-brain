package smtpoutbox

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// scriptedConn is a fake net.Conn: responses are pre-loaded line by line and
// served via Read, while every byte the client writes is captured for
// assertions.
type scriptedConn struct {
	mu        sync.Mutex
	responses []byte
	written   bytes.Buffer
}

func newScriptedConn(lines []string) *scriptedConn {
	var buf bytes.Buffer
	for _, l := range lines {
		buf.WriteString(l)
	}
	return &scriptedConn{responses: buf.Bytes()}
}

func (c *scriptedConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responses) == 0 {
		return 0, io.EOF
	}
	n := copy(b, c.responses)
	c.responses = c.responses[n:]
	return n, nil
}

func (c *scriptedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.written.Write(b)
}

func (c *scriptedConn) Close() error { return nil }

func (c *scriptedConn) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (c *scriptedConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func (c *scriptedConn) SetDeadline(t time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(t time.Time) error { return nil }

func (c *scriptedConn) transcript() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.written.String()
}

func loginMessage() ports.OutboxMessage {
	return ports.OutboxMessage{
		Address: "admin@example.com", Channel: "email", Purpose: "login", Code: "123456",
	}
}

// TestWriteSendsLoginCodeEmailOverImplicitTLS pins the port 465 path: the
// implicit-TLS dial is used, the conversation completes, and no STARTTLS
// command is ever issued.
func TestWriteSendsLoginCodeEmailOverImplicitTLS(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250 smtp.example.com\r\n", // EHLO reply
		"250 OK\r\n",               // MAIL FROM
		"250 OK\r\n",               // RCPT TO
		"354 End data with <CR LF>.<CR LF>\r\n",
		"250 queued\r\n",
		"221 bye\r\n",
	})
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com", Timeout: 5 * time.Second})
	var plainDials, tlsDials int
	outbox.dial = func(_, _ string, _ time.Duration) (net.Conn, error) {
		plainDials++
		return conn, nil
	}
	outbox.dialTLS = func(_, _ string, _ time.Duration) (net.Conn, error) {
		tlsDials++
		return conn, nil
	}

	err := outbox.Write(context.Background(), loginMessage())
	if err != nil {
		t.Fatalf("Write = %v", err)
	}
	if plainDials != 0 || tlsDials != 1 {
		t.Fatalf("dials = %d plain, %d tls; want 0 plain, 1 tls on port 465", plainDials, tlsDials)
	}
	transcript := conn.transcript()
	for _, want := range []string{"MAIL FROM:<noreply@example.com>", "RCPT TO:<admin@example.com>", "Subject: 登录验证码", "123456"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("transcript missing %q:\n%s", want, transcript)
		}
	}
	if strings.Contains(transcript, "STARTTLS") {
		t.Fatalf("implicit-TLS transcript must not issue STARTTLS:\n%s", transcript)
	}
}

// TestWriteAuthenticatesOverImplicitTLS pins AUTH on the 465 path: the local
// PLAIN auth emits the credentials (base64, NUL-separated) even though the
// connection was never STARTTLS-upgraded, and the raw password never appears.
func TestWriteAuthenticatesOverImplicitTLS(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250-smtp.example.com\r\n250-AUTH PLAIN\r\n250 OK\r\n",
		"235 2.7.0 Authentication successful\r\n",
		"250 OK\r\n", // MAIL FROM
		"250 OK\r\n", // RCPT TO
		"354 End data with <CR LF>.<CR LF>\r\n",
		"250 queued\r\n",
		"221 bye\r\n",
	})
	outbox := New(Config{
		Host: "smtp.example.com", Port: 465,
		Username: "brain-user", Password: "secret-password",
		From: "noreply@example.com", Timeout: 5 * time.Second,
	})
	outbox.dialTLS = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }

	if err := outbox.Write(context.Background(), loginMessage()); err != nil {
		t.Fatalf("Write = %v", err)
	}
	transcript := conn.transcript()
	wantAuth := "AUTH PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00brain-user\x00secret-password"))
	if !strings.Contains(transcript, wantAuth) {
		t.Fatalf("transcript missing %q:\n%s", wantAuth, transcript)
	}
	if strings.Contains(transcript, "secret-password") {
		t.Fatalf("transcript leaks the raw password:\n%s", transcript)
	}
	if !strings.Contains(transcript, "MAIL FROM:<noreply@example.com>") {
		t.Fatalf("send did not continue after AUTH:\n%s", transcript)
	}
}

// TestWriteStartTlsUpgradeInvokedWhenAdvertised pins the 587-style decision:
// when the server advertises STARTTLS on a non-465 port, the upgrade seam is
// invoked with the dialed host as ServerName.
func TestWriteStartTlsUpgradeInvokedWhenAdvertised(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250-smtp.example.com\r\n250-STARTTLS\r\n250 OK\r\n",
		"250 OK\r\n", // MAIL FROM
		"250 OK\r\n", // RCPT TO
		"354 End data with <CR LF>.<CR LF>\r\n",
		"250 queued\r\n",
		"221 bye\r\n",
	})
	outbox := New(Config{Host: "smtp.example.com", Port: 587, From: "noreply@example.com", Timeout: 5 * time.Second})
	outbox.dial = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }
	var startTLSCalls []*tls.Config
	outbox.startTLS = func(_ *smtp.Client, config *tls.Config) error {
		startTLSCalls = append(startTLSCalls, config)
		return nil
	}

	if err := outbox.Write(context.Background(), loginMessage()); err != nil {
		t.Fatalf("Write = %v", err)
	}
	if len(startTLSCalls) != 1 {
		t.Fatalf("startTLS invoked %d times, want 1 when STARTTLS is advertised", len(startTLSCalls))
	}
	if startTLSCalls[0].ServerName != "smtp.example.com" {
		t.Fatalf("startTLS ServerName = %q, want the dialed host", startTLSCalls[0].ServerName)
	}
}

// TestWriteSkipsStartTlsOnImplicitTLSPort pins that port 465 never attempts
// STARTTLS even if the server advertises it: the seam must not run and no
// STARTTLS command may appear.
func TestWriteSkipsStartTlsOnImplicitTLSPort(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250-smtp.example.com\r\n250-STARTTLS\r\n250 OK\r\n",
		"250 OK\r\n", // MAIL FROM
		"250 OK\r\n", // RCPT TO
		"354 End data with <CR LF>.<CR LF>\r\n",
		"250 queued\r\n",
		"221 bye\r\n",
	})
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com", Timeout: 5 * time.Second})
	outbox.dialTLS = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }
	startTLSCalls := 0
	outbox.startTLS = func(_ *smtp.Client, _ *tls.Config) error {
		startTLSCalls++
		return nil
	}

	if err := outbox.Write(context.Background(), loginMessage()); err != nil {
		t.Fatalf("Write = %v", err)
	}
	if startTLSCalls != 0 {
		t.Fatalf("startTLS invoked %d times on port 465, want 0", startTLSCalls)
	}
	if transcript := conn.transcript(); strings.Contains(transcript, "STARTTLS") {
		t.Fatalf("implicit-TLS transcript must not issue STARTTLS:\n%s", transcript)
	}
}

// TestWriteRefusesAuthOnUnencryptedConnection pins fail-closed: over a plain
// connection with no STARTTLS upgrade, credentials must never be sent — the
// standard PLAIN auth refuses and the send fails.
func TestWriteRefusesAuthOnUnencryptedConnection(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250 smtp.example.com\r\n",
	})
	outbox := New(Config{
		Host: "smtp.example.com", Port: 587,
		Username: "brain-user", Password: "secret-password",
		From: "noreply@example.com", Timeout: 5 * time.Second,
	})
	outbox.dial = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }

	err := outbox.Write(context.Background(), loginMessage())
	if !errors.Is(err, domain.ErrCodeDeliveryFailed) {
		t.Fatalf("Write = %v, want ErrCodeDeliveryFailed", err)
	}
	if transcript := conn.transcript(); strings.Contains(transcript, "AUTH") || strings.Contains(transcript, "secret-password") {
		t.Fatalf("credentials sent over an unencrypted connection:\n%s", transcript)
	}
}

func TestWriteRejectsNonEmailMessage(t *testing.T) {
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com"})
	err := outbox.Write(context.Background(), ports.OutboxMessage{
		Address: "+8613800138000", Channel: "sms", Purpose: "login", Code: "123456",
	})
	if !errors.Is(err, domain.ErrSmsUnavailable) {
		t.Fatalf("Write = %v, want ErrSmsUnavailable", err)
	}
}

func TestWritePermanentRefusalWrapsDeliveryFailed(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250 smtp.example.com\r\n",
		"550 mailbox unavailable\r\n", // MAIL FROM refused
	})
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com", Timeout: 5 * time.Second})
	outbox.dialTLS = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }

	err := outbox.Write(context.Background(), loginMessage())
	if !errors.Is(err, domain.ErrCodeDeliveryFailed) {
		t.Fatalf("Write = %v, want ErrCodeDeliveryFailed", err)
	}
}

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

// tlsSMTPServer is a scripted listener speaking enough SMTP for the standard
// library client, optionally encrypted from the first byte (implicitTLS) or
// upgradeable via STARTTLS (startTLS).
type tlsSMTPServer struct {
	addr        string
	implicitTLS bool
	startTLS    bool
	tlsConfig   *tls.Config
	cert        *x509.Certificate

	mu       sync.Mutex
	commands []string
	authLine string
	data     strings.Builder
}

func startTLSSMTPServer(t *testing.T, implicitTLS, startTLS bool) *tlsSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	tlsConfig, cert := selfSignedTLS(t)
	server := &tlsSMTPServer{
		addr:        ln.Addr().String(),
		implicitTLS: implicitTLS,
		startTLS:    startTLS,
		tlsConfig:   tlsConfig,
		cert:        cert,
	}
	t.Cleanup(func() { ln.Close() })
	go server.serve(ln)
	return server
}

// caPool returns a trust pool holding only the server's test certificate.
func (s *tlsSMTPServer) caPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.cert)
	return pool
}

func (s *tlsSMTPServer) serve(ln net.Listener) {
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
				fmt.Fprintf(conn, "250 queued\r\n")
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
			fmt.Fprintf(conn, "250-artificial-brain\r\n250-AUTH PLAIN\r\n250-STARTTLS\r\n250 OK\r\n")
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
			fmt.Fprintf(conn, "250 2.1.5 OK\r\n")
		case strings.HasPrefix(upper, "DATA"):
			fmt.Fprintf(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
			inData = true
		case strings.HasPrefix(upper, "QUIT"):
			fmt.Fprintf(conn, "221 2.0.0 Bye\r\n")
			return
		default:
			fmt.Fprintf(conn, "502 5.5.2 Command not recognized\r\n")
		}
	}
}

func (s *tlsSMTPServer) commandList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

func (s *tlsSMTPServer) auth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authLine
}

func (s *tlsSMTPServer) body() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.String()
}

func listenerConfig(t *testing.T, server *tlsSMTPServer, port int) Config {
	t.Helper()
	host, listenerPort, err := net.SplitHostPort(server.addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", server.addr, err)
	}
	if port == 0 {
		port, err = strconv.Atoi(listenerPort)
		if err != nil {
			t.Fatalf("Atoi(%q) error = %v", listenerPort, err)
		}
	}
	return Config{Host: host, Port: port, From: "noreply@example.com", Timeout: 5 * time.Second}
}

// TestWriteStartTlsThenAuthEndToEnd drives the real STARTTLS path against a
// listener with a self-signed certificate: the STARTTLS command is issued
// before AUTH, the handshake upgrades the connection, and the standard PLAIN
// auth then transmits the credentials.
func TestWriteStartTlsThenAuthEndToEnd(t *testing.T) {
	server := startTLSSMTPServer(t, false, true)
	cfg := listenerConfig(t, server, 0)
	cfg.Username = "brain-user"
	cfg.Password = "secret-password"
	outbox := New(cfg)
	// Trust only the server's self-signed test certificate for the upgrade.
	outbox.startTLS = func(client *smtp.Client, config *tls.Config) error {
		config.RootCAs = server.caPool()
		return client.StartTLS(config)
	}

	if err := outbox.Write(context.Background(), loginMessage()); err != nil {
		t.Fatalf("Write = %v", err)
	}
	commands := server.commandList()
	startTLSIndex, authIndex, mailIndex := -1, -1, -1
	for i, command := range commands {
		upper := strings.ToUpper(command)
		switch {
		case strings.HasPrefix(upper, "STARTTLS") && startTLSIndex == -1:
			startTLSIndex = i
		case strings.HasPrefix(upper, "AUTH") && authIndex == -1:
			authIndex = i
		case strings.HasPrefix(upper, "MAIL") && mailIndex == -1:
			mailIndex = i
		}
	}
	if startTLSIndex == -1 {
		t.Fatalf("STARTTLS never issued, commands = %v", commands)
	}
	if authIndex == -1 || authIndex < startTLSIndex {
		t.Fatalf("AUTH must follow STARTTLS, commands = %v", commands)
	}
	if mailIndex == -1 || mailIndex < authIndex {
		t.Fatalf("MAIL must follow AUTH, commands = %v", commands)
	}
	authLine := server.auth()
	if !strings.HasPrefix(authLine, "AUTH PLAIN ") {
		t.Fatalf("AUTH command = %q, want AUTH PLAIN with initial response", authLine)
	}
	if strings.Contains(authLine, "secret-password") {
		t.Fatalf("AUTH command %q leaks the raw password", authLine)
	}
	if body := server.body(); !strings.Contains(body, "123456") {
		t.Fatalf("server body %q does not carry the code", body)
	}
}

// TestWriteImplicitTLSAuthEndToEnd drives the real 465 path against a
// listener encrypted from the first byte: no STARTTLS command is issued and
// the local PLAIN auth transmits the credentials over the encrypted
// connection. The dial seam redirects port 465 to the ephemeral listener
// port while preserving the real TLS dial behavior.
func TestWriteImplicitTLSAuthEndToEnd(t *testing.T) {
	server := startTLSSMTPServer(t, true, false)
	cfg := listenerConfig(t, server, 465)
	cfg.Username = "brain-user"
	cfg.Password = "secret-password"
	outbox := New(cfg)
	outbox.dialTLS = func(_, _ string, timeout time.Duration) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: timeout}
		// Trust only the server's self-signed test certificate.
		return tls.DialWithDialer(dialer, "tcp", server.addr, &tls.Config{
			ServerName: cfg.Host,
			RootCAs:    server.caPool(),
		})
	}

	if err := outbox.Write(context.Background(), loginMessage()); err != nil {
		t.Fatalf("Write = %v", err)
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
	if body := server.body(); !strings.Contains(body, "123456") {
		t.Fatalf("server body %q does not carry the code", body)
	}
}
