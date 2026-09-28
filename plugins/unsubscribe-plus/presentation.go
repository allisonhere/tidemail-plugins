package main

import "strings"

// presentation is what people see in TideMail's result card. Annotations
// stay machine-readable; this explains them in plain words. It carries no
// colors, layout, links, or escapes: TideMail draws it.
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

// transactionalTypes name the protected classes for people.
var transactionalTypes = map[string]string{
	"security": "Security alert",
	"billing":  "Billing or receipt",
	"shipping": "Shipping update",
	"support":  "Support conversation",
	"calendar": "Calendar",
	"work":     "Work notification",
}

func confidenceWord(c float64) string {
	switch {
	case c >= 0.85:
		return "high"
	case c >= 0.65:
		return "medium"
	}
	return "low"
}

// presentation explains a verdict. Reasons are capped at TideMail's limit.
func (v verdict) presentation() presentation {
	switch {
	case v.newsletter:
		p := presentation{Title: "This looks like a newsletter", Status: "info", Confidence: confidenceWord(v.newsletterConf),
			Summary: "List or marketing mail with low expected priority."}
		if v.automated {
			p.Summary = "Automated list mail with low expected priority."
		}
		p.Facts = []fact{{"Type", "Newsletter"}, {"Priority", "Low"}, {"Unsubscribe", v.unsubscribeText()}}
		if v.unsub.available {
			p.Facts = append(p.Facts, fact{"Method", methodText(v.unsub)})
		}
		p.Reasons = append([]string(nil), v.reasons...)
		if v.automated {
			p.Reasons = append(p.Reasons, "The sender appears automated")
		}
		p.Reasons = p.Reasons[:min(len(p.Reasons), 6)]
		return p
	case v.automated:
		p := presentation{Title: "Automated message", Status: "neutral", Confidence: confidenceWord(v.automatedConf),
			Summary: "Sent by an automated system, not a newsletter."}
		kind := transactionalTypes[v.protected]
		if kind == "" {
			kind = "Automated notification"
		}
		priority := "Normal"
		if v.value == valueHigh {
			priority = "High"
			p.Status = "warning"
			p.Summary = "Automated security mail. Worth reading."
		}
		p.Facts = []fact{{"Type", kind}, {"Priority", priority}, {"Newsletter", "No"}}
		p.Reasons = []string{"The sender address is an automated one (no-reply, notifications, alerts…)"}
		if v.protected != "" {
			p.Reasons = append(p.Reasons, "It reads like "+articled(kind)+", so it is never treated as low priority")
		}
		return p
	}
	p := presentation{Title: "Not a newsletter", Status: "neutral",
		Summary: "No newsletter, mailing-list, or automated-sender signals were found."}
	if v.protected != "" {
		p.Facts = []fact{{"Type", transactionalTypes[v.protected]}}
	}
	return p
}

func (v verdict) unsubscribeText() string {
	switch {
	case v.unsub.available:
		return "Available"
	case v.headersSeen:
		return "Not found"
	}
	// TideMail's Plugin API v1 does not share list headers yet.
	return "Not checked"
}

func methodText(u unsubscribe) string {
	switch {
	case u.oneClick:
		return "One-click secure link"
	case u.method == "https":
		return "Secure web link"
	case u.method == "http":
		return "Web link"
	}
	return "Email"
}

func articled(s string) string {
	if s == "" {
		return "a notice"
	}
	switch s[0] {
	case 'A', 'E', 'I', 'O', 'U':
		return "an " + lowerFirst(s)
	}
	return "a " + lowerFirst(s)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
