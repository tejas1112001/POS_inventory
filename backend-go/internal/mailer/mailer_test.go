package mailer

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer/smtptest"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

func good() Message {
	return Message{
		From:      Address{Name: "Asha Stores", Email: "shop@example.com"},
		ReplyTo:   Address{Email: "owner@example.com"},
		To:        []Address{{Name: "Ravi", Email: "ravi@example.org"}},
		Subject:   "Your receipt POS-INV-000001",
		TextBody:  "Thanks",
		HTMLBody:  "<p>Thanks</p>",
		MessageID: "sale:1:receipt@mail.example.com",
	}
}

func TestHeaderInjectionIsRefused(t *testing.T) {
	require.NoError(t, good().Validate())
	cases := map[string]func(m *Message){
		"CRLF in subject":      func(m *Message) { m.Subject = "Hi\r\nBcc: victim@example.net" },
		"LF in subject":        func(m *Message) { m.Subject = "Hi\nX: y" },
		"NUL in subject":       func(m *Message) { m.Subject = "Hi\x00" },
		"CRLF in from name":    func(m *Message) { m.From.Name = "Shop\r\nBcc: x@example.net" },
		"CRLF in to name":      func(m *Message) { m.To[0].Name = "Ravi\r\nCc: x@example.net" },
		"CRLF in to address":   func(m *Message) { m.To[0].Email = "ravi@example.org\r\nBcc: x@example.net" },
		"two addresses in one": func(m *Message) { m.To[0].Email = "a@example.org, b@example.org" },
		"display form address": func(m *Message) { m.To[0].Email = "Ravi <ravi@example.org>" },
		"bad reply-to":         func(m *Message) { m.ReplyTo.Email = "x\n@example.org" },
		"unicode line sep":     func(m *Message) { m.Subject = "a b" },
		"C1 control":           func(m *Message) { m.From.Name = "a\u0085b" },
		"header not allowed":   func(m *Message) { m.Headers = map[string]string{"Bcc": "x@example.net"} },
		"CRLF in header value": func(m *Message) { m.Headers = map[string]string{"List-Unsubscribe": "<https://x>\r\nBcc: y"} },
		"too many recipients":  func(m *Message) { m.To = make([]Address, 11) },
		"no recipients":        func(m *Message) { m.To = nil },
		"bad message id":       func(m *Message) { m.MessageID = "a b@c" },
		"attachment path": func(m *Message) {
			m.Attachments = []Attachment{{Name: "../x.pdf", ContentType: "application/pdf", Data: []byte("x")}}
		},
		"attachment type": func(m *Message) {
			m.Attachments = []Attachment{{Name: "x.html", ContentType: "text/html", Data: []byte("x")}}
		},
		"attachment too large": func(m *Message) {
			m.Attachments = []Attachment{{Name: "x.pdf", ContentType: "application/pdf", Data: make([]byte, MaxAttachment+1)}}
		},
		"subject too long":      func(m *Message) { m.Subject = strings.Repeat("a", MaxSubjectRunes+1) },
		"display name too long": func(m *Message) { m.From.Name = strings.Repeat("a", MaxNameRunes+1) },
	}
	for name, mutate := range cases {
		m := good()
		mutate(&m)
		require.ErrorIs(t, m.Validate(), ErrInvalid, name)
	}
}

func TestSanitizeName(t *testing.T) {
	require.Equal(t, "Shop Bcc: x@example.net", SanitizeName("Shop\r\nBcc: x@example.net"))
	require.Equal(t, "क्षितिज Stores", SanitizeName("  क्षितिज\t Stores "))
	require.Len(t, []rune(SanitizeName(strings.Repeat("é", 200))), MaxNameRunes)
	m := good()
	m.From.Name = SanitizeName("Evil\r\nBcc: x@example.net")
	require.NoError(t, m.Validate())
}

func TestBlockedRanges(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.8.9.10", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "100.64.0.1",
		"169.254.169.254", "0.0.0.0", "0.1.2.3", "224.0.0.1", "239.1.1.1", "255.255.255.255", "240.0.0.1",
		"192.0.2.1", "198.18.0.1", "203.0.113.5",
		"::1", "::", "fc00::1", "fd12:3456::1", "fe80::1", "ff02::1", "fec0::1", "2001:db8::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", // IPv4-mapped
		"64:ff9b::a00:1", "2002:a00:1::1", "2001:0:a00:1::1", "::127.0.0.1",
	}
	for _, s := range blocked {
		require.True(t, Blocked(netip.MustParseAddr(s), nil), s)
	}
	for _, s := range []string{"142.250.4.108", "8.8.8.8", "1.1.1.1", "2607:f8b0:4005:80a::200e", "::ffff:142.250.4.108"} {
		require.False(t, Blocked(netip.MustParseAddr(s), nil), s)
	}
	deny := []netip.Prefix{netip.MustParsePrefix("142.250.0.0/16")}
	require.True(t, Blocked(netip.MustParseAddr("142.250.4.108"), deny), "SMTP_DENY_CIDRS")
}

