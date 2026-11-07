// Package idempotency implements the optional Idempotency-Key header on
// POSTs that create documents (S2: sales, returns, purchases). The key is
// claimed in the document's own transaction, so a concurrent request with
// the same key waits for it; a completed request is replayed; a key reused
// for a different request is refused. Failed requests roll back, so only
// successful responses are stored.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Header names.
const (
	Header         = "Idempotency-Key"
	ReplayedHeader = "Idempotent-Replayed"
	maxKeyLen      = 100
)

// Key reads the Idempotency-Key header: "" when absent, an error when it
// is too long or not printable ASCII.
func Key(r *http.Request) (string, error) {
	k := strings.TrimSpace(r.Header.Get(Header))
	if k == "" {
		return "", nil
	}
	if len(k) > maxKeyLen {
		return "", apperr.Invalid(Header, "Idempotency-Key must be at most 100 characters.")
	}
	for _, c := range k {
		if c < 0x21 || c > 0x7e {
			return "", apperr.Invalid(Header, "Idempotency-Key must be printable ASCII.")
		}
	}
	return k, nil
}

type hashingBody struct {
	io.Reader
	io.Closer
}

// Hash makes the request body feed a SHA-256 as the JSON decoder reads it
// and returns a function that gives the sum after decoding. scope is
// hashed first, so a key used on one endpoint never replays on another.
func Hash(r *http.Request, scope string) func() []byte {
	h := sha256.New()
	_, _ = io.WriteString(h, scope)
	r.Body = hashingBody{io.TeeReader(r.Body, h), r.Body}
	return func() []byte { return h.Sum(nil) }
}

// Replay is a stored response.
type Replay struct {
	Status int
	Body   []byte
}

const (
	qExpire = `DELETE FROM idempotency_keys WHERE user_id = $1 AND key = $2 AND created_at < now() - interval '24 hours'`
	qClaim  = `INSERT INTO idempotency_keys (user_id, key, request_hash) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`
	qRead   = `SELECT request_hash, status, response FROM idempotency_keys WHERE user_id = $1 AND key = $2`
	qStore  = `UPDATE idempotency_keys SET status = $3, response = $4 WHERE user_id = $1 AND key = $2`
)

// Claim records the key in tx. It returns the stored response when the
// same request already completed (keys expire after 24 hours).
func Claim(ctx context.Context, tx tenancy.TenantTx, userID int64, key string, sum []byte) (*Replay, error) {
	if _, err := tx.Exec(ctx, qExpire, userID, key); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, qClaim, userID, key, sum)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}
	var (
		h      []byte
		status *int
		body   []byte
	)
	if err := tx.QueryRow(ctx, qRead, userID, key).Scan(&h, &status, &body); err != nil {
		return nil, err
	}
	if !bytes.Equal(h, sum) {
		return nil, apperr.Conflict(apperr.CodeConflict, "This Idempotency-Key was already used for a different request.")
	}
	if status == nil {
		return nil, apperr.Conflict(apperr.CodeConflict, "A request with this Idempotency-Key is still in progress.")
	}
	return &Replay{Status: *status, Body: body}, nil
}

// Store saves the response for a claimed key, in the same transaction.
func Store(ctx context.Context, tx tenancy.TenantTx, userID int64, key string, status int, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, qStore, userID, key, status, string(b))
	return err
}

// Write sends a replayed response.
func Write(w http.ResponseWriter, rp *Replay) {
	w.Header().Set(ReplayedHeader, "true")
	httpx.WriteJSON(w, rp.Status, json.RawMessage(rp.Body))
}
