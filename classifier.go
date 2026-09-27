package main

import (
	"math"
	"net/mail"
	"strings"
	"unicode"
)

// Annotation is one key/value this plugin attaches to a message. Keys and
// values are fixed strings from this file, so they always satisfy TideMail's
// annotation limits.
type Annotation struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// Annotation keys TideMail renders as badges.
const (
	keyNeedsReply = "needs_reply"
	keyUrgency    = "urgency"
	keyImportance = "importance"
	keyCategory   = "category"
)

// Confidence values are heuristic rule strength, not calibrated probabilities.

// Classify turns message metadata into annotations using fixed, local rules.
// It only emits positive signals (needs_reply=true, urgency=high,
// importance=high, one category); a key that does not apply is left out, and
// TideMail's replace-on-success storage clears it. The result is never nil.
func Classify(meta MessageMetadata) []Annotation {
	msg := analyze(meta)
	out := []Annotation{}
	if c := needsReply(msg); c > 0 {
		out = append(out, annotation(keyNeedsReply, "true", c))
	}
	if c := urgency(msg); c > 0 {
		out = append(out, annotation(keyUrgency, "high", c))
	}
	category, categoryConf := categorize(msg)
	if c := importance(msg, category); c > 0 {
		out = append(out, annotation(keyImportance, "high", c))
	}
	if category != "" {
		out = append(out, annotation(keyCategory, category, categoryConf))
	}
	return out
}

func annotation(key, value string, confidence float64) Annotation {
	return Annotation{Key: key, Value: value, Confidence: math.Round(confidence*100) / 100}
}

// message is the pre-processed view the rules work on.
type message struct {
	subject   string   // original subject, trimmed
	tokens    []string // lowercase words of the subject, Re:/Fwd: removed
	isReply   bool     // subject started with Re:
	question  bool     // subject ends with '?'
	local     string   // sender local part, lowercase
	domain    string   // sender domain, lowercase
	name      string   // sender display name, lowercase
	automated bool
}

func analyze(meta MessageMetadata) message {
	subject := strings.TrimSpace(meta.Subject)
	stripped, isReply := stripReplyPrefixes(subject)
	msg := message{
		subject:  subject,
		tokens:   tokenize(stripped),
		isReply:  isReply,
		question: strings.HasSuffix(subject, "?"),
	}
	msg.name, msg.local, msg.domain = parseSender(meta.From)
	msg.automated = isAutomatedSender(msg.name, msg.local)
	return msg
}

// stripReplyPrefixes removes any run of Re:/Fwd:/Fw: prefixes and reports
// whether one of them was a reply.
func stripReplyPrefixes(subject string) (string, bool) {
	isReply := false
	for {
		lower := strings.ToLower(subject)
		switch {
		case strings.HasPrefix(lower, "re:"):
			isReply = true
			subject = strings.TrimSpace(subject[3:])
		case strings.HasPrefix(lower, "fwd:"):
			subject = strings.TrimSpace(subject[4:])
		case strings.HasPrefix(lower, "fw:"):
			subject = strings.TrimSpace(subject[3:])
		default:
			return subject, isReply
		}
	}
}

