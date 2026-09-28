package main

import (
	"testing"
)

// fixture is one message and what Unsubscribe+ should say about it. All
// names, addresses, and domains are invented.
type fixture struct {
	name       string
	meta       metadata
	h          headers
	newsletter bool
	automated  bool
	value      string // "" means no sender_value annotation
	protected  string
}

func m(from, subject string) metadata { return metadata{ID: 1, From: from, Subject: subject} }

// corpus is a realistic set of mail (spec §25) plus the spec's named cases.
var corpus = []fixture{
	// Realistic corpus.
	{name: "substack newsletter", meta: m("Field Notes <fieldnotes@substack.com>", "Issue #42: Tidepools and tiny crabs"),
		h:          headers{ListID: "<fieldnotes.substack.com>", ListUnsubscribe: "<https://fieldnotes.substack.com/action/disable_email?token=x>", ListUnsubscribePost: "List-Unsubscribe=One-Click"},
		newsletter: true, value: valueLow},
	{name: "substack without headers (API v1)", meta: m("Field Notes <fieldnotes@substack.com>", "Tidepools and tiny crabs"), newsletter: true, value: valueLow},
	{name: "github notification", meta: m("Octo Bot <notifications@github.com>", "[acme/app] Run failed: CI - main (a105243)"),
		h:         headers{ListID: "<app.acme.github.com>", ListUnsubscribe: "<mailto:unsub+x@reply.github.com>"},
		automated: true, value: valueNormal, protected: "work"},
	{name: "amazon shipment", meta: m("Amazon.example <shipment-tracking@amazon.example>", "Your package was delivered"), protected: "shipping"},
	{name: "bank security alert", meta: m("First Example Bank <alerts@firstbank.example>", "Unusual activity on your card ending 1234"),
		automated: true, value: valueHigh, protected: "security"},
	{name: "stripe receipt", meta: m("Acme via Stripe <receipts+acct_123@stripe.example>", "Your receipt from Acme #2211-4410"),
		automated: true, value: valueNormal, protected: "billing"},
	{name: "marketing campaign", meta: m("Shoply <promotions@shoply.example>", "Last chance: 30% off everything"), newsletter: true, value: valueLow},
	{name: "weekly saas digest", meta: m("Trackly <digest@trackly.example>", "Your weekly digest"), newsletter: true, value: valueLow},
	{name: "support ticket response", meta: m("Acme Support <support@acme.example>", "Re: [Ticket #4411] Cannot export report"), protected: "support"},
	{name: "calendar reminder", meta: m("Calendar <calendar-notification@calendar.example>", "Invitation: Standup @ Mon Sep 28 9am"),
		automated: true, value: valueNormal, protected: "calendar"},
	{name: "personal email", meta: m("Dana Rivers <dana@rivers.example>", "Dinner on Friday?")},

	// §24 Newsletter detection.
	{name: "1 List-ID only", meta: m("Acme <team@acme.example>", "Hello"), h: headers{ListID: "<news.acme.example>"}, newsletter: true, value: valueLow},
	{name: "2 List-Unsubscribe only", meta: m("Acme <team@acme.example>", "Hello"), h: headers{ListUnsubscribe: "<https://acme.example/u/1>"}, newsletter: true, value: valueLow},
	{name: "3 Precedence bulk", meta: m("Acme <team@acme.example>", "Hello"), h: headers{Precedence: "bulk"}, newsletter: true, automated: true, value: valueLow},
	{name: "4 digest subject + newsletter sender", meta: m("Acme Newsletter <newsletter@acme.example>", "The monthly digest"), newsletter: true, value: valueLow},
	{name: "5 promotions sender", meta: m("Shop <promotions@shop.example>", "New arrivals on sale"), newsletter: true, value: valueLow},
	{name: "6 weekly newsletter", meta: m("The Weekly <newsletter@weekly.example>", "This week in tidepools"), newsletter: true, value: valueLow},

	// §24 Automated senders.
	{name: "7 noreply", meta: m("Example <noreply@example.com>", "Your export is ready"), automated: true, value: valueNormal},
	{name: "8 no-reply", meta: m("Example <no-reply@example.com>", "Your export is ready"), automated: true, value: valueNormal},
	{name: "9 notifications", meta: m("Example <notifications@example.com>", "Someone commented"), automated: true, value: valueNormal},
	{name: "10 mailer-daemon", meta: m("Mail Delivery System <MAILER-DAEMON@mx.example>", "Undeliverable: Hello"), automated: true, value: valueNormal},
	{name: "11 postmaster", meta: m("postmaster@mx.example", "Delivery Status Notification"), automated: true, value: valueNormal},
	{name: "12 human noreen", meta: m("Noreen Ply <noreen@example.com>", "Updates from the trip")},
	{name: "12 human bouncer", meta: m("Bo <bouncer@club.example>", "Guest list")},
	{name: "12 human ann.lee", meta: m("Ann Lee <ann.lee@example.com>", "Weekly sync notes")},

	// §24 Transactional protections (never newsletter, never low).
	{name: "13 security alert with list headers", meta: m("Bank <alerts@bank.example>", "Security alert: new sign-in"),
		h: headers{ListUnsubscribe: "<https://bank.example/u>", Precedence: "bulk"}, automated: true, value: valueHigh, protected: "security"},
	{name: "14 password reset", meta: m("Example <no-reply@example.com>", "Reset your password"), automated: true, value: valueHigh, protected: "security"},
	{name: "15 invoice", meta: m("Acme Billing <billing@acme.example>", "Invoice #1234 for September"), protected: "billing"},
	{name: "16 receipt", meta: m("Shop <noreply@shop.example>", "Your receipt"), h: headers{ListUnsubscribe: "<https://shop.example/u>"}, automated: true, value: valueNormal, protected: "billing"},
	{name: "17 shipping", meta: m("Shop <noreply@shop.example>", "Your order has shipped"), automated: true, value: valueNormal, protected: "billing"},
	{name: "17 shipping only", meta: m("Carrier <noreply@carrier.example>", "Out for delivery today"), automated: true, value: valueNormal, protected: "shipping"},
	{name: "18 support reply", meta: m("Help Desk <help@acme.example>", "Re: your question about exports"), protected: "support"},
	{name: "19 github review request", meta: m("Octo <notifications@github.com>", "[acme/app] @dana requested your review on #42"),
		h: headers{ListID: "<app.acme.github.com>"}, automated: true, value: valueNormal, protected: "work"},
	{name: "20 calendar invite", meta: m("Calendar <noreply@calendar.example>", "Updated invitation: Planning"), automated: true, value: valueNormal, protected: "calendar"},

	// §24 Category/value.
	{name: "31 weak subject alone", meta: m("Dana <dana@example.com>", "Weekly digest of our hikes")},
	{name: "31 news sender alone", meta: m("News <news@acme.example>", "Hello")},
}

