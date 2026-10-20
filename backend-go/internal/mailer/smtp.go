package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"regexp"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

// TLSMode is how the connection is encrypted.
type TLSMode string

// TLS modes. TLSNone exists only for the dev relay (Mailpit); config
// refuses it in production and tenant settings never use it.
const (
	TLSImplicit TLSMode = "implicit" // port 465
	TLSStartTLS TLSMode = "starttls" // port 587, STARTTLS mandatory
	TLSNone     TLSMode = "none"
)

// Server is an SMTP submission server.
type Server struct {
	Host     string
	Port     int
	TLS      TLSMode
	Username string
	Password secrets.Secret
}

// Options configure a sender.
type Options struct {
	Guard       Guard
	HELO        string         // host name sent in EHLO; default "localhost"
	RootCAs     *x509.CertPool // nil: system roots (tests pass their own)
	SendTimeout time.Duration  // whole send; default 30 s
}

// SMTPSender sends through one server.
type SMTPSender struct {
	srv Server
	opt Options
}

// ErrTenantPort means a tenant server uses a port other than 465/587.
var ErrTenantPort = errors.New("mailer: only ports 465 (implicit TLS) and 587 (STARTTLS) are allowed")

// TenantServerOK checks the tenant rules: port 465 with implicit TLS or
// 587 with STARTTLS. Port 25 is relay, not submission, and is refused.
func TenantServerOK(s Server) error {
	switch {
	case s.Port == 465 && s.TLS == TLSImplicit, s.Port == 587 && s.TLS == TLSStartTLS:
		return nil
	default:
		return ErrTenantPort
	}
}

// NewSMTP creates a sender.
func NewSMTP(srv Server, opt Options) *SMTPSender {
	if opt.SendTimeout <= 0 {
		opt.SendTimeout = 30 * time.Second
	}
	if opt.HELO == "" {
		opt.HELO = "localhost"
	}
	return &SMTPSender{srv: srv, opt: opt}
}

func (s *SMTPSender) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: s.srv.Host, MinVersion: tls.VersionTLS12, RootCAs: s.opt.RootCAs}
}

func (s *SMTPSender) client() (*gomail.Client, error) {
	opts := []gomail.Option{
		gomail.WithPort(s.srv.Port),
		gomail.WithTimeout(s.opt.SendTimeout),
		gomail.WithHELO(s.opt.HELO),
		gomail.WithTLSConfig(s.tlsConfig()),
	}
	switch s.srv.TLS {
	case TLSImplicit:
		// The dialer completes the TLS handshake; WithSSL stops go-mail from
		// asking for STARTTLS on top of it.
		opts = append(opts, gomail.WithSSL(), gomail.WithDialContextFunc(s.opt.Guard.Dialer(s.srv.Host, s.srv.Port, s.tlsConfig())))
	case TLSStartTLS:
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory), gomail.WithDialContextFunc(s.opt.Guard.Dialer(s.srv.Host, s.srv.Port, nil)))
	case TLSNone:
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS), gomail.WithDialContextFunc(s.opt.Guard.Dialer(s.srv.Host, s.srv.Port, nil)))
	default:
		return nil, fmt.Errorf("%w: unknown TLS mode", ErrAuth)
	}
	if s.srv.Username != "" {
		// PLAIN only over TLS (go-mail refuses it on a plain connection).
		opts = append(opts, gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(s.srv.Username), gomail.WithPassword(s.srv.Password.Reveal()))
	}
	return gomail.NewClient(s.srv.Host, opts...)
}

func build(m Message) (*gomail.Msg, error) {
	msg := gomail.NewMsg()
	if err := msg.FromFormat(m.From.Name, m.From.Email); err != nil {
		return nil, fmt.Errorf("%w: from", ErrInvalid)
	}
	if m.ReplyTo.Email != "" {
		if err := msg.ReplyToFormat(m.ReplyTo.Name, m.ReplyTo.Email); err != nil {
			return nil, fmt.Errorf("%w: reply-to", ErrInvalid)
		}
	}
	for _, a := range m.To {
		if err := msg.AddToFormat(a.Name, a.Email); err != nil {
			return nil, fmt.Errorf("%w: to", ErrInvalid)
		}
	}
	msg.Subject(m.Subject)
	msg.SetDate()
	if m.MessageID != "" {
		msg.SetMessageIDWithValue(m.MessageID)
	} else {
		msg.SetMessageID()
	}
	for k, v := range m.Headers {
		msg.SetGenHeader(gomail.Header(k), v)
	}
	switch {
	case m.TextBody != "" && m.HTMLBody != "":
		msg.SetBodyString(gomail.TypeTextPlain, m.TextBody)
		msg.AddAlternativeString(gomail.TypeTextHTML, m.HTMLBody)
	case m.HTMLBody != "":
		msg.SetBodyString(gomail.TypeTextHTML, m.HTMLBody)
	default:
		msg.SetBodyString(gomail.TypeTextPlain, m.TextBody)
	}
	for _, a := range m.Attachments {
		if err := msg.AttachReader(a.Name, bytes.NewReader(a.Data), gomail.WithFileContentType(gomail.ContentType(a.ContentType))); err != nil {
			return nil, fmt.Errorf("%w: attachment", ErrInvalid)
		}
	}
	return msg, nil
}

