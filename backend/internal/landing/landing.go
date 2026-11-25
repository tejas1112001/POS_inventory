// Package landing serves the public website on the main address
// (<BASE_DOMAIN>, TENANCY §7.1). The home, features, industries, pricing
// and contact pages are the React marketing site (landing/ build, Deps.SPA).
// The rest is server-rendered HTML and one CSS file, no JavaScript, under
// the strict CSP: staff sign-in, "Find my shop" (the same reply whatever
// the email), contact form replies, terms and privacy. There is no sign-up.
package landing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"

	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/site.css static/favicon.svg
var staticFS embed.FS

// Limits are the form rate limits. Zero fields take the defaults.
type Limits struct {
	Window       time.Duration // default 1 hour
	FindPerIP    int           // "Find my shop" requests per IP (default 5)
	FindPerEmail int           // emails to one address; over it the request is dropped quietly (default 3)
	ContactPerIP int           // contact requests per IP (default 5)
	ContactTotal int           // contact requests from everyone (default 100)
	LoginPerIP   int           // shop address look-ups per IP (default 60)
}

func (l Limits) withDefaults() Limits {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	if l.Window <= 0 {
		l.Window = time.Hour
	}
	def(&l.FindPerIP, 5)
	def(&l.FindPerEmail, 3)
	def(&l.ContactPerIP, 5)
	def(&l.ContactTotal, 100)
	def(&l.LoginPerIP, 60)
	return l
}

// Deps are the inputs of the site.
type Deps struct {
	Sessions     *tenancy.Sessions
	Network      httpx.Network
	PublicScheme string // https in production
	PublicPort   string // dev proxy port (empty in production)
	BaseDomain   string
	PlatformName string
	// ContactEmail receives contact and demo requests. Empty: requests are
	// stored only.
	ContactEmail string
	// FormKey signs the form time stamps (see FormKey).
	FormKey []byte
	// MailQueued runs after a platform email row commits, so it goes out
	// before the next sweep. Optional.
	MailQueued func(context.Context)
	Metrics    *obs.Metrics
	Log        *slog.Logger
	Limits     Limits
	// SPA is the React marketing site (landing/ build, docs/LANDING_DEPLOYMENT.md).
	// When set it serves the pages in spaPages and /assets/*, and the Go
	// templates keep the other pages (terms, privacy, login, find my shop,
	// form replies). Nil (no build deployed): the home page says the
	// website is not available, and the Go contact page is served.
	SPA *httpx.SPA
	Now func() time.Time // tests
}

// FormKey derives the form-signing key from the master key, so every
// instance accepts every other instance's forms.
func FormKey(master []byte) []byte {
	m := hmac.New(sha256.New, master)
	m.Write([]byte("landing form token v1"))
	return m.Sum(nil)
}

// Site is the landing website.
type Site struct {
	d       Deps
	origin  string // canonical origin, e.g. https://example.com
	pages   map[string]*template.Template
	cssPath string
	css     []byte
	favicon []byte
	store   store

	findIP, findEmail, contactIP, contactAll, loginIP *httprate.RateLimiter
}

// spaPages are the routes of the React marketing site (landing/src/App.jsx).
// Any other path the Go site does not own is a real 404.
var spaPages = []string{"/", "/features", "/industries", "/pricing", "/contact"}

var pageNames = []string{"find", "find_sent", "contact", "contact_sent", "login", "terms", "privacy", "error"}

// New builds the site.
func New(d Deps) (*Site, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if len(d.FormKey) < 32 {
		return nil, fmt.Errorf("landing: form key too short")
	}
	d.Limits = d.Limits.withDefaults()
	s := &Site{d: d, origin: httpx.OriginURL(d.PublicScheme, d.BaseDomain, d.PublicPort), pages: map[string]*template.Template{}}

	css, err := staticFS.ReadFile("static/site.css")
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(css)
	s.css, s.cssPath = css, "/assets/site-"+hex.EncodeToString(sum[:])[:12]+".css"
	if s.favicon, err = staticFS.ReadFile("static/favicon.svg"); err != nil {
		return nil, err
	}

	funcs := template.FuncMap{"icon": icon}
	for _, name := range pageNames {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("landing: template %s: %w", name, err)
		}
		s.pages[name] = t
	}

	if d.Sessions != nil {
		s.store = dbStore{d.Sessions}
	}

	quiet := httprate.WithResponseHeaders(httprate.ResponseHeaders{})
	l := d.Limits
	s.findIP = httprate.NewRateLimiter(l.FindPerIP, l.Window, quiet)
	s.findEmail = httprate.NewRateLimiter(l.FindPerEmail, l.Window, quiet)
	s.contactIP = httprate.NewRateLimiter(l.ContactPerIP, l.Window, quiet)
	s.contactAll = httprate.NewRateLimiter(l.ContactTotal, l.Window, quiet)
	s.loginIP = httprate.NewRateLimiter(l.LoginPerIP, time.Minute, quiet)
	return s, nil
}

