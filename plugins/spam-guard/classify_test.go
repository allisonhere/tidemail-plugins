package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

var normal = sensitivityFor("normal")

type msg struct {
	name string
	meta metadata
}

// Genuine mail that must never be flagged at the default sensitivity.
var benign = []msg{
	{"paypal receipt", metadata{From: `PayPal <service@paypal.com>`, Subject: "Receipt for your payment to Acme", Auth: &auth{SPF: "pass", DKIM: "pass", DMARC: "pass"}}},
	{"paypal regional", metadata{From: `PayPal <service@paypal.co.uk>`, Subject: "You have a new message"}},
	{"apple receipt", metadata{From: `Apple <no_reply@email.apple.com>`, Subject: "Your receipt from Apple"}},
	{"aws notice", metadata{From: `Amazon Web Services <no-reply@aws.amazon.com>`, Subject: "Your AWS account bill is available"}},
	{"amazon order", metadata{From: `Amazon.com <auto-confirm@amazon.com>`, Subject: "Your order has shipped"}},
	{"microsoft security", metadata{From: `Microsoft account team <account-security-noreply@accountprotection.microsoft.com>`, Subject: "Unusual sign-in activity", Auth: &auth{SPF: "pass", DKIM: "pass", DMARC: "pass"}}},
	{"google alert", metadata{From: `Google <no-reply@accounts.google.com>`, Subject: "Security alert", Auth: &auth{SPF: "pass", DKIM: "pass", DMARC: "pass"}}},
	{"chase alert", metadata{From: `Chase <no.reply.alerts@alertsp.chase.com>`, Subject: "Your account has a new transaction"}},
	{"apple pie newsletter", metadata{From: `Apple Pie Weekly <news@piebites.example>`, Subject: "10 apple pies you must bake"}},
	{"github", metadata{From: `GitHub <notifications@github.com>`, Subject: "[repo] Please verify your email address"}},
	{"substack", metadata{From: `Some Writer <writer@substack.com>`, Subject: "Weekly roundup", ReplyTo: "writer@gmail.com"}},
	{"colleague", metadata{From: `Ann Lee <ann@acme.example>`, To: "me@acme.example", Subject: "Re: quarterly numbers"}},
	{"photos from a trip", metadata{From: `Ann Lee <ann@acme.example>`, Subject: "Photos from Maui"}},
	{"photo share from a friend", metadata{From: `Sam <sam@gmail.com>`, Subject: "Photos 2026/08/14"}},
	{"relative using a nickname address", metadata{From: `Walter Bayless <waltb0948@live.com>`, Subject: "pics"}},
	{"relative with an unrelated address on a company domain", metadata{From: `Jane Roe <jroe@acme.example>`, Subject: "Pics 2026/08/14"}},
	{"self-sent note", metadata{From: `Pat <pat@example.com>`, To: "pat@example.com", Subject: "pat reminders"}},
	{"small shop on gmail", metadata{From: `Corner Shop Support <cornershop@gmail.com>`, Subject: "Your order is ready"}},
	{"friend on gmail with invoice words", metadata{From: `Sam <sam@gmail.com>`, Subject: "Our dinner receipt", HasAttachment: false}},
	{"forwarded mail failing spf only", metadata{From: `Ann Lee <ann@acme.example>`, Subject: "Fwd: schedule", Auth: &auth{SPF: "fail"}}},
	{"mailing list", metadata{From: `Dev List <dev@lists.example.org>`, ReplyTo: "dev@lists.example.org", Subject: "[dev] release plan", Auth: &auth{SPF: "softfail", DKIM: "none"}}},
	{"zoom invite", metadata{From: `Zoom <no-reply@zoom.us>`, Subject: "Meeting invitation: standup"}},
	{"no sender", metadata{Subject: "hello"}},
	{"unparsable sender", metadata{From: "not an address", Subject: "hello"}},
}

