package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretNeverPrints(t *testing.T) {
	const raw = "super-secret-value"
	s := Secret(raw)

	outputs := []string{
		fmt.Sprint(s),
		fmt.Sprintf("%v %s %+v %#v %q", s, s, s, s, s),
		fmt.Sprintf("%v", struct{ S Secret }{s}),
	}
	b, err := json.Marshal(struct{ S Secret }{s})
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, string(b))

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "s", s, slog.Any("any", s))
	outputs = append(outputs, buf.String())

	for _, out := range outputs {
		if strings.Contains(out, raw) {
			t.Fatalf("secret leaked in %q", out)
		}
	}
	if s.Reveal() != raw {
		t.Fatal("Reveal must return the raw value")
	}
}
