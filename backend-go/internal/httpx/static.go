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
	fsys fs.FS
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
	} else if strings.HasPrefix(name, "assets/") {
		// Vite puts content-hashed files under assets/.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	// name is cleaned above and s.fsys is an os.Root, which refuses ".." and
	// symlinks that leave the directory.
	http.ServeFileFS(w, r, s.fsys, name) //nolint:gosec // G703: confined by os.Root
}
