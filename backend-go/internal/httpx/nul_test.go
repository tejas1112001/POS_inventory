package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
)

func TestRejectNUL(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	h := RejectNUL(ok)
	for target, want := range map[string]int{
		"/api/users?q=abc":         204,
		"/api/users?q=a%00b":       400,
		"/api/users?q=a%2500b":     204, // the text "%00", not a NUL
		"/api/users/1%00":          400,
		"/api/users?x=1&q=%00":     400,
		"/api/users?Q=%00":         400,
		"/api/files/abc%00def/raw": 400,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
		require.Equal(t, want, rec.Code, target)
		if want == 400 {
			require.Equal(t, apperr.CodeValidationFailed, decodeErr(t, rec).Code, target)
		}
	}
}

func TestDecodeJSONRejectsNULAnywhere(t *testing.T) {
	type line struct {
		Note string `json:"note"`
	}
	type in struct {
		Name  string            `json:"name"`
		Ptr   *string           `json:"ptr"`
		Lines []line            `json:"lines"`
		Tags  map[string]string `json:"tags"`
		Any   any               `json:"any"`
		Raw   json.RawMessage   `json:"raw"`
	}
	decode := func(body string) error {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		var v in
		return DecodeJSON(r, &v)
	}
	require.NoError(t, decode(`{"name":"ok","lines":[{"note":"fine"}],"raw":{"a":"b"},"any":["x"]}`))
	require.NoError(t, decode(`{"name":"a\\u0000"}`), "an escaped backslash followed by u0000 is plain text")
	for _, body := range []string{
		`{"name":"a\u0000b"}`,
		`{"ptr":"\u0000"}`,
		`{"lines":[{"note":"ok"},{"note":"\u0000"}]}`,
		`{"tags":{"k":"\u0000"}}`,
		`{"tags":{"\u0000":"v"}}`,
		`{"any":{"deep":["\u0000"]}}`,
		`{"raw":{"limit":"\u0000"}}`,
	} {
		err := decode(body)
		e, ok := apperr.As(err)
		require.True(t, ok, body)
		require.Equal(t, apperr.CodeValidationFailed, e.Code, body)
	}
}
