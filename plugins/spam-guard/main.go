// Command tidemail-plugin-spam-guard is Spam & Phishing Guard, a TideMail
// plugin that warns about phishing, spoofed senders, and scam mail. It reads
// header metadata only (sender, Reply-To, subject, and the sender checks the
// mail server recorded), never message bodies; makes no network requests; and
// never changes, moves, or opens mail or links. Every verdict is local and
// deterministic, so reclassifying a message gives the same answer.
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

// run handles one invocation: one request on stdin, exactly one JSON response
// on stdout (or nothing, with exit 1, when there is no request to answer).
func run(stdin io.Reader, stdout, stderr io.Writer) int {
	req, err := readRequest(stdin)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-spam-guard:", oneLine(err.Error()))
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(handle(req)); err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-spam-guard: write response:", oneLine(err.Error()))
		return 1
	}
	return 0
}

func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
}
