// Command tidemail-plugin-smart is a local, rule-based TideMail plugin. It
// reads one protocol v1 request on stdin, writes one response on stdout, and
// exits. It never touches the network, the filesystem, or message bodies.
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

// run handles one invocation. stdout receives exactly one JSON response or
// nothing; diagnostics go to stderr. It returns the process exit code:
// 0 when a response was written, 1 when the request could not be read (there
// is no request ID to reply to, so TideMail reports the exit status and
// stderr instead).
func run(stdin io.Reader, stdout, stderr io.Writer) int {
	req, err := readRequest(stdin)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-smart:", oneLine(err.Error()))
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(handle(req)); err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-smart: write response:", oneLine(err.Error()))
		return 1
	}
	return 0
}

// oneLine keeps stderr diagnostics to a single printable line.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
}
