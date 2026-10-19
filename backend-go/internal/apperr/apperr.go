// Package apperr defines the typed errors that services return. httpx maps
// them to HTTP status codes and the {"error","code","fields"} body once.
package apperr

import (
	"errors"
	"fmt"
	"time"
)

// Kind groups errors by the HTTP status they map to.
type Kind int

// Kinds.
const (
	KindInternal Kind = iota
	KindValidation
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindTooLarge
	KindUnsupportedMedia
	KindRateLimited
	KindUnavailable
	KindMisdirected
)

// Error is a typed application error. Message is safe to show to clients;
// Cause is only logged.
type Error struct {
	Kind       Kind
	Code       string
	Message    string
	Fields     map[string]string
	RetryAfter time.Duration
	Cause      error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// Codes (API_CONTRACT §1.1).
const (
	CodeInvalidJSON        = "INVALID_JSON"
	CodeValidationFailed   = "VALIDATION_FAILED"
	CodeUnauthenticated    = "UNAUTHENTICATED"
	CodeInvalidCredentials = "INVALID_CREDENTIALS" //nolint:gosec // error code, not a credential
	CodeTokenExpired       = "TOKEN_EXPIRED"
	CodeMFARequired        = "MFA_REQUIRED"
	CodeForbidden          = "FORBIDDEN"
	CodeStepUpRequired     = "STEP_UP_REQUIRED"
	CodeTenantSuspended    = "TENANT_SUSPENDED"
	CodeTenantReadOnly     = "TENANT_READ_ONLY"
	CodeImpersonationRO    = "IMPERSONATION_READ_ONLY"
	CodePlanLimit          = "PLAN_LIMIT_REACHED"
	CodeFeatureNotInPlan   = "FEATURE_NOT_IN_PLAN"
	CodeInsufficientStock  = "INSUFFICIENT_STOCK"
	CodePriceChanged       = "PRICE_CHANGED"
	CodeProductLocked      = "PRODUCT_LOCKED"
	CodeNotFound           = "NOT_FOUND"
	CodeConflict           = "CONFLICT"
	CodeInvalidState       = "INVALID_STATE"
	CodeBodyTooLarge       = "BODY_TOO_LARGE"
	CodeUnsupportedMedia   = "UNSUPPORTED_MEDIA_TYPE"
	CodeRateLimited        = "RATE_LIMITED"
	CodeAccountLocked      = "ACCOUNT_LOCKED"
	CodeInternal           = "INTERNAL"
	CodeUnavailable        = "UNAVAILABLE"
	CodeMisdirected        = "MISDIRECTED_REQUEST"
)

// Validation reports field errors.
func Validation(fields map[string]string) *Error {
	return &Error{Kind: KindValidation, Code: CodeValidationFailed, Message: "Validation failed", Fields: fields}
}

// Invalid reports one field error and uses it as the message, so clients
// that show only "error" still say what is wrong.
func Invalid(field, msg string) *Error {
	return &Error{Kind: KindValidation, Code: CodeValidationFailed, Message: msg, Fields: map[string]string{field: msg}}
}

// InvalidJSON reports a malformed body.
func InvalidJSON(msg string) *Error {
	return &Error{Kind: KindValidation, Code: CodeInvalidJSON, Message: msg}
}

// Unauthenticated reports a missing or invalid credential.
func Unauthenticated(code, msg string) *Error {
	return &Error{Kind: KindUnauthenticated, Code: code, Message: msg}
}

// Forbidden reports a permission or state denial.
func Forbidden(code, msg string) *Error {
	return &Error{Kind: KindForbidden, Code: code, Message: msg}
}

// NotFound is the single generic not-found error. It never says whether the
// thing exists elsewhere (another tenant, deleted, provisioning).
func NotFound() *Error {
	return &Error{Kind: KindNotFound, Code: CodeNotFound, Message: "Not found"}
}

// Conflict reports a uniqueness or state conflict.
func Conflict(code, msg string) *Error {
	return &Error{Kind: KindConflict, Code: code, Message: msg}
}

// TooLarge reports a body over the cap.
func TooLarge() *Error {
	return &Error{Kind: KindTooLarge, Code: CodeBodyTooLarge, Message: "Request body too large"}
}

// UnsupportedMedia reports a wrong content type.
func UnsupportedMedia() *Error {
	return &Error{Kind: KindUnsupportedMedia, Code: CodeUnsupportedMedia, Message: "Content-Type must be application/json"}
}

// RateLimited reports a rate limit or lockout.
func RateLimited(code string, retryAfter time.Duration) *Error {
	msg := "Too many requests"
	if code == CodeAccountLocked {
		msg = "Account temporarily locked"
	}
	return &Error{Kind: KindRateLimited, Code: code, Message: msg, RetryAfter: retryAfter}
}

// Unavailable reports draining or a dependency outage.
func Unavailable() *Error {
	return &Error{Kind: KindUnavailable, Code: CodeUnavailable, Message: "Service unavailable"}
}

// Misdirected reports a request for a host this server does not serve.
func Misdirected() *Error {
	return &Error{Kind: KindMisdirected, Code: CodeMisdirected, Message: "Misdirected request"}
}

// Internal wraps an unexpected error. The cause is logged, never returned.
func Internal(cause error) *Error {
	return &Error{Kind: KindInternal, Code: CodeInternal, Message: "Internal server error", Cause: cause}
}
