// Package smtptest is a small in-process SMTP server for tests: STARTTLS
// or implicit TLS with a generated certificate, AUTH PLAIN, and scripted
// failure replies per command. Never used outside tests.
package smtptest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Mode is how the server offers TLS.
type Mode int

// Modes.
const (
	StartTLS Mode = iota // plain greeting, STARTTLS offered (port 587 style)
	Implicit             // TLS from the first byte (port 465 style)
	Plain                // no TLS at all (dev relay)
)

// Options configure the server.
type Options struct {
	Mode       Mode
	User, Pass string   // empty: no AUTH required
	Names      []string // certificate DNS names (default smtp.example.test)
	NoStartTLS bool     // StartTLS mode, but the extension is not offered
}

// Received is one accepted message.
type Received struct {
	From string
	To   []string
	Data []byte
}

// Server is a running test server.
type Server struct {
	Port    int
	Addr    netip.AddrPort
	RootCAs *x509.CertPool

	opt  Options
	cert tls.Certificate
	ln   net.Listener

	mu       sync.Mutex
	messages []Received
	fail     map[string]string
	conns    int
}

// Start runs a server on 127.0.0.1 until the test ends.
func Start(t testing.TB, opt Options) *Server {
	t.Helper()
	if len(opt.Names) == 0 {
		opt.Names = []string{"smtp.example.test"}
	}
	s := &Server{opt: opt, fail: map[string]string{}}
	s.cert, s.RootCAs = selfSigned(t, opt.Names)
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	s.Addr = netip.MustParseAddrPort(ln.Addr().String())
	s.Port = int(s.Addr.Port())
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

// FailAt makes the server answer cmd ("AUTH", "MAIL", "RCPT", "DATA" for
// the reply after the data) with reply, e.g. "550 5.1.1 no such user".
// An empty reply clears it.
func (s *Server) FailAt(cmd, reply string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reply == "" {
		delete(s.fail, cmd)
		return
	}
	s.fail[cmd] = reply
}

// Messages returns the accepted messages.
func (s *Server) Messages() []Received {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Received(nil), s.messages...)
}

// Connections counts accepted connections.
func (s *Server) Connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns
}

func (s *Server) failure(cmd string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fail[cmd]
}

func (s *Server) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns++
		s.mu.Unlock()
		go s.handle(c)
	}
}

func (s *Server) tlsConfig() *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12}
}

func (s *Server) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	isTLS := false
	if s.opt.Mode == Implicit {
		tc := tls.Server(c, s.tlsConfig())
		if err := tc.HandshakeContext(context.Background()); err != nil {
			return
		}
		c, isTLS = tc, true
	}
	r := bufio.NewReader(c)
	w := func(line string) { _, _ = c.Write([]byte(line + "\r\n")) }
	w("220 smtptest ESMTP")
	authed := s.opt.User == ""
	var cur Received
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			w("250-smtptest")
			if s.opt.Mode == StartTLS && !isTLS && !s.opt.NoStartTLS {
				w("250-STARTTLS")
			}
			if s.opt.User != "" && (isTLS || s.opt.Mode == Plain) {
				w("250-AUTH PLAIN")
			}
			w("250 8BITMIME")
		case "STARTTLS":
			if s.opt.Mode != StartTLS || isTLS || s.opt.NoStartTLS {
				w("502 5.5.1 not supported")
				continue
			}
			w("220 2.0.0 ready")
			tc := tls.Server(c, s.tlsConfig())
			if err := tc.HandshakeContext(context.Background()); err != nil {
				return
			}
			c, isTLS = tc, true
			r = bufio.NewReader(c)
		case "AUTH":
			if f := s.failure("AUTH"); f != "" {
				w(f)
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 2 || strings.ToUpper(parts[1]) != "PLAIN" {
				w("504 5.5.4 unsupported mechanism")
				continue
			}
			resp := ""
			if len(parts) >= 3 {
				resp = parts[2]
			} else {
				w("334 ")
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				resp = strings.TrimSpace(l)
			}
			raw, _ := base64.StdEncoding.DecodeString(resp)
			f := bytes.Split(raw, []byte{0})
			if len(f) == 3 && string(f[1]) == s.opt.User && string(f[2]) == s.opt.Pass {
				authed = true
				w("235 2.7.0 authenticated")
			} else {
				w("535 5.7.8 authentication credentials invalid")
			}
		case "MAIL":
			if !authed {
				w("530 5.7.0 authentication required")
				continue
			}
			if f := s.failure("MAIL"); f != "" {
				w(f)
				continue
			}
			cur = Received{From: addr(line)}
			w("250 2.1.0 ok")
		case "RCPT":
			if f := s.failure("RCPT"); f != "" {
				w(f)
				continue
			}
			cur.To = append(cur.To, addr(line))
			w("250 2.1.5 ok")
		case "DATA":
			w("354 end with <CRLF>.<CRLF>")
			var buf bytes.Buffer
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				buf.WriteString(strings.TrimPrefix(l, "."))
			}
			if f := s.failure("DATA"); f != "" {
				w(f)
				continue
			}
			cur.Data = buf.Bytes()
			s.mu.Lock()
			s.messages = append(s.messages, cur)
			s.mu.Unlock()
			w("250 2.0.0 queued as " + strconv.Itoa(len(s.Messages())))
		case "RSET":
			cur = Received{}
			w("250 2.0.0 ok")
		case "NOOP":
			w("250 2.0.0 ok")
		case "QUIT":
			w("221 2.0.0 bye")
			return
		default:
			w("502 5.5.2 command not recognized")
		}
	}
}

func addr(line string) string {
	i, j := strings.Index(line, "<"), strings.Index(line, ">")
	if i < 0 || j < i {
		return ""
	}
	return line[i+1 : j]
}

func selfSigned(t testing.TB, names []string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: names[0]},
		DNSNames:              names,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}