// tokenize lowercases s and splits it into words of letters and digits, so
// phrase matching works on whole words ("ups" never matches "groups").
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// hasPhrase reports whether the words of phrase appear consecutively in
// tokens.
func hasPhrase(tokens []string, phrase string) bool {
	want := tokenize(phrase)
	if len(want) == 0 || len(want) > len(tokens) {
		return false
	}
	for i := 0; i+len(want) <= len(tokens); i++ {
		match := true
		for j, w := range want {
			if tokens[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func hasAnyPhrase(tokens []string, phrases ...string) bool {
	for _, p := range phrases {
		if hasPhrase(tokens, p) {
			return true
		}
	}
	return false
}

// parseSender splits a From header into lowercase display name, local part,
// and domain. Unparseable input falls back to the raw text as the name.
func parseSender(from string) (name, local, domain string) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", "", ""
	}
	addr, err := mail.ParseAddress(from)
	if err != nil {
		// Bare "user@host" or something malformed.
		if at := strings.LastIndex(from, "@"); at > 0 && !strings.ContainsAny(from, " <>") {
			return "", strings.ToLower(from[:at]), strings.ToLower(from[at+1:])
		}
		return strings.ToLower(from), "", ""
	}
	name = strings.ToLower(addr.Name)
	if at := strings.LastIndex(addr.Address, "@"); at > 0 {
		local, domain = strings.ToLower(addr.Address[:at]), strings.ToLower(addr.Address[at+1:])
	}
	return name, local, domain
}

// automatedLocalParts are sender local parts (or pieces of them) that usually
// mean a machine sent the message.
var automatedLocalParts = []string{
	"noreply", "no-reply", "no_reply",
	"donotreply", "do-not-reply", "do_not_reply",
	"notification", "notifications", "notify",
	"mailer-daemon", "postmaster", "bounce", "bounces",
	"alerts", "automated", "newsletter", "digest",
}

// isAutomatedSender recognizes likely machine senders. It only lowers the
// weight of reply heuristics; it is never treated as certain.
func isAutomatedSender(name, local string) bool {
	for _, part := range automatedLocalParts {
		if local == part || strings.HasPrefix(local, part+"+") || strings.HasPrefix(local, part+".") ||
			strings.HasPrefix(local, part+"-") || strings.Contains(local, part) && len(part) >= 7 {
			return true
		}
	}
	return strings.Contains(name, "[bot]") || strings.HasSuffix(name, " bot")
}

func domainIs(domain string, roots ...string) bool {
	for _, root := range roots {
		if domain == root || strings.HasSuffix(domain, "."+root) {
			return true
		}
	}
	return false
}

// ── needs_reply ──────────────────────────────────────────────────────────────

func needsReply(msg message) float64 {
	best := 0.0
	// Explicit requests for action count even from automated senders, at a
	// lower weight.
	if hasAnyPhrase(msg.tokens, "action required", "response required", "response needed", "reply needed") {
		if msg.automated {
			best = max(best, 0.7)
		} else {
			best = max(best, 0.9)
		}
	}
	if msg.automated {
		return best
	}
	if hasAnyPhrase(msg.tokens, "please confirm", "please review", "please respond", "please reply", "need your", "your thoughts", "can you", "could you", "would you", "are you able") {
		best = max(best, 0.8)
	}
	if hasAnyPhrase(msg.tokens, "let me know") {
		best = max(best, 0.75)
	}
	if msg.question {
		best = max(best, 0.75)
	}
	if msg.isReply {
		best = max(best, 0.6)
	}
	return best
}

// ── urgency ──────────────────────────────────────────────────────────────────

func urgency(msg message) float64 {
	t := msg.tokens
	switch {
	case hasAnyPhrase(t, "urgent", "critical", "emergency"):
		return 0.95
	case hasAnyPhrase(t, "asap", "immediately"):
		return 0.9
	case hasAnyPhrase(t, "time sensitive", "action required"):
		return 0.85
	case hasAnyPhrase(t, "deadline", "final notice", "expires today", "due today", "overdue"):
		return 0.8
	// "today" alone is routine ("arriving today"); it only counts next to a
	// deadline word.
	case hasPhrase(t, "today") && hasAnyPhrase(t, "due", "expires", "expiring", "by end of day", "eod", "respond", "reply"):
		return 0.7
	}
	return 0
}

// ── category ─────────────────────────────────────────────────────────────────

// categoryRule is one category check; rules run in categoryRules order and
// the first match wins.
type categoryRule struct {
	name  string
	match func(message) float64
}

// categoryRules is the precedence order: security, calendar, github, billing,
// receipt, shipping, support, newsletter, social, notification, personal.
var categoryRules = []categoryRule{
	{"security", matchSecurity},
	{"calendar", matchCalendar},
	{"github", matchGitHub},
	{"billing", matchBilling},
	{"receipt", matchReceipt},
	{"shipping", matchShipping},
	{"support", matchSupport},
	{"newsletter", matchNewsletter},
	{"social", matchSocial},
	{"notification", matchNotification},
	{"personal", matchPersonal},
}

func categorize(msg message) (string, float64) {
	for _, rule := range categoryRules {
		if c := rule.match(msg); c > 0 {
			return rule.name, c
		}
	}
	return "", 0
}

func matchSecurity(msg message) float64 {
	if hasAnyPhrase(msg.tokens,
		"security alert", "security notice", "new login", "new sign in", "new signin",
		"sign in attempt", "signin attempt", "login attempt", "suspicious activity",
		"unusual activity", "password changed", "password was changed", "password reset",
		"reset your password", "verification code", "security code", "2fa",
		"two factor", "new device") {
		return 0.9
	}
	return 0
}

func matchCalendar(msg message) float64 {
	t := msg.tokens
	if len(t) > 0 && (t[0] == "invitation" || t[0] == "invite") {
		return 0.95 // "Invitation: Standup @ ..." from calendar systems
	}
	if hasAnyPhrase(t, "invitation", "meeting", "appointment", "rescheduled", "reschedule",
		"calendar", "rsvp", "updated invitation", "event canceled", "event cancelled") {
		return 0.85
	}
	return 0
}

func matchGitHub(msg message) float64 {
	switch {
	case domainIs(msg.domain, "github.com"):
		return 0.98
	case hasPhrase(msg.tokens, "github"):
		return 0.9
	case hasAnyPhrase(msg.tokens, "pull request", "workflow run"):
		return 0.7
	}
	return 0
}

func matchBilling(msg message) float64 {
	if hasAnyPhrase(msg.tokens, "invoice", "payment due", "past due", "amount due", "overdue",
		"billing", "statement", "payment failed", "payment declined", "card declined") {
		return 0.85
	}
	return 0
}

func matchReceipt(msg message) float64 {
	if hasAnyPhrase(msg.tokens, "receipt", "order confirmation", "order confirmed",
		"payment received", "thank you for your purchase", "thanks for your purchase",
		"thank you for your order", "thanks for your order") {
		return 0.85
	}
	return 0
}

func matchShipping(msg message) float64 {
	if domainIs(msg.domain, "ups.com", "fedex.com", "usps.com", "dhl.com") {
		return 0.9
	}
	if hasAnyPhrase(msg.tokens, "shipped", "has shipped", "out for delivery", "tracking number",
		"delivery update", "on its way", "delivered", "shipment", "ups", "fedex", "usps", "dhl") {
		return 0.85
	}
	return 0
}

func matchSupport(msg message) float64 {
	t := msg.tokens
	if hasAnyPhrase(t, "support request", "support ticket", "support case", "help desk", "helpdesk", "incident") {
		return 0.85
	}
	// "case"/"ticket" only count with a number next to them ("Case #12345"),
	// so "in case you missed it" and "concert tickets" stay out.
	for i, tok := range t {
		if (tok == "case" || tok == "ticket") && i+1 < len(t) && isNumber(t[i+1]) {
			return 0.8
		}
	}
	if msg.local == "support" || msg.local == "help" || strings.HasPrefix(msg.local, "support+") {
		return 0.75
	}
	return 0
}

func isNumber(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

func matchNewsletter(msg message) float64 {
	switch {
	case strings.Contains(msg.local, "newsletter") || strings.Contains(msg.local, "digest"):
		return 0.9
	case hasAnyPhrase(msg.tokens, "newsletter", "digest", "weekly update", "weekly roundup", "monthly update", "this week in"):
		return 0.85
	case msg.local == "news" || strings.HasPrefix(msg.local, "news+") || strings.HasPrefix(msg.local, "news."):
		return 0.75
	}
	return 0
}

func matchSocial(msg message) float64 {
	if domainIs(msg.domain, "facebookmail.com", "linkedin.com", "twitter.com", "x.com",
		"instagram.com", "redditmail.com", "reddit.com", "mastodon.social", "bsky.app", "threads.net") {
		return 0.85
	}
	if hasAnyPhrase(msg.tokens, "mentioned you", "tagged you", "new follower", "followed you",
		"friend request", "commented on your", "replied to your post") {
		return 0.75
	}
	return 0
}

// matchNotification is the fallback for clearly automated mail.
func matchNotification(msg message) float64 {
	if msg.automated {
		return 0.65
	}
	return 0
}

// matchPersonal is deliberately narrow: a human sender at a consumer mail
// provider. Company addresses stay uncategorized rather than guessed.
func matchPersonal(msg message) float64 {
	if !msg.automated && domainIs(msg.domain,
		"gmail.com", "googlemail.com", "outlook.com", "hotmail.com", "live.com",
		"yahoo.com", "icloud.com", "me.com", "proton.me", "protonmail.com", "fastmail.com", "hey.com") {
		return 0.6
	}
	return 0
}

// ── importance ───────────────────────────────────────────────────────────────

func importance(msg message, category string) float64 {
	t := msg.tokens
	best := 0.0
	if category == "security" {
		best = max(best, 0.85)
	}
	if hasAnyPhrase(t, "action required", "account suspended", "account locked",
		"account will be", "account has been", "final notice") {
		best = max(best, 0.85)
	}
	if category == "billing" && hasAnyPhrase(t, "due", "overdue", "past due", "failed", "declined") {
		best = max(best, 0.8)
	}
	if category == "calendar" && hasAnyPhrase(t, "rescheduled", "canceled", "cancelled", "updated invitation", "moved") {
		best = max(best, 0.75)
	}
	if category == "support" && (msg.isReply || hasAnyPhrase(t, "response", "replied", "update on your", "resolved")) {
		best = max(best, 0.7)
	}
	// A direct, explicit request from a person.
	if !msg.automated && hasAnyPhrase(t, "please confirm", "please review", "please respond", "need your") {
		best = max(best, 0.7)
	}
	return best
}
