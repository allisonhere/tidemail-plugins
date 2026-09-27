package main

import (
	"os"
	"testing"
)

// Tests must never reach the real TypeSafe API, so the key TideMail would
// pass is always cleared. Tests that exercise Jev use httptest servers.
func TestMain(m *testing.M) {
	_ = os.Unsetenv(apiKeyEnv)
	os.Exit(m.Run())
}
