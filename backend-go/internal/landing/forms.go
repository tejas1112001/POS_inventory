package landing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Form names (also part of the signed time stamp).
const (
	formFind    = "find"
	formContact = "contact"
)

// Spam checks: a form sent less than minFormAge after the page was served
// is a bot; one older than maxFormAge asks the visitor to send it again.
const (
	minFormAge    = 2 * time.Second
	maxFormAge    = 24 * time.Hour
	formBodyLimit = 16 << 10
	// findBucket groups "Find my shop" requests for one address, so repeats
	// within it send one email.
	findBucket = 15 * time.Minute
)

// honeypot is a field hidden from people; bots fill it in.
const honeypot = "website"

const (
	qQueueFindShop = `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, template, template_data)
		VALUES ($1, 'platform_notice', $2, '` + mailtmpl.FindShopTemplate + `', '{}') ON CONFLICT (idempotency_key) DO NOTHING`
	qInsertContact = `INSERT INTO platform.contact_requests (topic, name, email, business, phone, message)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6) RETURNING id`
	qQueueContact = `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, template, template_data)
		VALUES ($1, 'platform_notice', $2, '` + mailtmpl.ContactRequestTemplate + `', $3) ON CONFLICT (idempotency_key) DO NOTHING`
)

// store is where the forms write (the database; a fake in unit tests).
type store interface {
	// queueFindShop queues one find_shop email row (once per key).
	queueFindShop(ctx context.Context, key, email string) error
	// saveContact stores a contact request and, when notify is set, queues
	// the notice to that address in the same transaction.
	saveContact(ctx context.Context, in contactInput, notify string) (int64, error)
}

type dbStore struct{ sessions *tenancy.Sessions }

func (d dbStore) queueFindShop(ctx context.Context, key, email string) error {
	return d.sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		_, err := tx.Exec(ctx, qQueueFindShop, key, email)
		return err
	})
}

func (d dbStore) saveContact(ctx context.Context, in contactInput, notify string) (int64, error) {
	var id int64
	err := d.sessions.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		if err := tx.QueryRow(ctx, qInsertContact, in.topic, in.name, in.email, in.business, in.phone, in.message).Scan(&id); err != nil {
			return err
		}
		if notify == "" {
			return nil
		}
		data, err := json.Marshal(map[string]int64{"request_id": id})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, qQueueContact, "contact:"+strconv.FormatInt(id, 10), notify, string(data))
		return err
	})
	return id, err
}

// token is "<unix time>.<mac>": proof that the form came from a page this
// site served, and when.
func (s *Site) token(name string) string {
	ts := strconv.FormatInt(s.d.Now().Unix(), 10)
	return ts + "." + s.mac(name, ts)
}

func (s *Site) mac(name, ts string) string {
	m := hmac.New(sha256.New, s.d.FormKey)
	m.Write([]byte(name + "|" + ts))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

type tokenState int

const (
	tokenOK tokenState = iota
	tokenBad
	tokenFast
	tokenOld
)

func (s *Site) checkToken(name, tok string) tokenState {
	ts, mac, ok := strings.Cut(tok, ".")
	if !ok || !hmac.Equal([]byte(mac), []byte(s.mac(name, ts))) {
		return tokenBad
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return tokenBad
	}
	age := s.d.Now().Sub(time.Unix(sec, 0))
	switch {
	case age < minFormAge:
		return tokenFast
	case age > maxFormAge:
		return tokenOld
	}
	return tokenOK
}

// readForm applies the checks every form shares. It returns false when it
// has answered already; dropped is true for a bot that gets the normal
// "sent" reply without anything happening.
func (s *Site) readForm(w http.ResponseWriter, r *http.Request, name string) (vals url.Values, dropped, ok bool) {
	if !s.d.Network.SameOrigin(r) {
		s.fail(w, r, http.StatusForbidden, "This form could not be sent", "Open this page again and send the form from there.")
		return nil, false, false
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/x-www-form-urlencoded" {
		s.fail(w, r, http.StatusUnsupportedMediaType, "This form could not be sent", "Open this page again and send the form from there.")
		return nil, false, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, formBodyLimit)
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, http.StatusBadRequest, "This form could not be sent", "The form was too long or not readable. Shorten your message and try again.")
		return nil, false, false
	}
	vals = r.PostForm
	if vals.Get(honeypot) != "" {
		s.drop(r, name, "honeypot")
		return vals, true, true
	}
	switch s.checkToken(name, vals.Get("ft")) {
	case tokenBad:
		s.drop(r, name, "token")
		return vals, true, true
	case tokenFast:
		s.drop(r, name, "too fast")
		return vals, true, true
	case tokenOld:
		return vals, false, true // the caller asks to send it again
	}
	return vals, false, true
}

