// Command tidemail-plugin-analytics is a TideMail report plugin: a
// plain-text mail dashboard with volume sparklines, weekday rhythm,
// categories, attention, response times, top correspondents, and your
// busiest conversations. It asks TideMail read-only queries (report.run) and
// never sees message bodies, uses the network, or changes mail.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

// run handles one invocation: one request on stdin, one response on stdout.
func run(stdin io.Reader, stdout, stderr io.Writer) int {
	req, err := readRequest(stdin)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-analytics:", oneLine(err.Error()))
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(handle(req)); err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-analytics: write response:", oneLine(err.Error()))
		return 1
	}
	return 0
}

func handle(req request) response {
	switch {
	case req.API != apiVersion:
		return fail(req, "unsupported_api", fmt.Sprintf("api %d is not supported (want %d)", req.API, apiVersion))
	case req.Type != "request":
		return fail(req, "bad_request", fmt.Sprintf("type %q is not a request", req.Type))
	}
	switch req.Method {
	case "ping":
		return ok(req, map[string]string{"message": "pong"})
	case "report.run":
		var rr reportRequest
		if err := json.Unmarshal(req.Data, &rr); err != nil {
			return fail(req, "bad_request", "cannot read report request")
		}
		s, err := step(rr, req.Settings)
		if err != nil {
			return fail(req, "bad_results", err.Error())
		}
		return ok(req, s)
	}
	return fail(req, "unsupported_method", fmt.Sprintf("method %q is not supported", req.Method))
}

func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
}
