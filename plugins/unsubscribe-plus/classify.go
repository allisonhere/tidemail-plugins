package main

import (
	"math"
	"net/mail"
	"regexp"
	"strings"
)

// Classification is a pure function of one message's header metadata: no
// state, no network, no order dependence, so single, bulk, and automatic
// runs all agree.
//
// Confidences are heuristic strengths for TideMail to show and sort by, not
// calibrated probabilities.

// headers are the list headers Unsubscribe+ understands. TideMail's Plugin
// API v1 does not send them, so today they are always empty and only the
// sender and subject heuristics apply. They are wired through so that a
// future safe-header capability turns on the strongest signals (and
// unsubscribe detection) without changing the classifier.
type headers struct {
	ListID               string
	ListUnsubscribe      string
	ListUnsubscribePost  string
	Precedence           string
	AutoSubmitted        string
	AutoResponseSuppress string
}

// Annotation keys and values.
const (
	keyCategory     = "category"
	keyNewsletter   = "newsletter"
	keyAutomated    = "automated_sender"
	keySenderValue  = "sender_value"
	keyUnsubAvail   = "unsubscribe_available"
	keyUnsubMethod  = "unsubscribe_method"
	keyUnsubOneClik = "unsubscribe_one_click"

	valueLow    = "low"
	valueNormal = "normal"
	valueHigh   = "high"
)

// newsletterThreshold is the evidence score that makes mail a newsletter.
const newsletterThreshold = 4

type annotation struct {
	Key        string   `json:"key"`
	Value      string   `json:"value"`
	Confidence *float64 `json:"confidence,omitempty"`
}

// verdict is one message's classification.
type verdict struct {
	score          int // newsletter evidence
	newsletter     bool
	newsletterConf float64
	automated      bool
	automatedConf  float64
	protected      string // transactional class, "" when none
	value          string // "" when there is nothing to say
	valueConf      float64
	unsub          unsubscribe
	// reasons explain the verdict in plain words, strongest first.
	reasons []string
	// headersSeen is true when TideMail supplied list headers.
	headersSeen bool
}

// sender is the parsed From header.
type sender struct {
	display string   // lowercase display name
	local   string   // lowercase local part
	domain  string   // lowercase domain
	tokens  []string // local part split on . _ - +
	norm    string   // local part without separators
}

func parseSender(from string) sender {
	var s sender
	addr := strings.TrimSpace(from)
	if a, err := mail.ParseAddress(from); err == nil {
		s.display, addr = strings.ToLower(a.Name), a.Address
	}
	addr = strings.ToLower(addr)
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 {
		return s
	}
	s.local, s.domain = addr[:at], addr[at+1:]
	s.tokens = strings.FieldsFunc(s.local, func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '+' })
	s.norm = strings.Join(s.tokens, "")
	return s
}

func (s sender) hasToken(words ...string) bool {
	for _, t := range s.tokens {
		for _, w := range words {
			if t == w {
				return true
			}
		}
	}
	return false
}

// displayHas matches whole words in the display name.
func (s sender) displayHas(words ...string) bool {
	for _, w := range strings.FieldsFunc(s.display, func(r rune) bool { return !isWordRune(r) }) {
		for _, x := range words {
			if w == x {
				return true
			}
		}
	}
	return false
}

// Automated local parts. Matched on whole tokens of the local part, or on
// the separator-free local part for the multi-word forms, never as loose
// substrings, so "noreen@" or "bouncer@" stay human.
var (
	automatedJoined = []string{"noreply", "donotreply", "mailerdaemon"}
	automatedTokens = []string{
		"noreply", "donotreply", "postmaster", "notification", "notifications",
		"updates", "mailer", "bounce", "bounces", "alert", "alerts", "automated", "receipts",
	}
)