// Mount registers the site's routes on the apex host router.
func (s *Site) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(middleware.Compress(5, "text/html", "text/css", "text/plain", "application/xml", "image/svg+xml",
			"text/javascript", "application/javascript", "application/json"))
		get := func(p string, h http.HandlerFunc) {
			r.Get(p, h)
			r.Head(p, h)
		}
		if s.d.SPA != nil {
			for _, p := range spaPages {
				get(p, s.d.SPA.ServeHTTP)
			}
			get("/assets/*", s.d.SPA.ServeHTTP) // s.cssPath below is more specific and wins
		} else {
			get("/", s.unavailable)
			get("/contact", s.contactPage)
		}
		get("/terms", s.simple("terms", "Terms of Service", "The terms that apply when you use "+s.d.PlatformName+"."))
		get("/privacy", s.simple("privacy", "Privacy Policy", "How "+s.d.PlatformName+" collects, uses and protects personal data."))
		get("/login", s.login)
		get("/login/go", s.loginGo)
		get("/find-my-shop", s.findPage)
		r.Post("/find-my-shop", s.findSubmit)
		get("/find-my-shop/sent", s.findSent)
		get("/contact/token", s.contactToken)
		r.Post("/contact", s.contactSubmit)
		get("/contact/sent", s.contactSent)
		get("/robots.txt", s.robots)
		get("/sitemap.xml", s.sitemap)
		get("/favicon.svg", s.serveFavicon)
		get(s.cssPath, s.serveCSS)
	})
}

// NotFound is the HTML 404 page of the main address.
func (s *Site) NotFound(w http.ResponseWriter, r *http.Request) {
	v := s.view(r, "Page not found", "This page does not exist.", "")
	v.Noindex = true
	v.Heading = "Page not found"
	v.Message = "The page you asked for does not exist. Check the address, or go to the home page."
	s.render(w, r, http.StatusNotFound, "error", v)
}

// ------------------------------------------------------------------ views

type siteInfo struct {
	Name, Domain, Origin, CSS string
}

// view is the data of every page.
type view struct {
	Site                     siteInfo
	Title, Description, Path string
	Canonical                string
	Noindex                  bool
	JSONLD                   template.JS
	Find, Contact            form
	Heading, Message         string
	Link, LinkText           string // error page button; default: the home page
	Shop                     string // login: what was typed
	ShopError                string
	Year                     int
}

// form is the state of one form on a page.
type form struct {
	Token  string
	Values map[string]string
	Errors map[string]string
	Alert  string
}

func (f form) Value(k string) string { return f.Values[k] }
func (f form) Error(k string) string { return f.Errors[k] }

func (s *Site) view(r *http.Request, title, desc, path string) *view {
	v := &view{
		Site:  siteInfo{Name: s.d.PlatformName, Domain: s.d.BaseDomain, Origin: s.origin, CSS: s.cssPath},
		Title: title, Description: desc, Path: path,
		Find:    form{Token: s.token(formFind)},
		Contact: form{Token: s.token(formContact), Values: map[string]string{"topic": "demo"}},
		Year:    s.d.Now().Year(),
	}
	if path != "" {
		v.Canonical = s.origin + path
	}
	return v
}

// render writes a page. The page is rendered into a buffer first, so a
// template error is a clean 500.
func (s *Site) render(w http.ResponseWriter, r *http.Request, status int, name string, v *view) {
	var buf bytes.Buffer
	if err := s.pages[name].ExecuteTemplate(&buf, "layout.html", v); err != nil {
		httpx.LoggerFrom(r.Context()).ErrorContext(r.Context(), "landing page failed", slog.String("page", name), slog.Any("err", err))
		http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	switch {
	case v.Noindex || status != http.StatusOK:
		h.Set("Cache-Control", "no-store")
		h.Set("X-Robots-Tag", "noindex")
	default:
		h.Set("Cache-Control", "public, max-age=300")
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// unavailable is the home page while no React build is deployed (Deps.SPA
// nil): a plain notice, never an old copy of the site. Staff sign-in and
// the other Go pages keep working.
func (s *Site) unavailable(w http.ResponseWriter, r *http.Request) {
	v := s.view(r, s.d.PlatformName, "The "+s.d.PlatformName+" website is not available right now.", "")
	v.Noindex = true
	v.Heading = "Website not available"
	v.Message = "The " + s.d.PlatformName + " website is not available right now. Staff can still sign in to their shop."
	v.Link, v.LinkText = "/login", "Staff login"
	s.render(w, r, http.StatusServiceUnavailable, "error", v)
}

func (s *Site) simple(name, title, desc string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := s.view(r, title+" | "+s.d.PlatformName, desc, "/"+name)
		v.Heading = title
		s.render(w, r, http.StatusOK, name, v)
	}
}

func (s *Site) serveCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(s.css)
}

func (s *Site) serveFavicon(w http.ResponseWriter, r *http.Request) {
	if s.d.SPA != nil && s.d.SPA.Has("/favicon.svg") {
		s.d.SPA.ServeHTTP(w, r) // the React site's own icon
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(s.favicon)
}

// clientKey is the rate-limit key of the caller: the IP, or its /64 for
// IPv6.
func (s *Site) clientKey(r *http.Request) string {
	return httprate.CanonicalizeIP(s.d.Network.ClientIP(r).String())
}

func (s *Site) limited(name string) {
	if s.d.Metrics != nil {
		s.d.Metrics.RateLimited.WithLabelValues(name).Inc()
	}
}

// discard is a ResponseWriter for limiters whose result is never shown.
type discard struct{ h http.Header }

func (d *discard) Header() http.Header         { return d.h }
func (d *discard) Write(b []byte) (int, error) { return len(b), nil }
func (d *discard) WriteHeader(int)             {}

func over(l *httprate.RateLimiter, r *http.Request, key string) bool {
	return l.OnLimit(&discard{h: http.Header{}}, r, key)
}
