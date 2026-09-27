// Command tidemail-plugin-smart is a TideMail plugin that tags mail with
// needs-reply, urgency, importance, and a category. It reads one protocol v1
// request on stdin, writes one response on stdout, and exits. Local rules
// always run; in hybrid or jev mode, with an API key, it also asks TypeSafe's
// Jev model about the sender and subject. It never sees message bodies.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

// run handles one invocation with the real TypeSafe client and the API key
// TideMail put in the environment.
func run(stdin io.Reader, stdout, stderr io.Writer) int {
	return smart{jev: newJevClient(), apiKey: apiKeyFromEnv(), stderr: stderr}.run(stdin, stdout, stderr)
}

// run handles one invocation. stdout receives exactly one JSON response or
// nothing; diagnostics go to stderr. It returns the process exit code:
// 0 when a response was written, 1 when the request could not be read (there
// is no request ID to reply to, so TideMail reports the exit status and
// stderr instead).
func (s smart) run(stdin io.Reader, stdout, stderr io.Writer) int {
	req, err := readRequest(stdin)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-smart:", oneLine(err.Error()))
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(s.handle(context.Background(), req)); err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-smart: write response:", oneLine(err.Error()))
		return 1
	}
	return 0
}

// oneLine keeps stderr diagnostics to a single printable line.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
}
