package obs

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

func TestRedaction(t *testing.T) {
	const (
		pw     = "hunter2-plain"
		tok    = "eyJhbGciOiJIUzI1NiJ9.payload.sig"
		dbpw   = "db-pass-123"
		cookie = "__Host-rt=abc123refresh"
	)
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			log := NewLogger(&buf, "debug", format)
			log.With("refresh_token", tok).Info("login",
				"password", pw,
				"Authorization", "Bearer "+tok,
				slog.Group("req", "cookie", cookie, "smtp_password", pw),
				"target", "postgres://pos_app:"+dbpw+"@db:5432/pos",
				"note", "header was Bearer "+tok,
				"err", errors.New("dial postgres://u:"+dbpw+"@h/x failed"),
				"secret_type", secrets.Secret(pw),
				"wrapped", secrets.Secret(pw),
				"email", "owner@example.com",
			)
			out := buf.String()
			for _, leaked := range []string{pw, tok, dbpw, "abc123refresh"} {
				if strings.Contains(out, leaked) {
					t.Fatalf("%q leaked in %s", leaked, out)
				}
			}
			if !strings.Contains(out, "owner@example.com") {
				t.Fatal("non-sensitive field was dropped")
			}
			if !strings.Contains(out, "db:5432/pos") {
				t.Fatal("URL host should stay visible")
			}
		})
	}
}

func TestLevelFallback(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, "bogus", "json")
	log.Debug("hidden")
	log.Info("shown")
	if strings.Contains(buf.String(), "hidden") || !strings.Contains(buf.String(), "shown") {
		t.Fatalf("unexpected output %s", buf.String())
	}
}
