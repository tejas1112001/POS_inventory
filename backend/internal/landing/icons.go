package landing

import "html/template"

// iconPaths are 24×24 stroke icons (path data only, constants).
var iconPaths = map[string]string{
	"store": `<path d="M3 9l1.5-5h15L21 9"/><path d="M3 9h18v2a3 3 0 0 1-6 0 3 3 0 0 1-6 0 3 3 0 0 1-6 0V9z"/><path d="M5 13v7h14v-7"/>`,
}

// icon returns an inline decorative SVG. Unknown names give nothing.
func icon(name string) template.HTML {
	p, ok := iconPaths[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="icon" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true" focusable="false">` + p + `</svg>`) //nolint:gosec // G203: constant markup
}
