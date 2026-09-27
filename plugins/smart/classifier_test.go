package main

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// annotationMap flattens annotations to key -> value.
func annotationMap(anns []Annotation) map[string]string {
	out := map[string]string{}
	for _, a := range anns {
		out[a.Key] = a.Value
	}
	return out
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		subject string
		want    map[string]string // keys that must be present with these values
		absent  []string          // keys that must not be present
	}{
		{
			name: "question subject needs reply", from: "Sam Lee <sam@acme.io>",
			subject: "Are we still on for Thursday?",
			want:    map[string]string{"needs_reply": "true"},
		},
		{
			name: "automated github question does not need reply", from: "Jane Doe <notifications@github.com>",
			subject: "Re: [allisonhere/tidemail] Can you review this PR? (#42)",
			want:    map[string]string{"category": "github"},
			absent:  []string{"needs_reply"},
		},
		{
			name: "explicit urgent", from: "Ops <ops@acme.io>",
			subject: "URGENT: production database is down",
			want:    map[string]string{"urgency": "high"},
		},
		{
			name: "github sender", from: "GitHub <noreply@github.com>",
			subject: "[GitHub] A third-party OAuth application has been added",
			want:    map[string]string{"category": "github"},
		},
		{
			name: "receipt", from: "Store <orders@store.example>",
			subject: "Your receipt from Store #1234",
			want:    map[string]string{"category": "receipt"},
			absent:  []string{"importance", "urgency", "needs_reply"},
		},
		{
			name: "shipping", from: "Store <ship@store.example>",
			subject: "Your package has shipped",
			want:    map[string]string{"category": "shipping"},
		},
		{
			name: "shipping arriving today is not urgent", from: "UPS <mcinfo@ups.com>",
			subject: "Your UPS package is arriving today",
			want:    map[string]string{"category": "shipping"},
			absent:  []string{"urgency"},
		},
		{
			name: "security alert is important", from: "Google <no-reply@accounts.google.com>",
			subject: "Security alert: new sign-in on Linux",
			want:    map[string]string{"category": "security", "importance": "high"},
			absent:  []string{"needs_reply"},
		},
		{
			name: "meeting rescheduled", from: "Pat <pat@acme.io>",
			subject: "Meeting rescheduled to 3pm",
			want:    map[string]string{"category": "calendar", "importance": "high"},
		},
		{
			name: "calendar invitation", from: "Calendar <calendar-notification@google.com>",
			subject: "Invitation: Weekly sync @ Mon Oct 5, 2026",
			want:    map[string]string{"category": "calendar"},
		},
		{
			name: "newsletter", from: "The Go Blog <newsletter@golang.example>",
			subject: "Go weekly: generics in practice",
			want:    map[string]string{"category": "newsletter"},
			absent:  []string{"importance", "needs_reply"},
		},
		{
			name: "support ticket", from: "Acme Support <support@acme.example>",
			subject: "Re: Ticket 48213 - login issue",
			want:    map[string]string{"category": "support", "importance": "high"},
		},
		{
			name: "invoice payment due", from: "Billing <billing@host.example>",
			subject: "Invoice INV-2201: payment due Oct 1",
			want:    map[string]string{"category": "billing", "importance": "high"},
		},
		{
			name: "social mention", from: "LinkedIn <messages-noreply@linkedin.com>",
			subject: "Alex mentioned you in a comment",
			want:    map[string]string{"category": "social"},
		},
		{
			name: "automated fallback", from: "Service <no-reply@service.example>",
			subject: "Your weekly usage report",
			want:    map[string]string{"category": "notification"},
		},
		{
			name: "neutral personal note", from: "Mom <mom@gmail.com>",
			subject: "Photos from the weekend",
			want:    map[string]string{"category": "personal"},
			absent:  []string{"needs_reply", "urgency", "importance"},
		},
		{
			name: "neutral company note has no noise", from: "Chris <chris@acme.io>",
			subject: "Notes from the offsite",
			absent:  []string{"needs_reply", "urgency", "importance", "category"},
		},
		{
			name: "precedence: security beats github", from: "GitHub <noreply@github.com>",
			subject: "[GitHub] Please verify your device: verification code",
			want:    map[string]string{"category": "security"},
		},
		{
			name: "precedence: billing beats receipt", from: "Vendor <billing@vendor.example>",
			subject: "Receipt and invoice for September",
			want:    map[string]string{"category": "billing"},
		},
		{
			name: "action required survives automated sender", from: "Bank <no-reply@bank.example>",
			subject: "Action required: confirm your account details",
			want:    map[string]string{"needs_reply": "true", "urgency": "high", "importance": "high"},
		},
		{
			name: "whole words only", from: "Team <team@acme.io>",
			subject: "Showcase for the groups: updates and backups, in case you missed it",
			absent:  []string{"category", "urgency", "needs_reply"},
		},
		{
			name: "reply from a person", from: "Dana <dana@acme.io>",
			subject: "RE: Budget draft",
			want:    map[string]string{"needs_reply": "true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := annotationMap(Classify(MessageMetadata{From: tt.from, Subject: tt.subject}))
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
				}
			}
			for _, k := range tt.absent {
				if _, ok := got[k]; ok {
					t.Errorf("%s should be absent (all: %v)", k, got)
				}
			}
		})
	}
}

