package main

import (
	"fmt"
	"math"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// sensitivity is the score a message needs before it is flagged.
type sensitivity struct{ medium, high int }

func sensitivityFor(name string) sensitivity {
	switch name {
	case "low":
		return sensitivity{medium: 65, high: 95}
	case "high":
		return sensitivity{medium: 35, high: 65}
	}
	return sensitivity{medium: 50, high: 80}
}

// signal is one piece of evidence: how strongly it points to a scam, and what
// to tell the reader.
type signal struct {
	points int
	reason string
}

type verdict struct {
	score   int
	level   string // "", "medium" or "high"
	signals []signal
	brand   string // the company the message claims to be, if any
}

// party is a parsed sender or Reply-To address.
type party struct {
	display string
	addr    string
	local   string
	domain  string
	apex    string
}

// angleAddr finds "<addr>" in a header that is not a valid RFC 5322 address,
// such as an unquoted display name that itself contains an @.
var angleAddr = regexp.MustCompile(`^(.*)<\s*([^<>\s]+@[^<>\s]+)\s*>\s*$`)

func parseParty(s string) party {
	var p party
	addr := strings.TrimSpace(s)
	if a, err := mail.ParseAddress(s); err == nil {
		p.display, addr = a.Name, a.Address
	} else if m := angleAddr.FindStringSubmatch(addr); m != nil {
		p.display, addr = strings.Trim(strings.TrimSpace(m[1]), `"`), m[2]
	}
	addr = strings.ToLower(addr)
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at == len(addr)-1 {
		return p
	}
	p.addr, p.local, p.domain = addr, addr[:at], strings.Trim(addr[at+1:], ".")
	p.apex = apex(p.domain)
	return p
}

var (
	// addressInName finds an email address or a web address inside a display
	// name, which has no business containing one.
	addressInName = regexp.MustCompile(`(?i)([a-z0-9._%+-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)+|\b[a-z0-9-]+(?:\.[a-z0-9-]+)*\.(?:com|net|org|gov|edu|io|co|us|uk|de|ru|info|biz|app)\b)`)

	// Subject wording that pressures or tempts. Each group counts once.
	subjectGroups = []struct {
		name string
		re   *regexp.Regexp
	}{
		{"an urgent account warning", regexp.MustCompile(`(?i)\b(account|access|card|wallet|mailbox)\b.{0,30}\b(suspend(ed)?|lock(ed)?|limit(ed)?|compromis(ed)?|disabled|deactivat(ed|ion)|terminat(ed|ion)|restrict(ed)?|on hold)\b|\b(suspend(ed)?|lock(ed)?|limit(ed)?|disabled)\b.{0,20}\b(account|access)\b|\b(blocked|banned)\b.{0,25}\b(account|profile|access|photos|videos|files|card)\b|\b(account|profile)\b.{0,25}\b(has been|is|was|been)\b.{0,12}\b(blocked|banned)\b|(memories|photos|files|data|storage|account|device|computer) (is|are) (at risk|full|almost full|infected)|storage (is )?(full|almost full)|renew (now|today|before|in \d)|unusual (sign-?in|activity|login)|suspicious (sign-?in|activity|login)|security alert.{0,20}(action|verify)|final notice|immediate action|action required`)},
		{"a request to verify or confirm something", regexp.MustCompile(`(?i)\b(verify|confirm|validate|re-?activate|update|restore|unlock)\b.{0,25}\b(your )?(account|identity|email|password|payment|billing|details|information|wallet)\b|password.{0,15}(expire[sd]?|reset required)|\\b(confirmation|verification)\\b.{0,12}\\b(required|needed|pending)\\b|security (check|confirmation|verification)`)},
		{"a prize or money offer", regexp.MustCompile(`(?i)\b(you('ve| have)? won|winner|congratulations.{0,30}(won|selected|chosen)|lottery|inheritance|unclaimed (funds|package)|claim your (prize|reward|gift)|refund (pending|available)|pending (payment|deposit))\b`)},
		{"a gift card, crypto, or wire request", regexp.MustCompile(`(?i)\b(gift ?cards?|bitcoin|crypto(currency)?|wire transfer|western union|moneygram|itunes cards?)\b`)},
	}

	// businessName is a role a company would use in a sender name; from a free
	// email account it is a mild warning on its own.
	businessName = regexp.MustCompile(`(?i)\b(accounts?|accounting|billing|invoices?|payments?|payroll|support|help ?desk|service desk|customer service|security|admin(istrator)?|finance|claims?|refunds?|orders?|delivery|shipping|verification)\b`)

	// A bare "pictures" subject: a long-running malware and spam lure.
	pictureLure = regexp.MustCompile(`(?i)^\s*(\(\d+\)|\d+\s*(new )?)?\s*(pictures?|pics?|photos?)\b\s*(\d{4}[/-]\d+[/-]\d+|\(?\d+\)?)?\s*$|^\s*pics?\s+\d{4}`)

	invoiceWords = regexp.MustCompile(`(?i)\b(invoice|payment|remittance|receipt|statement|purchase order|overdue|past due)\b`)
)

func classify(meta metadata, sens sensitivity) verdict {
	from := parseParty(meta.From)
	var v verdict
	if from.domain == "" {
		return v // nothing to judge without a sender address
	}
	add := func(points int, format string, args ...any) {
		v.signals = append(v.signals, signal{points, fmt.Sprintf(format, args...)})
	}

	claimed := claimedBrand(from)
	identity := false // some signal says the sender is pretending to be someone else

	// 1. The name says one company, the address says another.
	if claimed != nil && !claimed.owns(from.domain) {
		v.brand = claimed.name
		identity = true
		add(45, "The name says %s, but the message was sent from %s, which is not one of its addresses", claimed.name, from.domain)
		if freeMail[from.apex] {
			add(15, "%s does not write to customers from a free email address like %s", claimed.name, from.apex)
		}
	}

	// 2. The name itself contains a different address or website.
	if m := addressInName.FindString(from.display); m != "" {
		shown := strings.ToLower(m)
		shownDomain := shown
		if at := strings.LastIndexByte(shown, '@'); at >= 0 {
			shownDomain = shown[at+1:]
		}
		if apex(shownDomain) != from.apex {
			identity = true
			add(50, "The sender's name shows an address or website (%s) that is not the real sender (%s)", clip(shown, 40), from.domain)
		}
	}

	// 3. The sender's domain imitates a well-known one.
	if pts, text, name := lookalike(from, claimed); pts > 0 {
		identity = true
		add(pts, "%s", text)
		if v.brand == "" {
			v.brand = name
		}
	}
	if strings.Contains(from.domain, "xn--") {
		identity = true
		add(30, "The sender's domain uses international characters, which can imitate ordinary letters")
	} else if mixedScript(from.domain) {
		identity = true
		add(40, "The sender's domain mixes alphabets (for example Latin and Cyrillic letters), a common trick")
	}
	if riskyTLDs[tld(from.domain)] {
		add(10, "The sender's domain ends in .%s, which scammers use often", tld(from.domain))
	}

	// Tells of mass-produced scam mail.
	if stylized(from.display) {
		add(30, "The sender's name uses unusual lettering, a trick to slip past spam filters")
	}
	if mostlyDigits(from.local) {
		add(20, "The sender's address is mostly digits, like a machine-made account")
	}
	if l := label(from.domain); len(from.local) >= 6 && from.local == l {
		add(20, "The address name is the same as the domain, like a machine-made account")
	}
	if to := parseParty(firstAddress(meta.To)); to.local != "" && len(to.local) >= 5 && from.apex != to.apex &&
		normalize(from.display) == normalize(to.local) {
		add(20, "The sender's name is your own username, but the message comes from somewhere else")
	}
	if randomLooking(from.domain) {
		add(35, "The sender's domain looks randomly generated")
	}
	if b := brandInText(meta.Subject); b != nil && !b.owns(from.domain) && freeMail[from.apex] {
		if v.brand == "" {
			v.brand = b.name
		}
		add(30, "The subject is about %s, but the message comes from a free email address", b.name)
	}
	if to := parseParty(firstAddress(meta.To)); len(to.local) >= 5 && strings.HasPrefix(normalize(meta.Subject), normalize(to.local)) {
		add(15, "The subject starts with your username, a mass-mailing habit")
	}
	if warningSymbol(meta.Subject) {
		add(10, "The subject starts with a warning symbol")
	}

	// 4. Replies go somewhere else.
	if rt := parseParty(meta.ReplyTo); rt.domain != "" && rt.apex != from.apex {
		add(15, "Replies would go to %s, not to the sender's own domain", rt.domain)
		if freeMail[rt.apex] && !freeMail[from.apex] {
			add(10, "The reply address is a free email account")
		}
	}

	// 5. What the user's mail server recorded about the sender.
	authPoints := 0
	if a := meta.Auth; a != nil {
		var failed []string
		if a.DMARC == "fail" {
			authPoints += 25
			failed = append(failed, "DMARC")
		}
		switch a.SPF {
		case "fail":
			authPoints += 15
			failed = append(failed, "SPF")
		case "softfail":
			authPoints += 8
			failed = append(failed, "SPF (soft)")
		}
		if a.DKIM == "fail" {
			authPoints += 10
			failed = append(failed, "DKIM")
		}
		// Forwarding and mailing lists break these checks on honest mail, so on
		// their own they only count for so much.
		if !identity {
			authPoints = min(authPoints, 20)
		}
		if authPoints > 0 {
			add(authPoints, "Your mail server could not confirm the sender (%s failed)", strings.Join(failed, ", "))
		}
		if a.SpamFlag {
			add(25, "Your mail server's own spam filter marked this message")
		}
	}

	// 6. Pressure and bait in the subject.
	var wording []string
	for _, g := range subjectGroups {
		if g.re.MatchString(meta.Subject) {
			wording = append(wording, g.name)
		}
	}
	switch len(wording) {
	case 0:
	case 1:
		add(20, "The subject reads like %s", wording[0])
	default:
		add(30, "The subject mixes %s and %s", wording[0], wording[1])
	}

	if pictureLure.MatchString(meta.Subject) {
		add(30, "The subject is just a bare 'pictures' line, a long-running malware lure")
		if nameUnrelated(from) {
			add(20, "The sender's name has nothing to do with the address it came from")
		}
	}

	// Bait or a business role, from an address anyone can sign up for.
	if freeMail[from.apex] {
		for _, w := range wording {
			if w == "a prize or money offer" || w == "a gift card, crypto, or wire request" {
				add(15, "A prize or payment request from a free email address")
				break
			}
		}
		if businessName.MatchString(from.display) {
			add(15, "A business-sounding name on a free email account")
		}
	}

	// A warning about a company's account from someone who is not that company.
	if b := brandInText(meta.Subject); b != nil && !b.owns(from.domain) && v.brand == "" && len(wording) > 0 &&
		(wording[0] == "an urgent account warning" || wording[0] == "a request to verify or confirm something") {
		v.brand = b.name
		add(25, "The subject warns about your %s account, but the message is not from %s", b.name, b.name)
	}

	// 7. An "invoice" attached to mail from a free account.
	if meta.HasAttachment && freeMail[from.apex] && invoiceWords.MatchString(meta.Subject) {
		add(25, "A payment or invoice attachment from a free email account")
	}

	sort.SliceStable(v.signals, func(i, j int) bool { return v.signals[i].points > v.signals[j].points })
	for _, s := range v.signals {
		v.score += s.points
	}
	v.score = min(v.score, 100)
	switch {
	case v.score >= sens.high:
		v.level = "high"
	case v.score >= sens.medium:
		v.level = "medium"
	}
	return v
}

// nameUnrelated reports a personal-looking display name (two or more words)
// that shares nothing with the address it was sent from, on a domain that is
// not a free email service (people pick odd addresses there). Nicknames count
// as related: the first three letters of any word are enough.
func nameUnrelated(p party) bool {
	words := strings.Fields(normalize(p.display))
	if len(words) < 2 || freeMail[p.apex] {
		return false
	}
	haystack := p.local + " " + p.domain
	for _, w := range words {
		if len(w) >= 3 && strings.Contains(haystack, w[:3]) {
			return false
		}
	}
	return true
}

// stylized spots display names written in "mathematical" look-alike letters
// (𝐂𝐥𝐨𝐮𝐝, 𝗣𝗮𝘆𝗽𝗮𝗹), which scammers use to get past word-based filters.
func stylized(s string) bool {
	for _, r := range s {
		if r >= 0x1D400 && r <= 0x1D7FF {
			return true
		}
	}
	return false
}

// mostlyDigits reports an address like 162219657@… that no person chose.
func mostlyDigits(local string) bool {
	digits := 0
	for _, r := range local {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 6 && digits*10 >= len(local)*7
}

// randomLooking reports a domain label that no one would pick: long with
// almost no vowels, a long run of consonants, or letters and digits mixed
// like 0xablz2a.
func randomLooking(domain string) bool {
	labels := strings.Split(domain, ".")
	if len(labels) > 1 {
		labels = labels[:len(labels)-1] // the top-level domain is not a name
	}
	for _, l := range labels {
		if len(l) < 7 || strings.HasPrefix(l, "xn--") {
			continue
		}
		vowels, run, longest, digits, letters := 0, 0, 0, 0, 0
		for _, r := range l {
			switch {
			case r >= '0' && r <= '9':
				digits++
				run = 0
			case strings.ContainsRune("aeiouy", r):
				vowels++
				letters++
				run = 0
			case r >= 'a' && r <= 'z':
				letters++
				run++
				longest = max(longest, run)
			default:
				run = 0
			}
		}
		switch {
		case letters >= 7 && vowels*5 < letters: // under 20% vowels
			return true
		case longest >= 6:
			return true
		case digits >= 2 && letters >= 3 && len(l) >= 7 && !strings.ContainsRune(l, '-'):
			return true
		}
	}
	return false
}

// brandInText finds a company named in free text (a subject), by whole word.
func brandInText(s string) *brand {
	text := " " + normalize(s) + " "
	for i := range brands {
		for _, kw := range brands[i].keywords {
			if strings.Contains(text, " "+normalize(kw)+" ") {
				return &brands[i]
			}
		}
	}
	return nil
}

// firstAddress is the first address in a To header.
func firstAddress(to string) string {
	if list, err := mail.ParseAddressList(to); err == nil && len(list) > 0 {
		return list[0].Address
	}
	return strings.TrimSpace(strings.SplitN(to, ",", 2)[0])
}

func warningSymbol(subject string) bool {
	for _, r := range subject {
		return strings.ContainsRune("⛔🚫⚠❗🔴🛑❌", r)
	}
	return false
}

// claimedBrand finds the company a sender's name (or the start of its
// address) says it is.
func claimedBrand(p party) *brand {
	name := " " + normalize(p.display) + " "
	local := " " + normalize(p.local) + " "
	for i := range brands {
		for _, kw := range brands[i].keywords {
			needle := " " + normalize(kw) + " "
			if strings.Contains(name, needle) {
				return &brands[i]
			}
			// "paypal-support@…": a one-word brand at the front of the address.
			if !strings.Contains(kw, " ") && strings.HasPrefix(local, needle) {
				return &brands[i]
			}
		}
	}
	return nil
}

// normalize lowers a name and turns everything but letters and digits into
// single spaces, so keywords match as whole words.
func normalize(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// lookalike reports a sender domain that imitates a well-known company's. It
// returns the strongest finding only.
func lookalike(from party, claimed *brand) (points int, text, brandName string) {
	senderLabel := label(from.apex)
	if len(senderLabel) < 4 {
		return 0, "", ""
	}
	best := func(p int, t, name string) {
		if p > points {
			points, text, brandName = p, t, name
		}
	}
	for i := range brands {
		b := &brands[i]
		if b.owns(from.domain) {
			continue
		}
		for l := range b.labels() {
			switch {
			case senderLabel == l:
				best(35, fmt.Sprintf("%s has the same name as %s but is not one of its domains", from.apex, b.name), b.name)
			case len(l) >= 5 && (fold(senderLabel) == l || distance(senderLabel, l, 1) <= 1):
				best(55, fmt.Sprintf("%s looks like %s's address but is spelled differently", from.apex, b.name), b.name)
			case len(l) >= 5 && strings.Contains(senderLabel, l) && b != claimed:
				// (Skipped when the name already claims this brand: that signal
				// covers it, and counting both flagged genuine mail from a
				// company's own extra domains, such as amazonmusic.com.)
				best(45, fmt.Sprintf("%s borrows %s's name but is not one of its domains", from.apex, b.name), b.name)
			}
			// The company's name used as a subdomain of someone else's domain.
			if len(l) >= 5 {
				for _, sub := range strings.Split(strings.TrimSuffix(from.domain, "."+from.apex), ".") {
					if sub == l && sub != from.domain {
						best(50, fmt.Sprintf("%s uses %s's name inside someone else's domain (%s)", from.domain, b.name, from.apex), b.name)
					}
				}
			}
		}
	}
	return points, text, brandName
}

// mixedScript reports a domain whose letters come from more than one alphabet.
func mixedScript(domain string) bool {
	var latin, other bool
	for _, r := range domain {
		switch {
		case r < 0x80:
			if unicode.IsLetter(r) {
				latin = true
			}
		case unicode.Is(unicode.Cyrillic, r), unicode.Is(unicode.Greek, r), unicode.Is(unicode.Armenian, r):
			other = true
		}
	}
	return latin && other
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// confidence is a strength, not a probability.
func (v verdict) confidence() float64 {
	return math.Round(math.Min(0.95, 0.4+float64(v.score)/200)*100) / 100
}

const (
	keyCategory = "category"
	keyRisk     = "risk"
	keyClaims   = "claims_brand"
	keySignals  = "risk_signals"
)

// annotations are positive findings only: an unflagged message gets an empty
// set, which also clears an earlier warning on reclassification.
func (v verdict) annotations() []annotation {
	out := []annotation{}
	if v.level == "" {
		return out
	}
	c := v.confidence()
	add := func(key, value string) { out = append(out, annotation{Key: key, Value: value, Confidence: &c}) }
	add(keyCategory, "suspicious")
	add(keyRisk, v.level)
	if v.brand != "" {
		add(keyClaims, strings.ToLower(normalize(v.brand)))
	}
	add(keySignals, fmt.Sprint(len(v.signals)))
	return out
}