func (s *Site) drop(r *http.Request, name, why string) {
	httpx.LoggerFrom(r.Context()).InfoContext(r.Context(), "landing form dropped", slog.String("form", name), slog.String("reason", why))
	if s.d.Metrics != nil {
		s.d.Metrics.RateLimited.WithLabelValues("landing_" + name + "_spam").Inc()
	}
}

func (s *Site) fail(w http.ResponseWriter, r *http.Request, status int, heading, msg string) {
	v := s.view(r, heading+" | "+s.d.PlatformName, msg, "")
	v.Noindex, v.Heading, v.Message = true, heading, msg
	s.render(w, r, status, "error", v)
}

func (s *Site) tooMany(w http.ResponseWriter, r *http.Request, limiter string, retry time.Duration) {
	s.limited(limiter)
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
	s.fail(w, r, http.StatusTooManyRequests, "Too many requests", "Please wait a while and try again.")
}

func seeOther(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// clean trims a single-line field and reports whether it has control
// characters.
func clean(v string) (string, bool) {
	v = strings.TrimSpace(v)
	for _, c := range v {
		if unicode.IsControl(c) {
			return v, false
		}
	}
	return v, utf8.ValidString(v)
}

// -------------------------------------------------------- Find my shop

func (s *Site) findPage(w http.ResponseWriter, r *http.Request) {
	v := s.view(r, "Find my shop | "+s.d.PlatformName,
		"Forgot your shop's address? Enter the owner or billing email and we send the address to that email.", "/find-my-shop")
	s.render(w, r, http.StatusOK, "find", v)
}

func (s *Site) findSent(w http.ResponseWriter, r *http.Request) {
	v := s.view(r, "Check your email | "+s.d.PlatformName, "Find my shop.", "")
	v.Noindex = true
	s.render(w, r, http.StatusOK, "find_sent", v)
}

func (s *Site) findAgain(w http.ResponseWriter, r *http.Request, status int, email, field, alert string) {
	v := s.view(r, "Find my shop | "+s.d.PlatformName, "Find my shop.", "")
	v.Noindex = true
	v.Find.Values = map[string]string{"email": email}
	v.Find.Errors = map[string]string{}
	if field != "" {
		v.Find.Errors["email"] = field
	}
	v.Find.Alert = alert
	s.render(w, r, status, "find", v)
}

// findSubmit is POST /find-my-shop. Every valid request queues one email
// row and gets the same reply; the worker looks the shops up and sends
// nothing when there are none, so the reply and its timing say nothing
// about which emails have a shop.
func (s *Site) findSubmit(w http.ResponseWriter, r *http.Request) {
	if over(s.findIP, r, "find:"+s.clientKey(r)) {
		s.tooMany(w, r, "landing_find_ip", s.d.Limits.Window)
		return
	}
	vals, dropped, ok := s.readForm(w, r, formFind)
	if !ok {
		return
	}
	if dropped {
		seeOther(w, r, "/find-my-shop/sent")
		return
	}
	email, okText := clean(vals.Get("email"))
	if s.checkToken(formFind, vals.Get("ft")) == tokenOld {
		s.findAgain(w, r, http.StatusUnprocessableEntity, email, "", "This page was open for a long time. Please send the form again.")
		return
	}
	if !okText || !mailer.ValidEmail(email) {
		s.findAgain(w, r, http.StatusUnprocessableEntity, email, "Enter an email address, like name@example.com.", "Check the email address.")
		return
	}
	norm := strings.ToLower(email)
	sum := sha256.Sum256([]byte("find_shop|" + norm))
	id := hex.EncodeToString(sum[:16])
	if over(s.findEmail, r, "findmail:"+id) {
		s.drop(r, formFind, "address limit")
		seeOther(w, r, "/find-my-shop/sent")
		return
	}
	bucket := s.d.Now().Unix() / int64(findBucket/time.Second)
	key := "find_shop:" + id + ":" + strconv.FormatInt(bucket, 10)
	if err := s.store.queueFindShop(r.Context(), key, norm); err != nil {
		httpx.LoggerFrom(r.Context()).ErrorContext(r.Context(), "find my shop not queued", slog.Any("err", err))
		s.fail(w, r, http.StatusServiceUnavailable, "Please try again", "We could not take your request just now. Please try again in a few minutes.")
		return
	}
	s.mailQueued(r.Context())
	seeOther(w, r, "/find-my-shop/sent")
}

func (s *Site) mailQueued(ctx context.Context) {
	if s.d.MailQueued != nil {
		s.d.MailQueued(context.WithoutCancel(ctx))
	}
}

// ------------------------------------------------------------- Contact

// Contact form limits (platform.contact_requests).
const (
	maxName     = 120
	maxBusiness = 120
	maxPhone    = 40
	maxMessage  = 2000
)

func (s *Site) contactPage(w http.ResponseWriter, r *http.Request) {
	v := s.view(r, "Request a demo | "+s.d.PlatformName,
		"Ask for a demo of "+s.d.PlatformName+" or send us a question. We reply by email.", "/contact")
	if r.URL.Query().Get("topic") == "question" {
		v.Contact.Values["topic"] = "question"
	}
	s.render(w, r, http.StatusOK, "contact", v)
}

func (s *Site) contactSent(w http.ResponseWriter, r *http.Request) {
	v := s.view(r, "Thank you | "+s.d.PlatformName, "Your message was sent.", "")
	v.Noindex = true
	s.render(w, r, http.StatusOK, "contact_sent", v)
}

type contactInput struct {
	topic, name, email, business, phone, message string
}

func (c contactInput) values() map[string]string {
	return map[string]string{"topic": c.topic, "name": c.name, "email": c.email, "business": c.business,
		"phone": c.phone, "message": c.message}
}

func validPhone(p string) bool {
	for _, c := range p {
		if !strings.ContainsRune("0123456789+-() .", c) {
			return false
		}
	}
	return true
}

func readContact(vals url.Values) (contactInput, map[string]string) {
	errs := map[string]string{}
	var c contactInput
	c.topic = vals.Get("topic")
	if c.topic != "question" {
		c.topic = "demo"
	}
	var ok bool
	if c.name, ok = clean(vals.Get("name")); !ok || c.name == "" || utf8.RuneCountInString(c.name) > maxName {
		errs["name"] = "Enter your name (up to 120 characters)."
	}
	if c.email, ok = clean(vals.Get("email")); !ok || !mailer.ValidEmail(c.email) {
		errs["email"] = "Enter an email address, like name@example.com."
	}
	if c.business, ok = clean(vals.Get("business")); !ok || utf8.RuneCountInString(c.business) > maxBusiness {
		errs["business"] = "Use up to 120 characters."
	}
	if c.phone, ok = clean(vals.Get("phone")); !ok || utf8.RuneCountInString(c.phone) > maxPhone || !validPhone(c.phone) {
		errs["phone"] = "Use digits, spaces and + - ( ) only."
	}
	c.message = strings.TrimSpace(strings.ReplaceAll(vals.Get("message"), "\r\n", "\n"))
	msgOK := utf8.ValidString(c.message)
	for _, ch := range c.message {
		if unicode.IsControl(ch) && ch != '\n' && ch != '\t' {
			msgOK = false
		}
	}
	switch {
	case !msgOK || utf8.RuneCountInString(c.message) > maxMessage:
		errs["message"] = "Use up to 2,000 characters."
	case c.topic == "question" && c.message == "":
		errs["message"] = "Write your question."
	}
	return c, errs
}

// contactSubmit is POST /contact: a demo request or a question.
func (s *Site) contactSubmit(w http.ResponseWriter, r *http.Request) {
	if over(s.contactIP, r, "contact:"+s.clientKey(r)) {
		s.tooMany(w, r, "landing_contact_ip", s.d.Limits.Window)
		return
	}
	vals, dropped, ok := s.readForm(w, r, formContact)
	if !ok {
		return
	}
	if dropped {
		seeOther(w, r, "/contact/sent")
		return
	}
	in, errs := readContact(vals)
	again := func(alert string) {
		v := s.view(r, "Request a demo | "+s.d.PlatformName, "Request a demo.", "")
		v.Noindex = true
		v.Contact.Values, v.Contact.Errors, v.Contact.Alert = in.values(), errs, alert
		s.render(w, r, http.StatusUnprocessableEntity, "contact", v)
	}
	if s.checkToken(formContact, vals.Get("ft")) == tokenOld {
		again("This page was open for a long time. Please send the form again.")
		return
	}
	if len(errs) > 0 {
		again("Check the fields marked below.")
		return
	}
	if over(s.contactAll, r, "contact:all") {
		s.tooMany(w, r, "landing_contact_total", s.d.Limits.Window)
		return
	}
	id, err := s.store.saveContact(r.Context(), in, s.d.ContactEmail)
	if err != nil {
		httpx.LoggerFrom(r.Context()).ErrorContext(r.Context(), "contact request not stored", slog.Any("err", err))
		s.fail(w, r, http.StatusServiceUnavailable, "Please try again", "We could not take your message just now. Please try again in a few minutes.")
		return
	}
	httpx.LoggerFrom(r.Context()).InfoContext(r.Context(), "contact request stored", slog.Int64("contact_request_id", id),
		slog.String("topic", in.topic), slog.Bool("notified", s.d.ContactEmail != ""))
	if s.d.ContactEmail != "" {
		s.mailQueued(r.Context())
	}
	seeOther(w, r, "/contact/sent")
}

// ---------------------------------------------------------- Staff login

func (s *Site) loginView(r *http.Request) *view {
	v := s.view(r, "Staff sign-in | "+s.d.PlatformName, "Staff sign in at their shop's own address.", "/login")
	v.Noindex = true
	return v
}

// login is the staff sign-in helper: staff sign in on their shop's own
// address, so this page only takes them there.
func (s *Site) login(w http.ResponseWriter, r *http.Request) {
	s.setLoginCSP(w)
	s.render(w, r, http.StatusOK, "login", s.loginView(r))
}

// setLoginCSP lets the sign-in form's redirect reach a shop address
// (form-action also covers redirects after a form is sent).
func (s *Site) setLoginCSP(w http.ResponseWriter) {
	shops := httpx.OriginURL(s.d.PublicScheme, "*."+s.d.BaseDomain, s.d.PublicPort)
	csp := strings.Replace(httpx.CSPStrict, "form-action 'self'", "form-action 'self' "+shops, 1)
	w.Header().Set("Content-Security-Policy", csp)
}

// shopSlug turns what a visitor typed ("my-shop", "my-shop.<domain>",
// "https://my-shop.<domain>/login") into a slug, or "".
func (s *Site) shopSlug(in string) string {
	v := strings.ToLower(strings.TrimSpace(in))
	if i := strings.Index(v, "://"); i >= 0 {
		v = v[i+3:]
	}
	if i := strings.IndexAny(v, "/?#"); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, ':'); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSuffix(v, ".")
	v = strings.TrimSuffix(v, "."+s.d.BaseDomain)
	if !httpx.ValidSlug(v) || tenancy.ReservedSlugs[v] {
		return ""
	}
	return v
}

// loginGo is GET /login/go?shop=...: a redirect to that shop's sign-in
// page. It never checks whether the shop exists.
func (s *Site) loginGo(w http.ResponseWriter, r *http.Request) {
	if over(s.loginIP, r, "login:"+s.clientKey(r)) {
		s.tooMany(w, r, "landing_login_ip", time.Minute)
		return
	}
	shop := r.URL.Query().Get("shop")
	slug := s.shopSlug(shop)
	if slug == "" {
		s.setLoginCSP(w)
		v := s.loginView(r)
		v.Shop = strings.TrimSpace(shop)
		if rs := []rune(v.Shop); len(rs) > 100 {
			v.Shop = string(rs[:100])
		}
		v.ShopError = "Enter your shop's address, like your-shop." + s.d.BaseDomain + "."
		s.render(w, r, http.StatusUnprocessableEntity, "login", v)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	// slug passed ValidSlug, so the target is always <slug>.<BASE_DOMAIN>.
	http.Redirect(w, r, httpx.TenantURL(s.d.PublicScheme, slug, s.d.BaseDomain, s.d.PublicPort)+"/login", http.StatusSeeOther) //nolint:gosec // G710: host is built from a validated slug and BASE_DOMAIN
}