func TestIsAutomatedSender(t *testing.T) {
	tests := []struct {
		from string
		want bool
	}{
		{"GitHub <notifications@github.com>", true},
		{"no-reply@accounts.google.com", true},
		{"Shop <noreply@shop.example>", true},
		{"Mail Delivery <MAILER-DAEMON@mx.example>", true},
		{"Bank <do-not-reply@bank.example>", true},
		{"Bank <donotreply@bank.example>", true},
		{"dependabot[bot] <bot@users.noreply.example>", true},
		{"Sam <sam@acme.io>", false},
		{"alerts-team@acme.io", true},
		{"Noah <noah@acme.io>", false},
		{"", false},
	}
	for _, tt := range tests {
		name, local, _ := parseSender(tt.from)
		if got := isAutomatedSender(name, local); got != tt.want {
			t.Errorf("isAutomatedSender(%q) = %v, want %v", tt.from, got, tt.want)
		}
	}
}

func TestClassifyMissingFields(t *testing.T) {
	for _, meta := range []MessageMetadata{
		{},
		{Subject: "?"},
		{From: "not an address <<<"},
		{From: "@", Subject: "Re: Re: Fwd:"},
		{Subject: strings.Repeat("urgent ", 1000)},
	} {
		anns := Classify(meta)
		if anns == nil {
			t.Fatalf("Classify(%+v) returned nil; want an empty slice", meta)
		}
	}
}

// Contract with TideMail's annotation validation (internal/plugin in
// TideMail) and its badge rules (internal/ui/plugins.go).
var (
	tideKeyPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
	tideCategoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,11}$`)
	badgeValues         = map[string]string{"needs_reply": "true", "urgency": "high", "importance": "high"}
)

func checkContract(t *testing.T, context string, anns []Annotation) {
	t.Helper()
	if len(anns) > 32 {
		t.Fatalf("%s: %d annotations exceeds TideMail's limit", context, len(anns))
	}
	seen := map[string]bool{}
	for _, a := range anns {
		if seen[a.Key] {
			t.Fatalf("%s: duplicate key %q", context, a.Key)
		}
		seen[a.Key] = true
		if len(a.Key) > 64 || !tideKeyPattern.MatchString(a.Key) {
			t.Fatalf("%s: key %q breaks TideMail's key rules", context, a.Key)
		}
		if len(a.Value) > 512 || !utf8.ValidString(a.Value) || strings.IndexFunc(a.Value, func(r rune) bool {
			return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
		}) >= 0 {
			t.Fatalf("%s: value %q breaks TideMail's value rules", context, a.Value)
		}
		if math.IsNaN(a.Confidence) || a.Confidence < 0 || a.Confidence > 1 {
			t.Fatalf("%s: confidence %v outside 0..1", context, a.Confidence)
		}
		// Every value this plugin emits should actually produce a badge.
		if want, ok := badgeValues[a.Key]; ok && a.Value != want {
			t.Fatalf("%s: %s=%q would never show a badge", context, a.Key, a.Value)
		}
		if a.Key == "category" && !tideCategoryPattern.MatchString(a.Value) {
			t.Fatalf("%s: category %q is too long or not a badge tag", context, a.Value)
		}
	}
}

// TestContractAcrossCorpus runs every combination of sample senders and
// subjects through the classifier and checks each result against TideMail's
// rules.
func TestContractAcrossCorpus(t *testing.T) {
	senders := []string{
		"", "Sam <sam@acme.io>", "notifications@github.com", "no-reply@accounts.google.com",
		"UPS <mcinfo@ups.com>", "Mom <mom@gmail.com>", "newsletter@blog.example", "support@acme.example",
		"LinkedIn <messages-noreply@linkedin.com>", "dependabot[bot] <x@y.example>", "garbage <<<",
	}
	subjects := []string{
		"", "?", "Can you review this?", "URGENT: action required today", "Invoice past due",
		"Security alert: new sign-in", "Invitation: sync", "Meeting rescheduled", "Your order has shipped",
		"Receipt #1", "Ticket 123 updated", "Weekly digest", "Alex mentioned you", "Re: Re: lunch?",
		"Please confirm by EOD today — deadline", "émoji 🎉 subject ✓", "\x1b[31mcontrol\x00chars",
	}
	for _, from := range senders {
		for _, subject := range subjects {
			anns := Classify(MessageMetadata{From: from, Subject: subject})
			checkContract(t, fmt.Sprintf("from=%q subject=%q", from, subject), anns)
		}
	}
}

func TestAllCategoriesFitBadges(t *testing.T) {
	for _, rule := range categoryRules {
		if !tideCategoryPattern.MatchString(rule.name) {
			t.Errorf("category %q will not render as a TideMail badge", rule.name)
		}
	}
}

func TestHasPhraseWholeWords(t *testing.T) {
	tokens := tokenize("Groups, updates & backups: in case you missed it!")
	for _, phrase := range []string{"ups", "case", "missed it", "groups updates"} {
		want := phrase == "case" || phrase == "missed it" || phrase == "groups updates"
		if got := hasPhrase(tokens, phrase); got != want {
			t.Errorf("hasPhrase(%q) = %v, want %v", phrase, got, want)
		}
	}
}
