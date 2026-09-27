package main

import "os"

// classifierVersion identifies the rules and Jev questions. Bump it when either
// changes meaning. It is used in diagnostics and tests, never as an annotation.
const classifierVersion = "smart-v2"

// Modes.
const (
	modeLocal  = "local"  // deterministic rules only; never touches the network
	modeHybrid = "hybrid" // local rules for certain cases, Jev for the rest
	modeJev    = "jev"    // Jev for every enabled decision, local as fallback
)

// Jev models the settings allow.
const (
	modelLatest = "jev-latest"
	modelPinned = "jev-1.13.0"
)

// apiKeyEnv is where TideMail puts the jev_api_key secret for this process.
const apiKeyEnv = "TIDEMAIL_SECRET_JEV_API_KEY"

// options are the resolved plugin settings.
type options struct {
	Mode            string
	JevEnabled      bool
	Model           string
	LocalRulesFirst bool
	AskNeedsReply   bool
	AskUrgency      bool
	AskImportance   bool
	AskCategory     bool
	APIKey          string
}

func defaultOptions() options {
	return options{
		Mode: modeHybrid, JevEnabled: true, Model: modelLatest, LocalRulesFirst: true,
		AskNeedsReply: true, AskUrgency: true, AskImportance: true, AskCategory: true,
	}
}

// parseOptions reads TideMail's settings object. TideMail already resolves
// defaults, but anything missing or malformed still falls back safely here.
func parseOptions(settings map[string]any, apiKey string) options {
	o := defaultOptions()
	o.APIKey = apiKey
	str := func(key string, allowed ...string) (string, bool) {
		v, ok := settings[key].(string)
		if !ok {
			return "", false
		}
		for _, a := range allowed {
			if v == a {
				return v, true
			}
		}
		return "", false
	}
	boolean := func(key string, into *bool) {
		if v, ok := settings[key].(bool); ok {
			*into = v
		}
	}
	if v, ok := str("mode", modeLocal, modeHybrid, modeJev); ok {
		o.Mode = v
	}
	if v, ok := str("jev_model", modelLatest, modelPinned); ok {
		o.Model = v
	}
	boolean("jev_enabled", &o.JevEnabled)
	boolean("local_rules_first", &o.LocalRulesFirst)
	boolean("jev_needs_reply", &o.AskNeedsReply)
	boolean("jev_urgency", &o.AskUrgency)
	boolean("jev_importance", &o.AskImportance)
	boolean("jev_category", &o.AskCategory)
	return o
}

// jevUsable reports whether any Jev call may happen at all.
func (o options) jevUsable() bool {
	return o.Mode != modeLocal && o.JevEnabled && o.APIKey != "" &&
		(o.AskNeedsReply || o.AskUrgency || o.AskImportance || o.AskCategory)
}

func apiKeyFromEnv() string { return os.Getenv(apiKeyEnv) }
