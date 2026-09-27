package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Starting thresholds for turning Jev's answers into annotations. They are
// initial choices, not calibrated on real mail; revisit them with data.
const (
	// needsReplyThreshold and importanceThreshold apply to the Noul's
	// probability of yes.
	needsReplyThreshold = 0.72
	importanceThreshold = 0.72
	// urgencyThreshold applies to P(urgent) + P(critical) on the urgency
	// Score.
	urgencyThreshold = 0.6
	// categoryThreshold is the minimum probability of Jev's chosen category;
	// below it the local category (if any) is kept.
	categoryThreshold = 0.5
)

// smart classifies messages with the local rules and, when configured, Jev.
type smart struct {
	jev    jevClient
	apiKey string
	stderr io.Writer
}

// classify returns the annotations for one message. Jev failures never fail
// the call: the local result is returned instead and the reason goes to
// stderr (without the API key or any message content).
func (s smart) classify(ctx context.Context, meta MessageMetadata, opts options) []Annotation {
	msg := analyze(meta)
	local := localDecisions(msg)
	if !opts.jevUsable() {
		return local.annotations()
	}

	ask := map[string]bool{
		qNeedsReply: opts.AskNeedsReply,
		qUrgency:    opts.AskUrgency,
		qImportance: opts.AskImportance,
		qCategory:   opts.AskCategory,
	}
	_, _, strong := strongLocalCategory(msg)
	keepLocalCategory := opts.Mode == modeHybrid && strong && opts.LocalRulesFirst
	if keepLocalCategory {
		ask[qCategory] = false
		// Automated mail from a service the rules recognize for certain
		// (GitHub, a carrier, a social network, a security notice): the
		// local rules settle it, so Jev is not called at all.
		if msg.automated {
			return local.annotations()
		}
	}
	if !ask[qNeedsReply] && !ask[qUrgency] && !ask[qImportance] && !ask[qCategory] {
		return local.annotations()
	}

	state := jevState{
		From:            strings.TrimSpace(meta.From),
		Subject:         strings.TrimSpace(meta.Subject),
		HasAttachment:   meta.HasAttachment,
		IsReply:         msg.isReply,
		AutomatedSender: msg.automated,
		LocalCategory:   local.category,
	}
	answers, _, err := s.jev.decide(ctx, opts.APIKey, opts.Model, state, buildQuestions(ask))
	if err != nil {
		_, _ = fmt.Fprintf(s.stderr, "tidemail-plugin-smart %s: %s; using local rules\n", classifierVersion, err)
		return local.annotations()
	}
	return merge(local, answers, ask).annotations()
}

// merge overlays Jev's answers on the local decisions. For a question Jev
// answered, Jev decides (including deciding "no"); for anything not asked, the
// local rules stand.
func merge(local decisions, answers map[string]jevAnswer, ask map[string]bool) decisions {
	d := local
	if ask[qNeedsReply] {
		d.needsReply = aboveOrZero(*answers[qNeedsReply].Noul, needsReplyThreshold)
	}
	if ask[qImportance] {
		d.importance = aboveOrZero(*answers[qImportance].Noul, importanceThreshold)
	}
	if ask[qUrgency] {
		a := answers[qUrgency]
		d.urgency = aboveOrZero(a.Probabilities["2"]+a.Probabilities["3"], urgencyThreshold)
	}
	if ask[qCategory] {
		a := answers[qCategory]
		p := a.Probabilities[a.Choice]
		switch {
		case a.Choice == "none" && p >= categoryThreshold:
			d.category, d.categoryConf = "", 0
		case a.Choice != "none" && p >= categoryThreshold:
			d.category, d.categoryConf = a.Choice, p
		}
		// Otherwise Jev is unsure; the local category (if any) stays.
	}
	return d
}

func aboveOrZero(p, threshold float64) float64 {
	if p >= threshold {
		return min(p, 1)
	}
	return 0
}

// testResult is the plugin.test response data.
type testResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// testConnection checks the TypeSafe configuration with a tiny request. The
// message never contains the key.
func (s smart) testConnection(ctx context.Context, opts options) testResult {
	switch {
	case opts.Mode == modeLocal:
		return testResult{OK: true, Message: "Local mode: TypeSafe / Jev is not used"}
	case !opts.JevEnabled:
		return testResult{OK: true, Message: "TypeSafe / Jev is turned off; using local rules"}
	case opts.APIKey == "":
		return testResult{Message: "TypeSafe API key not set; using local rules"}
	}
	questions := map[string]jevQuestion{"ping": {Type: "noul", Instructions: "Is `subject` a connection test?"}}
	_, model, err := s.jev.decide(ctx, opts.APIKey, opts.Model, jevState{Subject: "TideMail connection test"}, questions)
	if err != nil {
		var je *jevError
		if errors.As(err, &je) {
			switch je.Status {
			case http.StatusUnauthorized, http.StatusForbidden:
				return testResult{Message: "TypeSafe rejected the API key"}
			case http.StatusUnprocessableEntity:
				return testResult{Message: "TypeSafe did not accept model " + opts.Model}
			}
		}
		return testResult{Message: "TypeSafe / Jev connection failed: " + err.Error()}
	}
	msg := "TypeSafe / Jev connection successful\nModel: " + opts.Model
	if model != "" && model != opts.Model {
		msg += " (" + model + ")"
	}
	return testResult{OK: true, Message: msg}
}
