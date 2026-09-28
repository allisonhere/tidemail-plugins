package main

import (
	"net/mail"
	"net/url"
	"strings"
)

// unsubscribe describes how a message can be unsubscribed from, parsed from
// List-Unsubscribe (RFC 2369) and List-Unsubscribe-Post (RFC 8058).
//
// It is description only. Parsing never opens, resolves, or contacts
// anything; the link itself is not returned, so annotations never carry
// long URLs, and a future TideMail action can re-read the header, show the
// destination, and ask for explicit confirmation before doing anything.
type unsubscribe struct {
	available bool
	method    string // "https", "http", or "mailto"
	oneClick  bool
}

// methodRank prefers HTTPS, then HTTP, then mailto.
var methodRank = map[string]int{"https": 3, "http": 2, "mailto": 1}

// parseUnsubscribe reads List-Unsubscribe's <…> entries and keeps the best
// valid one. Malformed entries are ignored.
func parseUnsubscribe(listUnsubscribe, listUnsubscribePost string) unsubscribe {
	var best unsubscribe
	for _, entry := range angleEntries(listUnsubscribe) {
		method, ok := validTarget(entry)
		if ok && methodRank[method] > methodRank[best.method] {
			best = unsubscribe{available: true, method: method}
		}
	}
	// One-click needs the POST header and an HTTPS target (RFC 8058).
	if best.method == "https" && strings.EqualFold(strings.Join(strings.Fields(listUnsubscribePost), ""), "List-Unsubscribe=One-Click") {
		best.oneClick = true
	}
	return best
}

// angleEntries returns the contents of each <…> in a header value.
func angleEntries(h string) []string {
	var out []string
	for {
		start := strings.IndexByte(h, '<')
		if start < 0 {
			return out
		}
		end := strings.IndexByte(h[start+1:], '>')
		if end < 0 {
			return out
		}
		out = append(out, strings.TrimSpace(h[start+1:start+1+end]))
		h = h[start+1+end+1:]
	}
}

// validTarget checks one entry's shape without contacting it.
func validTarget(raw string) (string, bool) {
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, " \t\r\n") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch scheme := strings.ToLower(u.Scheme); scheme {
	case "https", "http":
		if u.Hostname() == "" || u.User != nil {
			return "", false
		}
		return scheme, true
	case "mailto":
		addr := u.Opaque
		if i := strings.IndexByte(addr, '?'); i >= 0 {
			addr = addr[:i]
		}
		if unescaped, err := url.PathUnescape(addr); err == nil {
			addr = unescaped
		}
		if _, err := mail.ParseAddress(addr); err != nil {
			return "", false
		}
		return scheme, true
	}
	return "", false
}
