package shop

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestFinalPrice(t *testing.T) {
	d := decimal.RequireFromString
	pct, fixed, other := "Percentage", "Fixed", "Other"
	got, ok := finalPrice(d("120.5"), &pct, d("10"), "INR")
	require.True(t, ok)
	require.Equal(t, "108.45", got.StringFixed(2))
	got, ok = finalPrice(d("100"), &fixed, d("150"), "INR")
	require.True(t, ok)
	require.True(t, got.IsZero(), "never below zero")
	got, ok = finalPrice(d("100"), &pct, d("0"), "INR")
	require.False(t, ok)
	require.Equal(t, "100", got.String())
	_, ok = finalPrice(d("100"), nil, d("5"), "INR")
	require.False(t, ok)
	_, ok = finalPrice(d("100"), &other, d("5"), "INR")
	require.False(t, ok)
	got, _ = finalPrice(d("999"), &pct, d("33.3"), "JPY")
	require.Equal(t, "666", got.String(), "rounded to the currency")
}

func TestWriteCached(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	writeCached(w, r, map[string]any{"name": "Shop"})
	require.Equal(t, 200, w.Code)
	require.Equal(t, cacheControl, w.Header().Get("Cache-Control"))
	etag := w.Header().Get("ETag")
	require.Regexp(t, `^"[0-9a-f]{24}"$`, etag)
	require.JSONEq(t, `{"name":"Shop"}`, w.Body.String())

	r.Header.Set("If-None-Match", `"other", `+etag)
	w = httptest.NewRecorder()
	writeCached(w, r, map[string]any{"name": "Shop"})
	require.Equal(t, http.StatusNotModified, w.Code)
	require.Empty(t, w.Body.String())

	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	writeCached(w, r, map[string]any{"name": "Changed"})
	require.Equal(t, 200, w.Code, "a changed body gets a new ETag")
}
