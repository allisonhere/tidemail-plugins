// Command tidemail-plugin-unsubscribe-plus is Unsubscribe+, a TideMail
// plugin that recognizes newsletter and list mail, flags automated senders,
// rates sender value, and describes unsubscribe options when TideMail
// provides list headers. It reads header metadata only, never message
// bodies; makes no network requests; never visits an unsubscribe link; and
// never changes mail. Classification is local and deterministic.
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
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-unsubscribe-plus:", oneLine(err.Error()))
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(handle(req)); err != nil {
		_, _ = fmt.Fprintln(stderr, "tidemail-plugin-unsubscribe-plus: write response:", oneLine(err.Error()))
		return 1
	}
	return 0
}

func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
}