func TestCorpus(t *testing.T) {
	for _, f := range corpus {
		v := classify(f.meta, f.h)
		if v.newsletter != f.newsletter || v.automated != f.automated || v.value != f.value || v.protected != f.protected {
			t.Errorf("%s: newsletter=%v automated=%v value=%q protected=%q (score %d); want %v %v %q %q",
				f.name, v.newsletter, v.automated, v.value, v.protected, v.score, f.newsletter, f.automated, f.value, f.protected)
		}
		// Protected mail is never a newsletter and never low value.
		if v.protected != "" && (v.newsletter || v.value == valueLow) {
			t.Errorf("%s: transactional mail labeled newsletter/low", f.name)
		}
	}
}

func annotationMap(anns []annotation) map[string]string {
	out := map[string]string{}
	for _, a := range anns {
		out[a.Key] = a.Value
	}
	return out
}

func TestAnnotationsAreSparseAndValid(t *testing.T) {
	for _, f := range corpus {
		anns := classify(f.meta, f.h).annotations()
		seen := map[string]bool{}
		for _, a := range anns {
			if seen[a.Key] {
				t.Fatalf("%s: duplicate key %s", f.name, a.Key)
			}
			seen[a.Key] = true
			if a.Value == "false" {
				t.Fatalf("%s: explicit negative %s=false", f.name, a.Key)
			}
			if a.Confidence == nil || *a.Confidence <= 0 || *a.Confidence > 1 {
				t.Fatalf("%s: bad confidence on %s", f.name, a.Key)
			}
			if len(a.Value) > 16 {
				t.Fatalf("%s: long value %q (no URLs in annotations)", f.name, a.Value)
			}
		}
		got := annotationMap(anns)
		if f.newsletter != (got[keyCategory] == "newsletter" && got[keyNewsletter] == "true") {
			t.Fatalf("%s: category/newsletter = %v", f.name, got)
		}
		if !f.newsletter && got[keyCategory] != "" {
			t.Fatalf("%s: set category without newsletter evidence: %v", f.name, got)
		}
	}
	if anns := classify(m("Dana Rivers <dana@rivers.example>", "Dinner on Friday?"), headers{}).annotations(); len(anns) != 0 {
		t.Fatalf("personal mail should get no annotations: %v", anns)
	}
}

