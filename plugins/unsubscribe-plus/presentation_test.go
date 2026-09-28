package main

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func factsOf(p presentation) map[string]string {
	out := map[string]string{}
	for _, f := range p.Facts {
		out[f.Label] = f.Value
	}
	return out
}

// Case A (§17): newsletter, unsubscribe unknown under Plugin API v1.
func TestPresentationNewsletter(t *testing.T) {
	v := classify(m("Windows Insider <newsletter@insider.example>", "The Windows clipboard just got a big upgrade"), headers{})
	p := v.presentation()
	f := factsOf(p)
	if p.Title != "This looks like a newsletter" || p.Status != "info" || p.Confidence != "high" ||
		f["Type"] != "Newsletter" || f["Priority"] != "Low" || f["Unsubscribe"] != "Not checked" {
		t.Fatalf("presentation = %+v", p)
	}
	if len(p.Reasons) == 0 || !strings.Contains(strings.Join(p.Reasons, " "), "newsletter or digest address") {
		t.Fatalf("reasons = %v", p.Reasons)
	}
}

// §18: unsubscribe available (future headers), no action buttons.
func TestPresentationUnsubscribeAvailable(t *testing.T) {
	v := classify(m("Acme <team@acme.example>", "Hello"), headers{ListUnsubscribe: "<https://acme.example/u?t=SECRET>", ListUnsubscribePost: "List-Unsubscribe=One-Click"})
	p := v.presentation()
	f := factsOf(p)
	if f["Unsubscribe"] != "Available" || f["Method"] != "One-click secure link" {
		t.Fatalf("facts = %v", f)
	}
	for _, s := range append(p.Reasons, p.Title, p.Summary) {
		if strings.Contains(s, "http") || strings.Contains(s, "SECRET") {
			t.Fatalf("presentation leaks the link: %q", s)
		}
	}
	checked := classify(m("Acme <team@acme.example>", "Hello"), headers{Precedence: "bulk"}).presentation()
	if factsOf(checked)["Unsubscribe"] != "Not found" {
		t.Fatalf("with headers seen and no link: %v", factsOf(checked))
	}
}

// §19: important automated mail is never low value.
func TestPresentationSecurityAlert(t *testing.T) {
	p := classify(m("First Example Bank <alerts@firstbank.example>", "Unusual activity on your card"), headers{}).presentation()
	f := factsOf(p)
	if p.Title != "Automated message" || p.Status != "warning" || f["Type"] != "Security alert" || f["Priority"] != "High" || f["Newsletter"] != "No" {
		t.Fatalf("presentation = %+v", p)
	}
}

func TestPresentationReceiptAndPersonal(t *testing.T) {
	p := classify(m("Acme via Stripe <receipts+acct_1@stripe.example>", "Your receipt from Acme"), headers{}).presentation()
	if f := factsOf(p); f["Type"] != "Billing or receipt" || f["Priority"] != "Normal" || p.Status != "neutral" {
		t.Fatalf("receipt = %+v", p)
	}
	p = classify(m("Dana <dana@rivers.example>", "Dinner?"), headers{}).presentation()
	if p.Title != "Not a newsletter" || len(p.Facts) != 0 {
		t.Fatalf("personal = %+v", p)
	}
}

// TideMail's presentation limits (docs/plugins/presentation.md).
func TestPresentationWithinTideMailLimits(t *testing.T) {
	clean := func(s string, max int) {
		t.Helper()
		if utf8.RuneCountInString(s) > max {
			t.Fatalf("%q longer than %d", s, max)
		}
		for _, r := range s {
			if unicode.In(r, unicode.Cc, unicode.Cf) {
				t.Fatalf("control character in %q", s)
			}
		}
	}
	statuses := map[string]bool{"info": true, "success": true, "warning": true, "danger": true, "neutral": true}
	for _, f := range corpus {
		p := classify(f.meta, f.h).presentation()
		clean(p.Title, 100)
		clean(p.Summary, 500)
		if p.Title == "" || !statuses[p.Status] || len(p.Facts) > 8 || len(p.Reasons) > 6 {
			t.Fatalf("%s: %+v", f.name, p)
		}
		for _, fa := range p.Facts {
			clean(fa.Label, 40)
			clean(fa.Value, 160)
		}
		for _, r := range p.Reasons {
			clean(r, 180)
		}
	}
}