// Newsletter platforms whose sending domains mean list mail. Deliberately
// small: no reputation service, no blocklist.
var newsletterDomains = []string{
	"substack.com", "beehiiv.com", "mail.beehiiv.com", "mcsv.net", "mcdlv.net", "list-manage.com",
	"convertkit-mail.com", "convertkit-mail2.com", "buttondown.email", "ghost.io", "sendfox.com",
}

// Transactional classes protect mail from newsletter and low-value labels,
// however automated it is. security also raises value to high.
var protections = []struct {
	class   string
	subject *regexp.Regexp
	senders []string // local-part tokens
}{
	{"security", wordRe(`security`, `verification`, `verify`, `password`, `passcode`, `log[ -]?in`, `sign[ -]?in`,
		`2fa`, `two[- ]factor`, `one[- ]time`, `fraud`, `suspicious`, `unusual activity`, `new device`, `locked`, `authentication`), []string{"security"}},
	{"billing", wordRe(`invoice`, `receipt`, `payment`, `paid`, `billing`, `charged?`, `refund`, `statement`,
		`your order`, `order confirm\w*`, `order #?\d+`), []string{"billing", "invoice", "invoices", "receipts", "payments"}},
	{"shipping", wordRe(`shipment`, `shipped`, `shipping`, `delivered`, `out for delivery`,
		`delivery (date|scheduled|update|attempt|window)`, `tracking`, `package`),
		[]string{"shipment", "shipping", "tracking"}},
	{"support", wordRe(`support`, `ticket`, `case #?\d+`, `request #?\d+`, `help ?desk`), []string{"support", "help", "helpdesk"}},
	{"calendar", wordRe(`invitation`, `invite`, `meeting`, `appointment`, `calendar`, `event reminder`, `accepted`, `declined`),
		[]string{"calendar"}},
	{"work", wordRe(`review requested`, `requested your review`, `review request`, `pull request`, `merge request`, `assigned`,
		`mentioned you`, `run failed`, `build failed`, `workflow run`, `ci failed`), nil},
}

// repoPrefix is GitHub/GitLab's "[owner/repo]" subject prefix.
var repoPrefix = regexp.MustCompile(`^(re:\s*)?\[[\w.-]+/[\w.-]+\]`)

var workDomains = []string{"github.com", "gitlab.com", "bitbucket.org"}

// Subject hints add weak newsletter evidence; alone they never classify.
var (
	subjectNewsletter = wordRe(`weekly`, `monthly`, `digest`, `newsletter`, `roundup`, `round-up`, `this week`,
		`edition`, `issue #?\d+`, `product updates?`, `what's new`)
	subjectPromo = wordRe(`\d+% off`, `sale`, `deals?`, `offer`, `discount`, `last chance`, `limited time`, `free shipping`)
)