func TestConfidenceStrengths(t *testing.T) {
	cases := []struct {
		name string
		v    verdict
		want float64
	}{
		{"List-ID", classify(m("a@acme.example", "Hi"), headers{ListID: "<x>"}), 0.98},
		{"List-Unsubscribe", classify(m("a@acme.example", "Hi"), headers{ListUnsubscribe: "<https://x.example/u>"}), 0.98},
		{"Precedence bulk", classify(m("a@acme.example", "Hi"), headers{Precedence: "Bulk"}), 0.95},
		{"newsletter sender", classify(m("newsletter@acme.example", "Hi"), headers{}), 0.85},
	}
	for _, c := range cases {
		if !c.v.newsletter || c.v.newsletterConf != c.want {
			t.Errorf("%s: newsletter=%v conf=%v, want %v", c.name, c.v.newsletter, c.v.newsletterConf, c.want)
		}
	}
}

func TestUnsubscribeParsing(t *testing.T) {
	cases := []struct {
		name, list, post string
		want             unsubscribe
	}{
		{"21 https", "<https://news.example/u?t=1>", "", unsubscribe{true, "https", false}},
		{"22 mailto", "<mailto:unsub@news.example?subject=unsubscribe>", "", unsubscribe{true, "mailto", false}},
		{"23 prefer https", "<mailto:unsub@news.example>, <http://news.example/u>, <https://news.example/u>", "", unsubscribe{true, "https", false}},
		{"23 http over mailto", "<mailto:unsub@news.example>, <http://news.example/u>", "", unsubscribe{true, "http", false}},
		{"24 one-click", "<https://news.example/u>", "List-Unsubscribe=One-Click", unsubscribe{true, "https", true}},
		{"24 one-click needs https", "<mailto:unsub@news.example>", "List-Unsubscribe=One-Click", unsubscribe{true, "mailto", false}},
		{"25 malformed ignored", "<javascript:alert(1)>, <https://>, <mailto:not an address>, <https://user:pw@x.example/u>, https://no-brackets.example", "", unsubscribe{}},
		{"25 malformed then valid", "<ftp://x.example/u>, <mailto:u@news.example>", "", unsubscribe{true, "mailto", false}},
		{"25 unterminated", "<https://news.example/u", "", unsubscribe{}},
		{"empty", "", "", unsubscribe{}},
	}
	for _, c := range cases {
		if got := parseUnsubscribe(c.list, c.post); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestUnsubscribeAnnotationsCarryNoURL(t *testing.T) {
	v := classify(m("a@acme.example", "Hi"), headers{ListUnsubscribe: "<https://acme.example/u?token=SECRET>", ListUnsubscribePost: "List-Unsubscribe=One-Click"})
	got := annotationMap(v.annotations())
	want := map[string]string{keyCategory: "newsletter", keyNewsletter: "true", keySenderValue: valueLow,
		keyUnsubAvail: "true", keyUnsubMethod: "https", keyUnsubOneClik: "true"}
	if len(got) != len(want) {
		t.Fatalf("annotations = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q (all %v)", k, got[k], v, got)
		}
	}
}

// §15: no order dependence, no global state.
func TestClassificationIsDeterministic(t *testing.T) {
	first := make([][]annotation, len(corpus))
	for i, f := range corpus {
		first[i] = classify(f.meta, f.h).annotations()
	}
	for i := len(corpus) - 1; i >= 0; i-- {
		again := classify(corpus[i].meta, corpus[i].h).annotations()
		if len(again) != len(first[i]) {
			t.Fatalf("%s changed between runs", corpus[i].name)
		}
		for j := range again {
			if again[j].Key != first[i][j].Key || again[j].Value != first[i][j].Value || *again[j].Confidence != *first[i][j].Confidence {
				t.Fatalf("%s changed between runs", corpus[i].name)
			}
		}
	}
}
