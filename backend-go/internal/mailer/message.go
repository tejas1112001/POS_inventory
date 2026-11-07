// Package mailer sends email over SMTP (EMAIL_AND_RECEIPTS §1–§3). It
// validates every message against header injection before building it,
// dials only addresses that pass the SSRF guard, and classifies failures so
// the outbox worker can retry, fall back or give up.
package mailer

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits (EMAIL_AND_RECEIPTS §1).
const (
	MaxRecipients    = 10
	MaxSubjectRunes  = 200
	MaxNameRunes     = 78
	MaxAttachment    = 2 << 20
	MaxAddressLength = 254
)

// Address is a mailbox with an optional display name.
type Address struct {
	Name  string
	Email string
}

// Attachment is one file. Receipts attach a single PDF.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Message is one email. Build names with SanitizeName; Validate refuses
// anything that could inject headers.
type Message struct {
	From, ReplyTo Address
	To            []Address
	Subject       string
	HTMLBody      string
	TextBody      string
	Attachments   []Attachment
	MessageID     string            // "key@domain", without angle brackets
	Headers       map[string]string // allow-listed headers only
}

// Sender delivers a message. Errors wrap one of the class errors below.
type Sender interface {
	Send(ctx context.Context, m Message) (providerID string, err error)
}

// Error classes drive the retry policy.
var (
	ErrTransient = errors.New("transient") // 4xx, timeouts, resets: retry
	ErrPermanent = errors.New("permanent") // 5xx recipient/content: dead
	ErrAuth      = errors.New("auth")      // credentials, TLS, blocked host: sender config is failing
	ErrQuota     = errors.New("quota")     // provider sending limit: retry later or fall back
	ErrInvalid   = errors.New("invalid message")
)

// allowedHeaders may be set through Message.Headers.
var allowedHeaders = map[string]bool{
	"List-Unsubscribe":      true,
	"List-Unsubscribe-Post": true,
}

var (
	messageIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:+-]{1,200}@[A-Za-z0-9.-]{1,253}$`)
	attachmentTypes  = map[string]bool{"application/pdf": true}
)

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == ' ' || r == ' ' {
			return true
		}
	}
	return false
}

// SanitizeName makes data (customer names, business names) safe as a
// display name: control characters removed, spaces collapsed, at most 78
// runes. go-mail RFC 2047-encodes the result as a second layer.
func SanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
	if utf8.RuneCountInString(s) > MaxNameRunes {
		s = strings.TrimSpace(string([]rune(s)[:MaxNameRunes]))
	}
	return s
}

// ValidEmail reports whether s is a bare, valid address without control
// characters.
func ValidEmail(s string) bool {
	if s == "" || len(s) > MaxAddressLength || hasControl(s) {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && a.Name == ""
}

func checkAddress(field string, a Address, required bool) error {
	if a.Email == "" && !required {
		if a.Name != "" {
			return fmt.Errorf("%w: %s has a name but no address", ErrInvalid, field)
		}
		return nil
	}
	if !ValidEmail(a.Email) {
		return fmt.Errorf("%w: %s address", ErrInvalid, field)
	}
	if hasControl(a.Name) || utf8.RuneCountInString(a.Name) > MaxNameRunes {
		return fmt.Errorf("%w: %s display name", ErrInvalid, field)
	}
	return nil
}

// Validate refuses CR, LF, NUL and other control characters in every
// address, display name, subject and header, before anything is built
// (EMAIL_AND_RECEIPTS §3.2).
func (m Message) Validate() error {
	if err := checkAddress("from", m.From, true); err != nil {
		return err
	}
	if err := checkAddress("reply-to", m.ReplyTo, false); err != nil {
		return err
	}
	if len(m.To) == 0 || len(m.To) > MaxRecipients {
		return fmt.Errorf("%w: 1 to %d recipients", ErrInvalid, MaxRecipients)
	}
	for _, a := range m.To {
		if err := checkAddress("to", a, true); err != nil {
			return err
		}
	}
	if m.Subject == "" || hasControl(m.Subject) || utf8.RuneCountInString(m.Subject) > MaxSubjectRunes {
		return fmt.Errorf("%w: subject", ErrInvalid)
	}
	if m.TextBody == "" && m.HTMLBody == "" {
		return fmt.Errorf("%w: empty body", ErrInvalid)
	}
	if m.MessageID != "" && !messageIDPattern.MatchString(m.MessageID) {
		return fmt.Errorf("%w: message id", ErrInvalid)
	}
	for k, v := range m.Headers {
		if !allowedHeaders[k] || hasControl(v) || len(v) > 1000 {
			return fmt.Errorf("%w: header %q", ErrInvalid, k)
		}
	}
	for _, a := range m.Attachments {
		if a.Name == "" || hasControl(a.Name) || strings.ContainsAny(a.Name, `/\"`) || len(a.Name) > 100 {
			return fmt.Errorf("%w: attachment name", ErrInvalid)
		}
		if !attachmentTypes[a.ContentType] || len(a.Data) == 0 || len(a.Data) > MaxAttachment {
			return fmt.Errorf("%w: attachment", ErrInvalid)
		}
	}
	return nil
}

// Class returns the class name stored in email_outbox.last_error_class.
func Class(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrQuota):
		return "quota"
	case errors.Is(err, ErrAuth):
		return "auth"
	case errors.Is(err, ErrPermanent), errors.Is(err, ErrInvalid):
		return "permanent"
	default:
		return "transient"
	}
}
