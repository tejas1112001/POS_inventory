package httpx

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
)

// SPA serves a single-page-app build from a directory. Unknown paths without
// a file extension fall back to index.html so client-side routes work.
// Files are opened through os.Root, so ".." and symlinks cannot escape.
type SPA struct {
	fsys    fs.FS
	noindex func(urlPath string) bool
}

// WithNoindex makes page responses for paths where fn is true carry
// "X-Robots-Tag: noindex", so search engines do not list them.
func (s *SPA) WithNoindex(fn func(urlPath string) bool) *SPA {
	s.noindex = fn
	return s
}

// TenantStaffPage reports whether a tenant-app path is a staff page (sign-in,
// back office) rather than the public shop: "/", "/product/..." and
// "/shop/...".
func TenantStaffPage(urlPath string) bool {
	p := path.Clean("/" + urlPath)
	return p != "/" && !strings.HasPrefix(p, "/product/") && !strings.HasPrefix(p, "/shop/")
}

// NewSPA opens dir. It returns (nil, nil) when dir is empty: no build to serve.
func NewSPA(dir string) (*SPA, error) {
	if dir == "" {
		return nil, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(root.FS(), "index.html"); err != nil {
		return nil, errors.New("static: index.html missing in " + dir)
	}
	return &SPA{fsys: root.FS()}, nil
}

func (s *SPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		WriteError(w, r, apperr.NotFound())
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	st, err := fs.Stat(s.fsys, name)
	if err != nil || st.IsDir() {
		if path.Ext(name) != "" {
			WriteError(w, r, apperr.NotFound())
			return
		}
		name = "index.html"
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
		if s.noindex != nil && s.noindex(r.URL.Path) {
			w.Header().Set("X-Robots-Tag", "noindex")
		}
	} else if strings.HasPrefix(name, "assets/") {
		// Vite puts content-hashed files under assets/.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	// name is cleaned above and s.fsys is an os.Root, which refuses ".." and
	// symlinks that leave the directory.
	http.ServeFileFS(w, r, s.fsys, name) //nolint:gosec // G703: confined by os.Root
}
