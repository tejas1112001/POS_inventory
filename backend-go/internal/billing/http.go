package billing

import (
	"errors"
	"io"
	"net/http"
)

// WebhookHandler serves the provider's webhook. The answer never reflects
// the input (BILLING_DESIGN §8).
func (s *Service) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "application/json")
		h.Set("Cache-Control", "no-store")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				_, _ = io.WriteString(w, `{"error":"too large"}`)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"unreadable"}`)
			return
		}
		status, _ := s.HandleWebhook(r.Context(), raw, r.Header)
		w.WriteHeader(status)
		switch status {
		case http.StatusOK:
			_, _ = io.WriteString(w, `{"status":"ok"}`)
		case http.StatusBadRequest:
			_, _ = io.WriteString(w, `{"error":"invalid signature"}`)
		case http.StatusNotFound:
			_, _ = io.WriteString(w, `{"error":"not found"}`)
		default:
			_, _ = io.WriteString(w, `{"error":"try again"}`)
		}
	})
}
