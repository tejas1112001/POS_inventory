package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"time"
)

// ErrBlockedAddress means the SMTP host resolved to an address the guard
// refuses (EMAIL_AND_RECEIPTS §3.1).
var ErrBlockedAddress = errors.New("mailer: smtp host address is not allowed")

// blockedPrefixes are never dialled for tenant SMTP: loopback, private,
// CGNAT, link-local (incl. cloud metadata), multicast, unspecified,
// documentation and benchmark ranges, reserved space, IPv6 ULA and
// link/site-local, and IPv6 prefixes that embed IPv4 (NAT64, 6to4, Teredo,
// IPv4-compatible).
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/96", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32",
		"2002::/16", "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// Blocked reports whether ip must not be dialled. IPv4-mapped IPv6
// addresses are checked as IPv4.
func Blocked(ip netip.Addr, deny []netip.Prefix) bool {
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	for _, p := range deny {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Resolver is satisfied by *net.Resolver.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Guard resolves an SMTP host once, checks every address, and dials the
// checked address, so DNS rebinding between check and connect is not
// possible. TLS still verifies the certificate against the original host.
type Guard struct {
	Resolver     Resolver       // nil: net.DefaultResolver
	Deny         []netip.Prefix // extra ranges (SMTP_DENY_CIDRS)
	AllowPrivate bool           // operator-configured relays (platform SMTP, Mailpit) and tests
	DialTimeout  time.Duration  // default 10 s
	TLSTimeout   time.Duration  // default 10 s

	// dial is replaced in tests to observe the dialled address.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Resolve returns the address to dial. A literal IP is checked as is. If
// any resolved address is blocked the host is refused, so a name cannot mix
// a public and an internal answer.
func (g Guard) Resolve(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if !g.AllowPrivate && Blocked(ip, g.Deny) {
			return netip.Addr{}, ErrBlockedAddress
		}
		return ip.Unmap(), nil
	}
	r := g.Resolver
	if r == nil {
		r = net.DefaultResolver
	}
	ips, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, err
	}
	if len(ips) == 0 {
		return netip.Addr{}, &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true}
	}
	if !g.AllowPrivate {
		for _, ip := range ips {
			if Blocked(ip, g.Deny) {
				return netip.Addr{}, ErrBlockedAddress
			}
		}
	}
	return ips[0].Unmap().WithZone(""), nil
}

// Dialer returns a dial function for go-mail. With tlsCfg it completes an
// implicit-TLS handshake (port 465); without it the connection is plain
// and go-mail upgrades it with mandatory STARTTLS (port 587). The address
// go-mail passes in is ignored: only the checked address is dialled.
func (g Guard) Dialer(host string, port int, tlsCfg *tls.Config) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		ip, err := g.Resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		dt := g.DialTimeout
		if dt <= 0 {
			dt = 10 * time.Second
		}
		dial := g.dial
		if dial == nil {
			d := net.Dialer{Timeout: dt}
			dial = d.DialContext
		}
		dctx, cancel := context.WithTimeout(ctx, dt)
		defer cancel()
		conn, err := dial(dctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		if err != nil || tlsCfg == nil {
			return conn, err
		}
		tt := g.TLSTimeout
		if tt <= 0 {
			tt = 10 * time.Second
		}
		hctx, hcancel := context.WithTimeout(ctx, tt)
		defer hcancel()
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(hctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return tc, nil
	}
}