// Mail that should be flagged at the default sensitivity, and how strongly.
var scams = []struct {
	name  string
	meta  metadata
	level string
}{
	{"free-mail pretending to be paypal", metadata{From: `PayPal Support <paypal.help.desk@gmail.com>`, Subject: "Your account has been limited"}, "high"},
	{"lookalike domain with a warning", metadata{From: `PayPal <service@paypa1.com>`, Subject: "Verify your account to restore access"}, "high"},
	{"microsoft on a lookalike", metadata{From: `Microsoft Security <alert@rnicrosoft.com>`, Subject: "Unusual sign-in activity"}, "high"},
	{"address in the display name", metadata{From: `"billing@paypal.com" <x7f3@mailer-kz.example>`, Subject: "Invoice"}, "high"},
	{"address in the name of an unknown company", metadata{From: `"billing@acme-pay.example" <x7f3@mailer-kz.example>`, Subject: "Invoice"}, "medium"},
	{"subdomain trick", metadata{From: `Netflix <billing@netflix.com.account-verify.example>`, Subject: "Payment declined: update your billing details"}, "high"},
	{"failed authentication on a brand claim", metadata{From: `Amazon <orders@amazon-orders.example>`, Subject: "Order confirmation", Auth: &auth{SPF: "fail", DKIM: "fail", DMARC: "fail"}}, "high"},
	{"display name spoof with spam flag", metadata{From: `DHL Express <dhl-parcel@outlook.com>`, Subject: "Package on hold", Auth: &auth{SpamFlag: true}}, "high"},
	{"prize bait from free mail with reply elsewhere", metadata{From: `Claims Dept <claims7731@yahoo.com>`, ReplyTo: "payout.desk@proton.me", Subject: "Congratulations, you've won! Claim your prize: gift card"}, "medium"},
	{"mixed alphabets", metadata{From: "Bank <info@pаypal.example>", Subject: "Confirm your account"}, "medium"},
	{"invoice from free mail", metadata{From: `Accounts <invoice.dept@gmail.com>`, Subject: "Overdue invoice attached", HasAttachment: true, ReplyTo: "pay@proton.me"}, "medium"},
}

func TestGenuineMailIsNeverFlagged(t *testing.T) {
	for _, c := range benign {
		t.Run(c.name, func(t *testing.T) {
			if v := classify(c.meta, normal); v.level != "" {
				t.Errorf("flagged %s (score %d): %v", v.level, v.score, v.signals)
			}
		})
	}
}

func TestScamsAreFlagged(t *testing.T) {
	for _, c := range scams {
		t.Run(c.name, func(t *testing.T) {
			v := classify(c.meta, normal)
			if v.level != c.level {
				t.Errorf("level = %q (score %d), want %q: %v", v.level, v.score, c.level, v.signals)
			}
		})
	}
}

func TestSensitivityMovesTheThreshold(t *testing.T) {
	// A brand claim from a look-alike address and nothing else: 55 points.
	m := metadata{From: `Venmo <hello@venmoo.example>`, Subject: "Hello"}
	m.From = `Someone <hello@venmoo.com>`
	score := classify(m, normal).score
	if score != 55 {
		t.Fatalf("setup: score = %d, want 55 (a lookalike alone)", score)
	}
	for sens, want := range map[string]string{"low": "", "normal": "medium", "high": "medium"} {
		if got := classify(m, sensitivityFor(sens)).level; got != want {
			t.Errorf("%s: level = %q, want %q", sens, got, want)
		}
	}
	// A weaker case only trips at high sensitivity.
	weak := metadata{From: `PayPal <paypal@gmail.com>`, Subject: "Hi"} // 60: brand + free mail
	if classify(weak, sensitivityFor("low")).level != "" || classify(weak, sensitivityFor("high")).level == "" {
		t.Errorf("weak case should be flagged only at high sensitivity: low=%q high=%q", classify(weak, sensitivityFor("low")).level, classify(weak, sensitivityFor("high")).level)
	}
	if got := sensitivityFor("nonsense"); got != normal {
		t.Errorf("unknown sensitivity should mean normal, got %+v", got)
	}
}

