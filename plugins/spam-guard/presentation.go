package main

import (
	"fmt"
	"strings"
	"unicode"
)

// presentation is what people see in TideMail's result card. Annotations stay
// machine-readable; this explains them in plain words. It carries no colors,
// layout, links, or escapes: TideMail draws it.
type presentation struct {
	Title      string   `json:"title"`
	Summary    string   `json:"summary,omitempty"`
	Status     string   `json:"status,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
	Facts      []fact   `json:"facts,omitempty"`
	Reasons    []string `json:"reasons,omitempty"`
}

type fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// TideMail's limits for a card (docs/plugins/presentation.md).
const (
	maxFacts      = 8
	maxFactValue  = 160
	maxReasons    = 6
	maxReasonText = 180
)

func confidenceWord(score int) string {
	switch {
	case score >= 85:
		return "high"
	case score >= 60:
		return "medium"
	}
	return "low"
}

// clean makes untrusted header text safe for a card: no control, format
// (bidi, zero-width), or separator characters, and a length cap.
func clean(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r):
			continue
		}
		b.WriteRune(r)
	}
	return clip(strings.Join(strings.Fields(b.String()), " "), max)
}

// presentation explains a verdict. A message that trips nothing still gets a
// card, so running the plugin by hand always answers.
func (v verdict) presentation(meta metadata) *presentation {
	if v.level == "" {
		return &presentation{
			Title:   "Nothing suspicious found",
			Status:  "neutral",
			Summary: "None of the checks tripped. That is not a guarantee: be careful with unexpected requests for passwords, payments, or gift cards.",
		}
	}
	p := &presentation{Confidence: confidenceWord(v.score)}
	if v.level == "high" {
		p.Title, p.Status = "Likely phishing or a scam", "danger"
	} else {
		p.Title, p.Status = "This message looks suspicious", "warning"
	}
	p.Summary = "Do not click links, open attachments, or reply with passwords or payments. If you think it could be real, go to the company's own website or app yourself."

	from := parseParty(meta.From)
	p.Facts = append(p.Facts, fact{"Sender", clean(from.addr, maxFactValue)})
	if v.brand != "" {
		p.Facts = append(p.Facts, fact{"Claims to be", clean(v.brand, maxFactValue)})
	}
	if rt := parseParty(meta.ReplyTo); rt.addr != "" && rt.apex != from.apex {
		p.Facts = append(p.Facts, fact{"Replies go to", clean(rt.addr, maxFactValue)})
	}
	if a := meta.Auth; a != nil && (a.SPF != "" || a.DKIM != "" || a.DMARC != "") {
		var parts []string
		for _, c := range []struct{ name, val string }{{"SPF", a.SPF}, {"DKIM", a.DKIM}, {"DMARC", a.DMARC}} {
			if c.val != "" {
				parts = append(parts, c.name+" "+c.val)
			}
		}
		p.Facts = append(p.Facts, fact{"Sender checks", clean(strings.Join(parts, " · ")+" (as your mail server reported)", maxFactValue)})
	}
	p.Facts = p.Facts[:min(len(p.Facts), maxFacts)]

	for _, s := range v.signals {
		if len(p.Reasons) == maxReasons {
			break
		}
		p.Reasons = append(p.Reasons, clean(s.reason, maxReasonText))
	}
	if more := len(v.signals) - len(p.Reasons); more > 0 {
		p.Reasons[len(p.Reasons)-1] = clean(fmt.Sprintf("%s (and %d more)", p.Reasons[len(p.Reasons)-1], more), maxReasonText)
	}
	return p
}