func wordRe(words ...string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b(` + strings.Join(words, "|") + `)\b`)
}

func classify(meta metadata, h headers) verdict {
	var v verdict
	v.headersSeen = h != (headers{})
	s := parseSender(meta.From)
	subject := strings.ToLower(meta.Subject)

	// Automated sender.
	for _, j := range automatedJoined {
		if strings.Contains(s.norm, j) {
			v.automated, v.automatedConf = true, 0.95
		}
	}
	if s.hasToken("postmaster") || s.norm == "mailerdaemon" {
		v.automated, v.automatedConf = true, 0.98
	}
	if !v.automated && s.hasToken(automatedTokens...) {
		v.automated, v.automatedConf = true, 0.9
	}
	precedence := strings.ToLower(strings.TrimSpace(h.Precedence))
	if auto := strings.ToLower(strings.TrimSpace(h.AutoSubmitted)); auto != "" && auto != "no" {
		v.automated, v.automatedConf = true, math.Max(v.automatedConf, 0.95)
	}
	if precedence == "bulk" || precedence == "list" || precedence == "junk" || strings.TrimSpace(h.AutoResponseSuppress) != "" {
		v.automated, v.automatedConf = true, math.Max(v.automatedConf, 0.9)
	}

	// Transactional protection.
	for _, p := range protections {
		if p.subject.MatchString(subject) || (p.senders != nil && s.hasToken(p.senders...)) {
			v.protected = p.class
			break
		}
	}
	if v.protected == "" && (repoPrefix.MatchString(subject) || domainIn(s.domain, workDomains)) {
		v.protected = "work"
	}

	// Newsletter evidence.
	v.unsub = parseUnsubscribe(h.ListUnsubscribe, h.ListUnsubscribePost)
	conf := 0.0
	strong := func(points int, c float64, reason string) {
		v.score += points
		conf = math.Max(conf, c)
		v.reasons = append(v.reasons, reason)
	}
	// Each list header alone is enough; transactional protection still wins,
	// since receipts and GitHub notifications carry them too.
	if strings.TrimSpace(h.ListID) != "" {
		strong(newsletterThreshold, 0.98, "Sent through a mailing list (List-ID)")
	}
	if v.unsub.available {
		strong(newsletterThreshold, 0.98, "Offers a way to unsubscribe")
	}
	if precedence == "bulk" || precedence == "list" {
		strong(newsletterThreshold, 0.95, "Marked as bulk mail by the sender")
	}
	switch {
	case domainIn(s.domain, newsletterDomains):
		strong(4, 0.9, "Sent from a newsletter platform")
	case s.hasToken("newsletter", "newsletters", "digest") || s.displayHas("newsletter", "newsletters", "digest"):
		strong(4, 0.85, "The sender is a newsletter or digest address")
	case s.hasToken("marketing", "promotions", "promo", "promos", "offers", "deals") || s.displayHas("marketing", "promotions", "deals"):
		strong(3, 0.8, "The sender is a marketing or promotions address")
	case s.hasToken("news", "updates") || s.displayHas("news", "weekly"):
		strong(1, 0.7, "The sender looks like a news or updates address")
	}
	hints := len(subjectNewsletter.FindAllString(subject, -1)) + len(subjectPromo.FindAllString(subject, -1))
	if hints > 0 {
		v.score += min(hints, 2)
		v.reasons = append(v.reasons, "The subject reads like a digest or promotion")
	}

	if v.protected == "" && v.score >= newsletterThreshold {
		v.newsletter = true
		v.newsletterConf = conf
		if v.newsletterConf < 0.8 { // built from weaker signals together
			v.newsletterConf = 0.65 + 0.05*float64(min(v.score-newsletterThreshold, 3))
		}
	}

	// Sender value.
	switch {
	case v.newsletter:
		v.value, v.valueConf = valueLow, v.newsletterConf
	case v.protected == "security" && v.automated:
		v.value, v.valueConf = valueHigh, 0.8
	case v.automated && v.protected != "":
		v.value, v.valueConf = valueNormal, 0.8
	case v.automated:
		v.value, v.valueConf = valueNormal, 0.6
	}
	return v
}

func isWordRune(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' }

func domainIn(domain string, list []string) bool {
	for _, d := range list {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

// annotations are sparse: only keys with evidence, never explicit
// negatives. category=newsletter is a recommendation; TideMail's effective
// classification and the user's corrections decide what is shown.
func (v verdict) annotations() []annotation {
	out := []annotation{}
	add := func(key, value string, conf float64) {
		a := annotation{Key: key, Value: value}
		if conf > 0 {
			c := math.Round(conf*100) / 100
			a.Confidence = &c
		}
		out = append(out, a)
	}
	if v.newsletter {
		add(keyCategory, "newsletter", v.newsletterConf)
		add(keyNewsletter, "true", v.newsletterConf)
	}
	if v.automated {
		add(keyAutomated, "true", v.automatedConf)
	}
	if v.value != "" {
		add(keySenderValue, v.value, v.valueConf)
	}
	if v.unsub.available {
		add(keyUnsubAvail, "true", 0.98)
		add(keyUnsubMethod, v.unsub.method, 0.98)
		if v.unsub.oneClick {
			add(keyUnsubOneClik, "true", 0.98)
		}
	}
	return out
}
