package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// FuzzClassify: whatever the Host and X-Forwarded-Host headers hold, only a
// plain ASCII host name comes out, and a tenant slug is always a valid
// slug (TENANCY §4: request text never reaches SQL unchecked).
func FuzzClassify(f *testing.F) {
	for _, s := range []string{"acme.pos.test", `t_abc"; DROP SCHEMA x.pos.test`, "public.pos.test", "pg_catalog.pos.test",
		"аcme.pos.test", "acme.pos.test:443", "ACME.POS.TEST.", "admin.pos.test", "pos.test", "a.b.pos.test", "[::1]:80", "%61cme.pos.test"} {
		f.Add(s, "", false)
		f.Add("acme.pos.test", s, true)
	}
	f.Fuzz(func(t *testing.T, host, fwd string, trusted bool) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = host
		r.RemoteAddr = "203.0.113.9:1"
		if trusted {
			r.RemoteAddr = "10.1.2.3:1"
		}
		if fwd != "" {
			r.Header.Set("X-Forwarded-Host", fwd)
		}
		h := testNet.Classify(r)
		for _, c := range h.Name {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
				t.Fatalf("name %q from host %q / %q", h.Name, host, fwd)
			}
		}
		switch h.Kind {
		case HostTenant:
			if !ValidSlug(h.Slug) || h.Name != h.Slug+"."+testNet.BaseDomain {
				t.Fatalf("tenant %+v from host %q / %q", h, host, fwd)
			}
		case HostPlatform:
			if h.Name != PlatformLabel+"."+testNet.BaseDomain || h.Slug != "" {
				t.Fatalf("platform %+v", h)
			}
		case HostApex:
			if h.Name != testNet.BaseDomain || h.Slug != "" {
				t.Fatalf("apex %+v", h)
			}
		default:
			if h.Slug != "" {
				t.Fatalf("unknown host with slug %+v", h)
			}
		}
		if strings.ContainsAny(h.Slug, `"';\ `) {
			t.Fatalf("slug %q", h.Slug)
		}
	})
}
