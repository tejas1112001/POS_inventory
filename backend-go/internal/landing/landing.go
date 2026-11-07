// Package landing serves the public website on the main address
// (<BASE_DOMAIN>, TENANCY §7.1): server-rendered HTML and one CSS file, no
// JavaScript, under the strict CSP. It shows the public plans from
// platform.plans, answers "Find my shop" with the same reply whatever the
// email, and stores contact and demo requests. There is no sign-up.
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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/obs"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
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
	Now        func() time.Time // tests
	PlansTTL   time.Duration    // default 1 minute
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
	plans   *planCache
	store   store
	content content

	findIP, findEmail, contactIP, contactAll, loginIP *httprate.RateLimiter
}

// content is the page copy with {{domain}} filled in.
type content struct {
	Features, More []feature
	Steps          []step
	FAQs           []faq
}

var pageNames = []string{"home", "find", "find_sent", "contact", "contact_sent", "login", "terms", "privacy", "error"}

// New builds the site.
func New(d Deps) (*Site, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.PlansTTL <= 0 {
		d.PlansTTL = time.Minute
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

	fill := func(v string) string { return strings.ReplaceAll(v, "{{domain}}", d.BaseDomain) }
	for _, f := range mainFeatures {
		s.content.Features = append(s.content.Features, feature{f.Icon, fill(f.Title), fill(f.Text)})
	}
	for _, f := range moreFeatures {
		s.content.More = append(s.content.More, feature{f.Icon, fill(f.Title), fill(f.Text)})
	}
	s.content.Steps = steps
	for _, f := range faqs {
		s.content.FAQs = append(s.content.FAQs, faq{fill(f.Q), fill(f.A)})
	}

	s.plans = &planCache{ttl: d.PlansTTL, now: d.Now}
	if d.Sessions != nil {
		s.plans.load = loadPlans(d.Sessions)
		s.store = dbStore{d.Sessions}
	} else {
		s.plans.load = func(context.Context) ([]Plan, error) { return nil, nil }
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
		r.Use(middleware.Compress(5, "text/html", "text/css", "text/plain", "application/xml", "image/svg+xml"))
		get := func(p string, h http.HandlerFunc) {
			r.Get(p, h)
			r.Head(p, h)
		}
		get("/", s.home)
		get("/terms", s.simple("terms", "Terms of Service", "The terms that apply when you use "+s.d.PlatformName+"."))
		get("/privacy", s.simple("privacy", "Privacy Policy", "How "+s.d.PlatformName+" collects, uses and protects personal data."))
		get("/login", s.login)
		get("/login/go", s.loginGo)
		get("/find-my-shop", s.findPage)
		r.Post("/find-my-shop", s.findSubmit)
		get("/find-my-shop/sent", s.findSent)
		get("/contact", s.contactPage)
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
	content
	Plans            []Plan
	Find, Contact    form
	Heading, Message string
	Shop             string // login: what was typed
	ShopError        string
	Year             int
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
		Title: title, Description: desc, Path: path, content: s.content,
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

func (s *Site) home(w http.ResponseWriter, r *http.Request) {
	desc := s.d.PlatformName + " is a point of sale and stock system for shops with one or more stores: checkout, stock, receipts by email, returns, purchases, reports and an online shop per store."
	v := s.view(r, s.d.PlatformName+": POS, stock and online shop for retail stores", desc, "/")
	v.Plans = s.plans.get(r.Context())
	v.JSONLD = s.homeJSONLD(v)
	s.render(w, r, http.StatusOK, "home", v)
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
