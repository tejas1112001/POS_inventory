package httpx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSPAServesPrebuiltPages(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	write("index.html", "<p>home</p>")
	write("features/index.html", "<p>features page</p>")
	write("assets/app-abc.js", "console.log(1)")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "empty"), 0o755))

	spa, err := NewSPA(dir)
	require.NoError(t, err)
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}

	for p, want := range map[string]string{
		"/":         "home",
		"/features": "features page", // its own prebuilt file
		"/pricing":  "home",          // no file: the SPA shell
		"/empty":    "home",          // a folder without index.html
		"/a/b/c":    "home",
	} {
		rec := get(p)
		require.Equal(t, http.StatusOK, rec.Code, p)
		require.Contains(t, rec.Body.String(), want, p)
		require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"), p)
		require.Contains(t, rec.Header().Get("Content-Type"), "text/html", p)
	}

	rec := get("/assets/app-abc.js")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Cache-Control"), "immutable")
	require.Equal(t, http.StatusNotFound, get("/assets/missing.js").Code)

	// The noindex rule sees the request path, also for prebuilt pages.
	spa.WithNoindex(func(p string) bool { return p == "/features" })
	require.Equal(t, "noindex", get("/features").Header().Get("X-Robots-Tag"))
	require.Empty(t, get("/").Header().Get("X-Robots-Tag"))
}
