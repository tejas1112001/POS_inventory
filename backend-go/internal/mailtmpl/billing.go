package mailtmpl

import (
	"fmt"
	"strings"
)

// BillingNotice is a billing email to a tenant's owner or billing contact
// (BILLING_DESIGN §6). Dates are already formatted; PayURL is the
// provider's payment link when there is one, TenantURL the shop's sign-in.
type BillingNotice struct {
	Kind         string // trial_ending_7, payment_failed, suspended, ...
	TenantName   string
	PlatformName string
	Date         string // the end date the notice is about
	PayURL       string
	TenantURL    string
}

type billingView struct {
	page
	Heading, Action, Link string
	Lines                 []string
}

func plural(n string) string {
	if n == "1" {
		return "1 day"
	}
	return n + " days"
}

// RenderBillingNotice renders one billing notice. Unknown kinds are an
// error (the worker dead-letters them).
func RenderBillingNotice(d BillingNotice) (Rendered, error) {
	shop, platform := oneLine(d.TenantName, 120), oneLine(d.PlatformName, 60)
	d.PayURL, d.TenantURL = webLink(d.PayURL), webLink(d.TenantURL)
	date := oneLine(d.Date, 40)
	v := billingView{page: page{Footnote: "Sent by " + platform + " about the account of " + shop + "."}}
	signIn := func(action string) { v.Action, v.Link = action, d.TenantURL }
	pay := func() {
		if d.PayURL != "" {
			v.Action, v.Link = "Pay now", d.PayURL
		} else {
			signIn("Sign in to pay")
		}
	}
	var subj string
	kind := d.Kind
	days := ""
	for _, p := range []string{"trial_ending_", "subscription_ending_"} {
		if strings.HasPrefix(kind, p) {
			days, kind = strings.TrimPrefix(kind, p), strings.TrimSuffix(p, "_")
		}
	}
	switch kind {
	case "trial_ending":
		subj = fmt.Sprintf("Your %s trial for %s ends in %s", platform, shop, plural(days))
		v.Heading = "Your trial ends in " + plural(days)
		v.Lines = []string{"The free trial of " + shop + " ends on " + date + ".",
			"Choose a plan before then to keep selling without interruption. After the trial your data stays, but the shop becomes read-only until you subscribe."}
		signIn("Choose a plan")
	case "subscription_ending":
		subj = fmt.Sprintf("Your %s subscription for %s ends in %s", platform, shop, plural(days))
		v.Heading = "Your subscription ends in " + plural(days)
		v.Lines = []string{"The subscription of " + shop + " was cancelled and ends on " + date + ".",
			"After that the shop becomes read-only. Subscribe again at any time to continue."}
		signIn("Subscribe again")
	case "payment_failed":
		subj = "Payment failed for " + shop
		v.Heading = "We could not take your payment"
		v.Lines = []string{"The latest payment for " + shop + " on " + platform + " did not go through.",
			"Please pay by " + date + ". After that the shop is suspended until payment is made."}
		pay()
	case "grace_ending":
		subj = "Last day to pay for " + shop
		v.Heading = "Your shop will be suspended soon"
		v.Lines = []string{"The payment for " + shop + " is still due. Without payment the shop is suspended on " + date + "."}
		pay()
	case "suspended":
		subj = shop + " is suspended"
		v.Heading = "Your shop is suspended"
		v.Lines = []string{"Because the payment for " + shop + " is overdue, the shop is suspended and nobody can sign in.",
			"Your data is kept. As soon as payment is made, the shop is active again."}
		if d.PayURL != "" {
			v.Action, v.Link = "Pay now", d.PayURL
		} else {
			v.Lines = append(v.Lines, "Reply to this email and we will send you a payment link.")
		}
	case "reactivated":
		subj = shop + " is active again"
		v.Heading = "Welcome back"
		v.Lines = []string{"Thank you. " + shop + " is active again and everyone can sign in and sell as usual."}
		signIn("Open your shop")
	case "renewal_success":
		subj = "Payment received for " + shop
		v.Heading = "Payment received"
		v.Lines = []string{"Thank you. The subscription of " + shop + " on " + platform + " is renewed."}
		if date != "" {
			v.Lines = append(v.Lines, "The next renewal is on "+date+".")
		}
	default:
		return Rendered{}, fmt.Errorf("mailtmpl: unknown billing notice %q", d.Kind)
	}
	v.Title = subject(subj)
	text, html, err := render("billing", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}

// webLink keeps only plain http(s) links on one line.
func webLink(u string) string {
	u = strings.TrimSpace(u)
	l := strings.ToLower(u)
	if (!strings.HasPrefix(l, "https://") && !strings.HasPrefix(l, "http://")) || strings.ContainsAny(u, " \t\r\n\"<>") {
		return ""
	}
	return u
}
