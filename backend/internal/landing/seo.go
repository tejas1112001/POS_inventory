package landing

import (
	"html/template"
	"net/http"
	"strings"
)

// sitemapPaths are the indexable pages.
var sitemapPaths = []string{"/", "/find-my-shop", "/contact", "/terms", "/privacy"}

// spaSitemapPaths are the indexable pages while the React site is served.
var spaSitemapPaths = []string{"/", "/features", "/industries", "/pricing", "/contact", "/find-my-shop", "/terms", "/privacy"}

func (s *Site) sitemap(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	paths := sitemapPaths
	if s.d.SPA != nil {
		paths = spaSitemapPaths
	}
	for _, p := range paths {
		b.WriteString("  <url><loc>")
		template.HTMLEscape(&b, []byte(s.origin+p))
		b.WriteString("</loc></url>\n")
	}
	b.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(b.String()))
}

// robots allows everything: pages that must stay out of search results
// (sign-in, form replies) say noindex themselves, which crawlers can only
// see if they may fetch them.
func (s *Site) robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte("User-agent: *\nAllow: /\n\nSitemap: " + s.origin + "/sitemap.xml\n"))
}