// fakeResolver answers from a map and counts lookups.
type fakeResolver struct {
	answers map[string][][]string // successive answers per host
	calls   atomic.Int32
}

func (f *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	n := int(f.calls.Add(1)) - 1
	a := f.answers[host]
	if len(a) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	if n >= len(a) {
		n = len(a) - 1
	}
	var out []netip.Addr
	for _, s := range a[n] {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

func TestGuardResolve(t *testing.T) {
	r := &fakeResolver{answers: map[string][][]string{
		"smtp.good.test":     {{"142.250.4.108"}},
		"metadata.test":      {{"169.254.169.254"}},
		"mixed.test":         {{"142.250.4.108", "10.0.0.5"}},
		"mapped.test":        {{"::ffff:127.0.0.1"}},
		"localhost.evil.com": {{"127.0.0.1"}},
	}}
	g := Guard{Resolver: r}
	ip, err := g.Resolve(context.Background(), "smtp.good.test")
	require.NoError(t, err)
	require.Equal(t, "142.250.4.108", ip.String())
	for _, h := range []string{"metadata.test", "mixed.test", "mapped.test", "localhost.evil.com", "127.0.0.1", "169.254.169.254", "::1", "10.0.0.1"} {
		_, err := g.Resolve(context.Background(), h)
		require.ErrorIs(t, err, ErrBlockedAddress, h)
	}
	_, err = g.Resolve(context.Background(), "nx.test")
	var dnsErr *net.DNSError
	require.ErrorAs(t, err, &dnsErr)
	ok, err := Guard{Resolver: r, AllowPrivate: true}.Resolve(context.Background(), "localhost.evil.com")
	require.NoError(t, err, "operator relays may be private")
	require.Equal(t, "127.0.0.1", ok.String())

	// The test allow list exempts exact addresses only.
	g = Guard{Resolver: r, Allow: []netip.Addr{netip.MustParseAddr("127.0.0.2")}}
	_, err = g.Resolve(context.Background(), "127.0.0.2")
	require.NoError(t, err)
	_, err = g.Resolve(context.Background(), "127.0.0.1")
	require.ErrorIs(t, err, ErrBlockedAddress)
}

// DNS rebinding: the first answer is public, every later one is internal.
// The dialer resolves once and connects to exactly the checked address.
func TestGuardDialsTheCheckedAddressOnly(t *testing.T) {
	r := &fakeResolver{answers: map[string][][]string{"rebind.test": {{"142.250.4.108"}, {"127.0.0.1"}}}}
	var dialled []string
	g := Guard{Resolver: r, DialFunc: func(_ context.Context, _, addr string) (net.Conn, error) {
		dialled = append(dialled, addr)
		return nil, errors.New("stop")
	}}
	_, err := g.Dialer("rebind.test", 587, nil)(context.Background(), "tcp", "rebind.test:587")
	require.Error(t, err)
	require.Equal(t, []string{"142.250.4.108:587"}, dialled, "dialled the checked IP, not the name")
	require.EqualValues(t, 1, r.calls.Load(), "one lookup per connection")

	// The next connection sees the internal answer and is refused before
	// any dial.
	_, err = g.Dialer("rebind.test", 587, nil)(context.Background(), "tcp", "rebind.test:587")
	require.ErrorIs(t, err, ErrBlockedAddress)
	require.Len(t, dialled, 1)
}

func TestTenantPortPolicy(t *testing.T) {
	require.NoError(t, TenantServerOK(Server{Port: 465, TLS: TLSImplicit}))
	require.NoError(t, TenantServerOK(Server{Port: 587, TLS: TLSStartTLS}))
	for _, s := range []Server{{Port: 25, TLS: TLSStartTLS}, {Port: 2525, TLS: TLSStartTLS}, {Port: 465, TLS: TLSStartTLS},
		{Port: 587, TLS: TLSImplicit}, {Port: 587, TLS: TLSNone}, {Port: 25, TLS: TLSNone}} {
		require.ErrorIs(t, TenantServerOK(s), ErrTenantPort, "%+v", s)
	}
}

const pw = "app-password-very-secret"

func sender(srv *smtptest.Server, mode TLSMode, host string, allowPrivate bool, user string) *SMTPSender {
	r := &fakeResolver{answers: map[string][][]string{host: {{"127.0.0.1"}}}}
	return NewSMTP(Server{Host: host, Port: srv.Port, TLS: mode, Username: user, Password: secrets.Secret(pw)},
		Options{Guard: Guard{Resolver: r, AllowPrivate: allowPrivate}, RootCAs: srv.RootCAs, HELO: "mail.example.com"})
}

func TestSendStartTLSAndImplicit(t *testing.T) {
	st := smtptest.Start(t, smtptest.Options{Mode: smtptest.StartTLS, User: "shop@example.com", Pass: pw})
	m := good()
	m.Subject = "Receipt ₹1,234 for Ravi"
	m.Attachments = []Attachment{{Name: "receipt.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4 test")}}
	id, err := sender(st, TLSStartTLS, "smtp.example.test", true, "shop@example.com").Send(context.Background(), m)
	require.NoError(t, err)
	require.Equal(t, m.MessageID, id)
	got := st.Messages()
	require.Len(t, got, 1)
	require.Equal(t, []string{"ravi@example.org"}, got[0].To)
	data := string(got[0].Data)
	require.Contains(t, data, "Message-ID: <sale:1:receipt@mail.example.com>")
	require.Contains(t, data, "receipt.pdf")
	require.Contains(t, data, "=?UTF-8?", "non-ASCII subject is RFC 2047 encoded")
	require.NotContains(t, data, pw)

	im := smtptest.Start(t, smtptest.Options{Mode: smtptest.Implicit, User: "u", Pass: pw})
	_, err = sender(im, TLSImplicit, "smtp.example.test", true, "u").Send(context.Background(), good())
	require.NoError(t, err)
	require.Len(t, im.Messages(), 1)
}

func requireClass(t *testing.T, err error, class error, code string) {
	t.Helper()
	require.ErrorIs(t, err, class)
	var e *Error
	require.ErrorAs(t, err, &e)
	if code != "" {
		require.Equal(t, code, e.Code)
	}
	require.NotContains(t, err.Error(), pw, "errors never carry the password")
}

func TestSendFailuresAreClassified(t *testing.T) {
	ctx := context.Background()

	// SSRF: a name that resolves to loopback is refused before connecting.
	st := smtptest.Start(t, smtptest.Options{Mode: smtptest.StartTLS, User: "u", Pass: pw})
	_, err := sender(st, TLSStartTLS, "smtp.example.test", false, "u").Send(ctx, good())
	requireClass(t, err, ErrAuth, "blocked_address")
	require.Zero(t, st.Connections())

	// STARTTLS is mandatory.
	nost := smtptest.Start(t, smtptest.Options{Mode: smtptest.StartTLS, NoStartTLS: true, User: "u", Pass: pw})
	_, err = sender(nost, TLSStartTLS, "smtp.example.test", true, "u").Send(ctx, good())
	requireClass(t, err, ErrAuth, "starttls_unavailable")
	require.Empty(t, nost.Messages())

	// The certificate is checked against the configured host name, even
	// though the connection goes to the resolved IP.
	_, err = sender(st, TLSStartTLS, "other.example.test", true, "u").Send(ctx, good())
	requireClass(t, err, ErrAuth, "tls_verify")
	im := smtptest.Start(t, smtptest.Options{Mode: smtptest.Implicit})
	_, err = sender(im, TLSImplicit, "other.example.test", true, "").Send(ctx, good())
	requireClass(t, err, ErrAuth, "tls_verify")

	// Wrong password.
	bad := smtptest.Start(t, smtptest.Options{Mode: smtptest.StartTLS, User: "u", Pass: "other"})
	_, err = sender(bad, TLSStartTLS, "smtp.example.test", true, "u").Send(ctx, good())
	requireClass(t, err, ErrAuth, "smtp_535")

	// Implicit TLS against a STARTTLS port fails the handshake.
	_, err = sender(st, TLSImplicit, "smtp.example.test", true, "u").Send(ctx, good())
	require.ErrorIs(t, err, ErrAuth)

	for _, c := range []struct {
		cmd, reply string
		class      error
	}{
		{"RCPT", "550 5.1.1 no such user", ErrPermanent},
		{"RCPT", "450 4.2.1 mailbox busy", ErrTransient},
		{"DATA", "451 4.3.0 try later", ErrTransient},
		{"DATA", "552 5.3.4 message too big", ErrPermanent},
		{"MAIL", "550 5.4.5 Daily user sending quota exceeded", ErrQuota},
	} {
		st.FailAt(c.cmd, c.reply)
		_, err = sender(st, TLSStartTLS, "smtp.example.test", true, "u").Send(ctx, good())
		requireClass(t, err, c.class, "")
		st.FailAt(c.cmd, "")
	}

	// Nothing listens: transient.
	r := &fakeResolver{answers: map[string][][]string{"smtp.example.test": {{"127.0.0.1"}}}}
	s := NewSMTP(Server{Host: "smtp.example.test", Port: 1, TLS: TLSStartTLS}, Options{Guard: Guard{Resolver: r, AllowPrivate: true}})
	_, err = s.Send(ctx, good())
	require.ErrorIs(t, err, ErrTransient)

	// Invalid messages never reach the network.
	m := good()
	m.Subject = "x\r\nBcc: y@example.net"
	before := st.Connections()
	_, err = sender(st, TLSStartTLS, "smtp.example.test", true, "u").Send(ctx, m)
	require.ErrorIs(t, err, ErrInvalid)
	require.Equal(t, before, st.Connections())
	require.Equal(t, "permanent", Class(err))
}

func TestPlainRelayForDev(t *testing.T) {
	pl := smtptest.Start(t, smtptest.Options{Mode: smtptest.Plain})
	_, err := sender(pl, TLSNone, "mailpit", true, "").Send(context.Background(), good())
	require.NoError(t, err)
	require.Len(t, pl.Messages(), 1)
}
