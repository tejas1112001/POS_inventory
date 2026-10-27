package landing

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

// homeJSONLD is the structured data of the home page: the organization,
// the software with one offer per public plan (none while there are no
// public plans), and the FAQ. json.Marshal escapes <, > and &, so the
// text cannot end the script element.
func (s *Site) homeJSONLD(v *view) template.JS {
	org := map[string]any{"@type": "Organization", "@id": s.origin + "/#organization", "name": s.d.PlatformName, "url": s.origin + "/"}
	app := map[string]any{
		"@type": "SoftwareApplication", "name": s.d.PlatformName, "url": s.origin + "/",
		"applicationCategory": "BusinessApplication", "operatingSystem": "Web browser",
		"description": v.Description, "publisher": map[string]string{"@id": s.origin + "/#organization"},
	}
	if len(v.Plans) > 0 {
		offers := make([]map[string]any, 0, len(v.Plans))
		for _, p := range v.Plans {
			offers = append(offers, map[string]any{
				"@type": "Offer", "name": p.Name, "price": p.Decimal(), "priceCurrency": currencyOrINR(p.Currency),
				"url": s.origin + "/#pricing", "category": "subscription",
			})
		}
		app["offers"] = offers
	}
	qa := make([]map[string]any, 0, len(v.FAQs))
	for _, f := range v.FAQs {
		qa = append(qa, map[string]any{"@type": "Question", "name": f.Q,
			"acceptedAnswer": map[string]string{"@type": "Answer", "text": f.A}})
	}
	doc := map[string]any{"@context": "https://schema.org", "@graph": []any{org, app,
		map[string]any{"@type": "FAQPage", "mainEntity": qa}}}
	b, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	return template.JS(b) //nolint:gosec // G203: JSON from json.Marshal, which escapes <, > and &
}

func currencyOrINR(c string) string {
	if c == "" {
		return "INR"
	}
	return c
}

// sitemapPaths are the indexable pages.
var sitemapPaths = []string{"/", "/find-my-shop", "/contact", "/terms", "/privacy"}

func (s *Site) sitemap(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, p := range sitemapPaths {
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
