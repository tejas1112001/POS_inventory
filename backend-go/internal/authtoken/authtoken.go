// Package authtoken signs and verifies access JWTs. Tenant and platform
// tokens use different key rings and audiences, so a token from one can
// never be accepted by the other (PHASE1_DESIGN §4.1, §7.2, S3).
package authtoken

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
)

// Audiences.
const (
	AudTenant   = "tenant"
	AudPlatform = "platform"
)

// MaxLifetime caps exp - iat for any accepted token.
const MaxLifetime = 15 * time.Minute

// Claims are the access-token claims.
type Claims struct {
	jwt.RegisteredClaims
	TenantID        string `json:"tid,omitempty"` // tenant tokens
	TokenVersion    int    `json:"tv,omitempty"`  // tenant tokens
	SessionID       string `json:"sid,omitempty"` // refresh family / platform session
	ImpersonationID string `json:"imp,omitempty"`
	Mode            string `json:"mode,omitempty"` // impersonation mode
}

// Errors returned by Verify.
var (
	ErrExpired = errors.New("authtoken: expired")
	ErrInvalid = errors.New("authtoken: invalid")
)

// Signer issues and verifies tokens for one audience.
type Signer struct {
	ring config.KeyRing
	aud  string
	ttl  time.Duration
	now  func() time.Time
}

// NewSigner creates a signer. ttl must not exceed MaxLifetime.
func NewSigner(ring config.KeyRing, aud string, ttl time.Duration) (*Signer, error) {
	if len(ring) == 0 {
		return nil, errors.New("authtoken: empty key ring")
	}
	if ttl <= 0 || ttl > MaxLifetime {
		return nil, errors.New("authtoken: ttl must be between 0 and 15m")
	}
	return &Signer{ring: ring, aud: aud, ttl: ttl, now: time.Now}, nil
}

// TTL returns the access-token lifetime.
func (s *Signer) TTL() time.Duration { return s.ttl }

// Sign issues a token for subject on issuer (the exact host).
func (s *Signer) Sign(issuer, subject string, c Claims) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(s.ttl)
	c.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   subject,
		Audience:  jwt.ClaimStrings{s.aud},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	key := s.ring.Current()
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	t.Header["kid"] = key.ID
	signed, err := t.SignedString(key.Bytes())
	return signed, exp, err
}

// Verify checks signature (HS256 only, key by kid), issuer, audience,
// exp/nbf/iat and the maximum lifetime.
func (s *Signer) Verify(token, issuer string) (*Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		k, ok := s.ring.Lookup(kid)
		if !ok {
			return nil, ErrInvalid
		}
		return k.Bytes(), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(s.aud),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
		jwt.WithTimeFunc(s.now),
	)
	if errors.Is(err, jwt.ErrTokenExpired) {
		return nil, ErrExpired
	}
	if err != nil {
		return nil, ErrInvalid
	}
	if c.IssuedAt == nil || c.ExpiresAt.Sub(c.IssuedAt.Time) > MaxLifetime || c.Subject == "" {
		return nil, ErrInvalid
	}
	return &c, nil
}
