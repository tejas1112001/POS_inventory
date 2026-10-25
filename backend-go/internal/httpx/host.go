package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

// HostKind says which router serves a request (TENANCY §7.1).
type HostKind int

// Host kinds.
const (
	HostUnknown  HostKind = iota // anything else → 421
	HostApex                     // <BASE_DOMAIN>: landing page only
	HostPlatform                 // admin.<BASE_DOMAIN>: admin console + /api/platform/*
	HostTenant                   // <slug>.<BASE_DOMAIN>: tenant SPA + /api/*
)

func (k HostKind) String() string {
	switch k {
	case HostApex:
		return "apex"
	case HostPlatform:
		return "platform"
	case HostTenant:
		return "tenant"
	default:
		return "unknown"
	}
}

// PlatformLabel is the subdomain of the Super Admin console.
const PlatformLabel = "admin"

// slugPattern is the tenant slug rule from platform.tenants (TENANCY §3):
// 3–30 chars, lowercase letters, digits and single hyphens, no leading or
// trailing hyphen.
var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,28}[a-z0-9])$`)

// ValidSlug reports whether s is a well-formed tenant slug. It does not
// check the reserved list or whether the tenant exists (step 2).
func ValidSlug(s string) bool {
	return slugPattern.MatchString(s) && !strings.Contains(s, "--")
}

// Host is the classified request host.
type Host struct {
	Kind HostKind
	Name string // lowercased host without port
	Slug string // tenant slug when Kind == HostTenant
}

// Network decides which client-supplied forwarding headers to trust.
type Network struct {
	BaseDomain     string
	TrustedProxies []netip.Prefix
}

// trusted reports whether the direct peer is a trusted proxy.
func (n Network) trusted(r *http.Request) bool {
	if len(n.TrustedProxies) == 0 {
		return false
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	addr := ap.Addr().Unmap()
	for _, p := range n.TrustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP returns the caller's address. X-Forwarded-For is used only when
// the direct peer is a trusted proxy, and then the right-most untrusted entry
// wins (entries further left can be forged by the client).
func (n Network) ClientIP(r *http.Request) netip.Addr {
	peer := netip.Addr{}
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		peer = ap.Addr().Unmap()
	}
	if !n.trusted(r) {
		return peer
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	leftmost := peer
	for i := len(parts) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			break
		}
		a = a.Unmap()
		leftmost = a
		isProxy := false
		for _, p := range n.TrustedProxies {
			if p.Contains(a) {
				isProxy = true
				break
			}
		}
		if !isProxy {
			return a
		}
	}
	// Every hop is trusted (e.g. a client on the proxy's own network).
	return leftmost
}

// Scheme returns "https" or "http". X-Forwarded-Proto counts only from a
// trusted proxy.
func (n Network) Scheme(r *http.Request) string {
	if n.trusted(r) {
		if p := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); p == "https" || p == "http" {
			return p
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// RawHost returns the host the client asked for. X-Forwarded-Host counts only
// from a trusted proxy; otherwise a forged header is ignored.
func (n Network) RawHost(r *http.Request) string {
	if n.trusted(r) {
		if h := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); h != "" {
			// Only the first value; proxies append.
			h, _, _ = strings.Cut(h, ",")
			return strings.TrimSpace(h)
		}
	}
	return r.Host
}

// Origin is this request's own origin as a browser writes it in the Origin
// header: scheme, host name and the port when it is not the scheme's
// default (a dev proxy on :8443 forwards Host with its port).
func (n Network) Origin(r *http.Request) string {
	raw := n.RawHost(r)
	name := normalizeHost(raw)
	if name == "" {
		return ""
	}
	return canonicalOrigin(n.Scheme(r), name, portOf(raw))
}

// SameOrigin reports whether the request's Origin header is exactly this
// request's origin (scheme, host and port). CSRF check for the cookie
// endpoints (TENANCY §7.4).
func (n Network) SameOrigin(r *http.Request) bool {
	want := n.Origin(r)
	u, err := url.Parse(strings.TrimSpace(r.Header.Get("Origin")))
	if want == "" || err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return false
	}
	name := normalizeHost(u.Host)
	if name == "" {
		return false
	}
	return canonicalOrigin(strings.ToLower(u.Scheme), name, u.Port()) == want
}

// TenantURL is a tenant's public base URL for links in emails. port is
// empty in production (443); a dev proxy sets PUBLIC_PORT (e.g. 8443).
func TenantURL(scheme, slug, baseDomain, port string) string {
	return canonicalOrigin(scheme, slug+"."+baseDomain, port)
}

func portOf(hostport string) string {
	if _, p, err := net.SplitHostPort(hostport); err == nil {
		return p
	}
	return ""
}

func canonicalOrigin(scheme, name, port string) string {
	if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return scheme + "://" + name
	}
	return scheme + "://" + name + ":" + port
}

// Classify maps the request host to a router.
func (n Network) Classify(r *http.Request) Host {
	name := normalizeHost(n.RawHost(r))
	h := Host{Name: name}
	switch {
	case name == "":
		return h
	case name == n.BaseDomain:
		h.Kind = HostApex
	case name == PlatformLabel+"."+n.BaseDomain:
		h.Kind = HostPlatform
	case strings.HasSuffix(name, "."+n.BaseDomain):
		label := strings.TrimSuffix(name, "."+n.BaseDomain)
		if ValidSlug(label) {
			h.Kind = HostTenant
			h.Slug = label
		}
	}
	return h
}

// normalizeHost lowercases, strips the port and a trailing dot, and rejects
// anything that is not a plain ASCII host name (no IPs, no unicode).
func normalizeHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 255 {
		return ""
	}
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for i := 0; i < len(host); i++ {
		c := host[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
			return ""
		}
	}
	return host
}