// Send validates, builds and sends m. The returned ID is the Message-ID.
func (s *SMTPSender) Send(ctx context.Context, m Message) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	msg, err := build(m)
	if err != nil {
		return "", err
	}
	c, err := s.client()
	if err != nil {
		return "", &Error{Class: ErrAuth, Code: "client_config", Detail: "invalid SMTP settings"}
	}
	ctx, cancel := context.WithTimeout(ctx, s.opt.SendTimeout)
	defer cancel()
	if err := c.DialAndSendWithContext(ctx, msg); err != nil {
		return "", classify(err)
	}
	return m.MessageID, nil
}

// Error is a classified send failure. Detail is safe to store and show:
// a code and the server's short reply, never credentials.
type Error struct {
	Class  error
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

// Unwrap lets errors.Is match the class.
func (e *Error) Unwrap() error { return e.Class }

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// classify maps go-mail, TLS, DNS and network errors to a class.
func classify(err error) error {
	var (
		tp       *textproto.Error
		se       *gomail.SendError
		dnsErr   *net.DNSError
		certErr  *tls.CertificateVerificationError
		hostErr  x509.HostnameError
		authErr  x509.UnknownAuthorityError
		recErr   tls.RecordHeaderError
		alertErr tls.AlertError
	)
	msg := err.Error()
	switch {
	case errors.Is(err, ErrBlockedAddress):
		return &Error{Class: ErrAuth, Code: "blocked_address", Detail: "the SMTP host resolves to a private or reserved address"}
	case errors.As(err, &certErr), errors.As(err, &hostErr), errors.As(err, &authErr):
		return &Error{Class: ErrAuth, Code: "tls_verify", Detail: "the server's TLS certificate is not valid for this host"}
	case errors.As(err, &recErr), errors.As(err, &alertErr):
		return &Error{Class: ErrAuth, Code: "tls_handshake", Detail: "TLS handshake failed (check the port and TLS mode)"}
	case strings.Contains(msg, "does not support STARTTLS"):
		return &Error{Class: ErrAuth, Code: "starttls_unavailable", Detail: "the server does not offer STARTTLS"}
	case errors.Is(err, gomail.ErrPlainAuthNotSupported), strings.Contains(msg, "does not support SMTP AUTH"):
		return &Error{Class: ErrAuth, Code: "auth_unsupported", Detail: "the server does not offer PLAIN authentication"}
	case errors.As(err, &se):
		enh := se.EnhancedStatusCode()
		if enh == "" {
			enh = enhanced(msg)
		}
		return fromCode(se.ErrorCode(), enh, se.IsTemp(), msg)
	case errors.As(err, &tp):
		return fromCode(tp.Code, enhanced(tp.Msg), tp.Code >= 400 && tp.Code < 500, tp.Msg)
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return &Error{Class: ErrAuth, Code: "dns_not_found", Detail: "the SMTP host name does not resolve"}
		}
		return &Error{Class: ErrTransient, Code: "dns", Detail: "DNS lookup failed"}
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return &Error{Class: ErrTransient, Code: "timeout", Detail: "timed out"}
	default:
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return &Error{Class: ErrTransient, Code: "timeout", Detail: "timed out"}
		}
		return &Error{Class: ErrTransient, Code: "network", Detail: "connection failed"}
	}
}

var enhancedPattern = regexp.MustCompile(`\b([245]\.\d{1,3}\.\d{1,3})\b`)

// enhanced finds an RFC 3463 status code ("5.4.5") in a server reply.
func enhanced(s string) string {
	if m := enhancedPattern.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func fromCode(code int, enh string, temp bool, msg string) error {
	detail := short(msg)
	c := fmt.Sprintf("smtp_%d", code)
	switch {
	case code == 535 || code == 534 || code == 530 || code == 538 || code == 454 && strings.HasPrefix(enh, "4.7."):
		return &Error{Class: ErrAuth, Code: c, Detail: detail}
	case enh == "5.4.5" || enh == "4.4.5" || strings.HasPrefix(enh, "4.7.28") || code == 421 && strings.Contains(strings.ToLower(msg), "limit"):
		return &Error{Class: ErrQuota, Code: c, Detail: detail}
	case temp || (code >= 400 && code < 500):
		return &Error{Class: ErrTransient, Code: c, Detail: detail}
	case code >= 500:
		return &Error{Class: ErrPermanent, Code: c, Detail: detail}
	default:
		return &Error{Class: ErrTransient, Code: "smtp", Detail: detail}
	}
}