func TestMissingAuthIsUnknownNotFailure(t *testing.T) {
	base := metadata{From: `Ann Lee <ann@acme.example>`, Subject: "hello"}
	if v := classify(base, normal); v.level != "" || v.score != 0 {
		t.Errorf("no auth should add nothing: %+v", v)
	}
	withNone := base
	withNone.Auth = &auth{SPF: "none", DKIM: "none", DMARC: "none"}
	if v := classify(withNone, normal); v.score != 0 {
		t.Errorf("'none' is not 'fail': score %d", v.score)
	}
}

func TestAuthFailureAloneIsCapped(t *testing.T) {
	m := metadata{From: `Ann Lee <ann@acme.example>`, Subject: "hello", Auth: &auth{SPF: "fail", DKIM: "fail", DMARC: "fail"}}
	if v := classify(m, normal); v.score > 20 || v.level != "" {
		t.Errorf("failed checks alone must not flag honest mail: %+v", v)
	}
}

func TestVerdictsAreDeterministic(t *testing.T) {
	for _, c := range scams {
		a, b := classify(c.meta, normal), classify(c.meta, normal)
		ja, _ := json.Marshal(a.annotations())
		jb, _ := json.Marshal(b.annotations())
		if !bytes.Equal(ja, jb) || a.score != b.score {
			t.Errorf("%s: two runs differ", c.name)
		}
	}
}

func TestAnnotationsArePositiveOnly(t *testing.T) {
	if got := classify(benign[0].meta, normal).annotations(); len(got) != 0 {
		t.Errorf("clean mail should clear annotations, got %+v", got)
	}
	got := classify(scams[0].meta, normal).annotations()
	byKey := map[string]annotation{}
	for _, a := range got {
		byKey[a.Key] = a
		if a.Confidence == nil || *a.Confidence <= 0 || *a.Confidence > 1 {
			t.Errorf("%s: bad confidence %v", a.Key, a.Confidence)
		}
	}
	if byKey["category"].Value != "suspicious" || byKey["risk"].Value != "high" || byKey["claims_brand"].Value != "paypal" {
		t.Errorf("annotations: %+v", got)
	}
	if len(byKey["category"].Value) > 12 {
		t.Error("category must fit TideMail's tag pattern")
	}
}

func TestDomainHelpers(t *testing.T) {
	for in, want := range map[string]string{
		"paypal.com": "paypal.com", "mail.paypal.com": "paypal.com", "a.b.example.co.uk": "example.co.uk", "x.com.au": "x.com.au", "localhost": "localhost",
	} {
		if got := apex(in); got != want {
			t.Errorf("apex(%q) = %q, want %q", in, got, want)
		}
	}
	if fold("paypa1") != "paypal" || fold("rnicrosoft") != "microsoft" {
		t.Errorf("fold: %q %q", fold("paypa1"), fold("rnicrosoft"))
	}
	if distance("paypal", "paypa", 1) != 1 || distance("paypal", "payments", 1) <= 1 || distance("abc", "abc", 1) != 0 {
		t.Error("distance is wrong")
	}
}

// Header text is attacker-controlled and ends up on a terminal.
func TestCardTextIsPlainAndBounded(t *testing.T) {
	meta := metadata{
		From:    "PayPal \x1b[31m<paypal@gmail.com>‮",
		ReplyTo: "evil\x07@proton.me",
		Subject: "Your account has been limited",
		Auth:    &auth{SPF: "fail"},
	}
	v := classify(meta, normal)
	p := v.presentation(meta)
	if p == nil || v.level == "" {
		t.Fatalf("expected a warning, got %+v", v)
	}
	blob, _ := json.Marshal(p)
	for _, bad := range []string{"\\u001b", "\\u0007", "\\u202e"} {
		if strings.Contains(string(blob), bad) {
			t.Errorf("card contains %s: %s", bad, blob)
		}
	}
	if len(p.Reasons) > maxReasons || len(p.Facts) > maxFacts {
		t.Errorf("card exceeds TideMail's limits: %d reasons, %d facts", len(p.Reasons), len(p.Facts))
	}
	if p.Title == "" || p.Status == "" || p.Confidence == "" {
		t.Errorf("card is incomplete: %+v", p)
	}
}

