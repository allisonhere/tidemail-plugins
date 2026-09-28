package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func call(t *testing.T, in string) (map[string]any, string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(strings.NewReader(in), &out, &errOut)
	var resp map[string]any
	if out.Len() > 0 {
		dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
		if err := dec.Decode(&resp); err != nil {
			t.Fatalf("stdout is not JSON: %q", out.String())
		}
		if dec.More() {
			t.Fatalf("stdout has more than one JSON value: %q", out.String())
		}
	}
	return resp, out.String(), errOut.String(), code
}

func TestPing(t *testing.T) {
	resp, _, stderr, code := call(t, `{"api":1,"type":"request","request_id":"p1","method":"ping"}`)
	if code != 0 || resp["ok"] != true || resp["request_id"] != "p1" || resp["data"].(map[string]any)["message"] != "pong" || stderr != "" {
		t.Fatalf("ping = %v (stderr %q)", resp, stderr)
	}
}

func TestMetadataAndReceived(t *testing.T) {
	for _, method := range []string{methodMessageMetadata, methodMessageReceived} {
		in := fmt.Sprintf(`{"api":1,"type":"request","request_id":"r-%s","method":%q,"data":{"id":7,"from":"Shoply <promotions@shoply.example>","subject":"Last chance: 30%% off","read":false,"starred":false,"has_attachment":false}}`, method, method)
		resp, stdout, stderr, code := call(t, in)
		if code != 0 || resp["ok"] != true || resp["request_id"] != "r-"+method || stderr != "" {
			t.Fatalf("%s = %v (stderr %q)", method, resp, stderr)
		}
		if strings.Count(strings.TrimSpace(stdout), "\n") != 0 {
			t.Fatalf("%s: stdout must be one line of JSON: %q", method, stdout)
		}
		var data annotationsResult
		raw, _ := json.Marshal(resp["data"])
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		got := annotationMap(data.Annotations)
		if got[keyCategory] != "newsletter" || got[keySenderValue] != valueLow {
			t.Fatalf("%s annotations = %v", method, got)
		}
		// TideMail's validation rules: <=32 annotations, key syntax, values.
		if len(data.Annotations) > 32 {
			t.Fatal("too many annotations")
		}
		for _, a := range data.Annotations {
			for _, r := range a.Key {
				if !isWordRune(r) && r != '_' && r != '.' && r != '-' {
					t.Fatalf("invalid key %q", a.Key)
				}
			}
		}
	}
}

func TestProtocolErrors(t *testing.T) {
	for in, code := range map[string]string{
		`{"api":2,"type":"request","request_id":"e","method":"ping"}`:                        "unsupported_api",
		`{"api":1,"type":"request","request_id":"e","method":"report.run"}`:                  "unsupported_method",
		`{"api":1,"type":"request","request_id":"e","method":"message.metadata","data":"x"}`: "bad_metadata",
		`{"api":1,"type":"response","request_id":"e","method":"ping"}`:                       "bad_request",
	} {
		resp, _, _, exit := call(t, in)
		errBody, _ := resp["error"].(map[string]any)
		if exit != 0 || resp["ok"] != false || resp["request_id"] != "e" || errBody["code"] != code {
			t.Fatalf("%s: %v", in, resp)
		}
	}
	for _, in := range []string{``, `not json`, `{"api":1,"type":"request","method":"ping"}`} {
		if _, stdout, _, exit := call(t, in); exit != 1 || stdout != "" {
			t.Fatalf("%q: unreadable input must exit 1 with no stdout (got %d %q)", in, exit, stdout)
		}
	}
}

// Privacy (§8, §24 38-40): the plugin's own code cannot reach the network,
// run programs, or read the environment, and the manifest asks for nothing
// beyond metadata and annotations.
func TestNoNetworkNoExecNoEnv(t *testing.T) {
	banned := map[string]bool{"net/http": true, "net": true, "net/smtp": true, "crypto/tls": true, "os/exec": true, "syscall": true, "net/rpc": true}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if banned[path] {
				t.Fatalf("%s imports %s", name, path)
			}
		}
		src, _ := os.ReadFile(name)
		for _, call := range []string{"os.Getenv", "os.LookupEnv", "os.Environ", "os.ReadFile", "os.Open"} {
			if bytes.Contains(src, []byte(call)) {
				t.Fatalf("%s calls %s", name, call)
			}
		}
	}
	manifest, err := os.ReadFile("plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, perm := range []string{"network", "message_body", "messages_query", "threads_query", "annotations_query", "analytics_read"} {
		if bytes.Contains(manifest, []byte(perm+" = true")) {
			t.Fatalf("manifest requests %s", perm)
		}
	}
	for _, perm := range []string{"message_metadata = true", "annotations = true"} {
		if !bytes.Contains(manifest, []byte(perm)) {
			t.Fatalf("manifest missing %s", perm)
		}
	}
}

func TestNoBodyRequired(t *testing.T) {
	// Only headers are sent; a body field, if TideMail ever sent one, is ignored.
	resp, _, _, _ := call(t, `{"api":1,"type":"request","request_id":"b","method":"message.metadata","data":{"id":1,"from":"newsletter@acme.example","subject":"Hi","body":"SECRET BODY"}}`)
	raw, _ := json.Marshal(resp)
	if bytes.Contains(raw, []byte("SECRET")) {
		t.Fatal("body content echoed")
	}
}

func BenchmarkClassify(b *testing.B) {
	for _, n := range []int{1, 1000, 10000} {
		b.Run(fmt.Sprintf("messages=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for j := 0; j < n; j++ {
					f := corpus[j%len(corpus)]
					_ = classify(f.meta, f.h).annotations()
				}
			}
		})
	}
}