func TestEveryMessageGetsACardWhenRunByHand(t *testing.T) {
	p := classify(benign[0].meta, normal).presentation(benign[0].meta)
	if p == nil || p.Status != "neutral" || !strings.Contains(p.Title, "Nothing suspicious") {
		t.Errorf("clean mail card: %+v", p)
	}
}

// Patterns seen in real mass-mailed spam (names and addresses are made up).
func TestMassSpamPatterns(t *testing.T) {
	spam := []msg{
		{"stylized name with a digits address", metadata{From: "𝐂𝐥𝐨𝐮𝐝-𝐒𝐭𝐨𝐫𝐚𝐠𝐞 <162219657@utdzzhqlm.us>", To: "jdoe123@example.com", Subject: "jdoe123, ☁️ your memories are at risk — renew in 1 day"}},
		{"payment declined in lookalike letters", metadata{From: "𝗣aym𝗲nt_Declin𝗲d <162219657@dockpckdn.us>", Subject: "⛔️ We have blocked your account!"}},
		{"random domain, brand-like name, username subject", metadata{From: "Tractor-Supply <wXMigEgx@wfcvgvypl.us>", To: "jdoe123@example.com", Subject: "jdoe123 you have (1) reward pending"}},
		{"venmo from a free address", metadata{From: "Support Desk <eddietown5867@outlook.com>", Subject: "Security confirmation required for your Venmo account"}},
		{"brand name on a machine-made address", metadata{From: "DHL <5YvXjMeE@5yvxjmee.fr>", Subject: "Ihr Paket ist zur Auslieferung bereit."}},
		{"your own username as the sender name", metadata{From: "jdoe123 <alert-3162@ygynw.example>", To: "jdoe123@example.com", Subject: "Your Norton Account Has Been Suspended"}},
		{"bare pictures subject", metadata{From: "Tammy Harmon <tesfay.g@ienetworks.example>", Subject: "(5) Picture"}},
		{"blocked account bait", metadata{From: "Alerts <a7s9d8f@qzkxvbnmw.top>", Subject: "⛔️ Your Account Has been Blocked! Your Photos and Videos"}},
	}
	for _, c := range spam {
		t.Run(c.name, func(t *testing.T) {
			if v := classify(c.meta, normal); v.level == "" {
				t.Errorf("not flagged (score %d): %v", v.score, v.signals)
			}
		})
	}
}

func TestOwnExtraDomainsAreNotDoubleCounted(t *testing.T) {
	// Genuine Amazon mail from a company domain that merely contains the name.
	m := metadata{From: "Amazon Music <no-reply@amazonmusic-offers.example>", Subject: "Congratulations! You're eligible for 4 months free"}
	if v := classify(m, normal); v.level != "" && v.score > 60 {
		t.Errorf("name+domain should not stack: %d %v", v.score, v.signals)
	}
	if v := classify(metadata{From: "Amazon Music <no-reply@amazonmusic.com>", Subject: "Congratulations! You're eligible"}, normal); v.level != "" {
		t.Errorf("amazonmusic.com is Amazon's: %v", v.signals)
	}
	// A header that is not a valid address still parses sensibly.
	p := parseParty("service@paypal.com <service@paypal.com>")
	if p.domain != "paypal.com" || p.display != "service@paypal.com" {
		t.Errorf("malformed header parsed as %+v", p)
	}
	if v := classify(metadata{From: "service@paypal.com <service@paypal.com>", Subject: "Receipt for Your Payment"}, normal); v.level != "" {
		t.Errorf("paypal's own malformed header flagged: %v", v.signals)
	}
}
