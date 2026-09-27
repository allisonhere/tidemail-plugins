package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// invoke runs the plugin on one stdin payload.
func invoke(t *testing.T, stdin string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// decodeOnly parses stdout as exactly one JSON response and nothing else.
func decodeOnly(t *testing.T, stdout string) response {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var resp response
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if _, err := dec.Token(); err == nil {
		t.Fatalf("stdout has data after the response:\n%s", stdout)
	}
	return resp
}

func TestPing(t *testing.T) {
	code, stdout, stderr := invoke(t, `{"api":1,"type":"request","request_id":"abc123","method":"ping","data":{}}`+"\n")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	resp := decodeOnly(t, stdout)
	if !resp.OK || resp.RequestID != "abc123" || resp.API != 1 || resp.Type != "response" {
		t.Fatalf("resp = %+v", resp)
	}
	var data pingResult
	if err := json.Unmarshal(resp.Data, &data); err != nil || data.Message != "pong" {
		t.Fatalf("data = %s", resp.Data)
	}
}

// TestFullMetadataRequest feeds a complete request, shaped exactly like the
// one TideMail sends, through the real entry point.
func TestFullMetadataRequest(t *testing.T) {
	req := `{"api":1,"type":"request","request_id":"9f1c2b7e4d","method":"message.metadata","data":{` +
		`"id":1234,"message_id":"<abc@mail.example>","from":"Sam Lee <sam@acme.io>","to":"me@acme.io",` +
		`"cc":"team@acme.io","reply_to":"sam@acme.io","subject":"Urgent: please review the overdue Q3 invoice?",` +
		`"date":"2026-09-26T10:00:00Z","read":false,"starred":false,"has_attachment":true,` +
		`"flags":["\\Recent"],"account_name":"Work","mailbox_name":"INBOX"}}` + "\n"
	code, stdout, stderr := invoke(t, req)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	resp := decodeOnly(t, stdout)
	if !resp.OK || resp.RequestID != "9f1c2b7e4d" || resp.Error != nil {
		t.Fatalf("resp = %+v", resp)
	}
	var data metadataResult
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatal(err)
	}
	got := annotationMap(data.Annotations)
	want := map[string]string{"needs_reply": "true", "urgency": "high", "importance": "high", "category": "billing"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all %v)", k, got[k], v, got)
		}
	}
	// Order matches TideMail's badge order.
	var keys []string
	for _, a := range data.Annotations {
		keys = append(keys, a.Key)
	}
	if strings.Join(keys, ",") != "needs_reply,urgency,importance,category" {
		t.Errorf("key order = %v", keys)
	}
	checkContract(t, "full request", data.Annotations)
}

func TestNeutralMessageReturnsEmptyAnnotations(t *testing.T) {
	_, stdout, _ := invoke(t, `{"api":1,"type":"request","request_id":"r1","method":"message.metadata","data":{"id":1,"from":"Chris <chris@acme.io>","subject":"Notes"}}`)
	resp := decodeOnly(t, stdout)
	// An explicit empty array, which TideMail treats as "clear this plugin's
	// annotations on the message".
	if !strings.Contains(string(resp.Data), `"annotations":[]`) {
		t.Fatalf("data = %s", resp.Data)
	}
}

func TestMissingOptionalFields(t *testing.T) {
	for _, data := range []string{`{}`, `{"id":5}`, `{"subject":"Hi?"}`, `{"flags":null,"from":""}`, `{"unknown_field":{"nested":true}}`} {
		code, stdout, _ := invoke(t, `{"api":1,"type":"request","request_id":"r","method":"message.metadata","data":`+data+`}`)
		resp := decodeOnly(t, stdout)
		if code != 0 || !resp.OK {
			t.Errorf("data %s: code=%d resp=%+v", data, code, resp)
		}
	}
}

func TestProtocolErrors(t *testing.T) {
	tests := []struct {
		name, req, code string
	}{
		{"unsupported method", `{"api":1,"type":"request","request_id":"x1","method":"message.body"}`, codeUnsupportedMethod},
		{"unsupported api", `{"api":2,"type":"request","request_id":"x1","method":"ping"}`, codeUnsupportedAPI},
		{"wrong type", `{"api":1,"type":"response","request_id":"x1","method":"ping"}`, codeBadRequest},
		{"metadata not an object", `{"api":1,"type":"request","request_id":"x1","method":"message.metadata","data":[1,2]}`, codeBadMetadata},
		{"metadata missing", `{"api":1,"type":"request","request_id":"x1","method":"message.metadata"}`, codeBadMetadata},
		{"metadata wrong types", `{"api":1,"type":"request","request_id":"x1","method":"message.metadata","data":{"subject":5}}`, codeBadMetadata},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, _ := invoke(t, tt.req)
			resp := decodeOnly(t, stdout)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0 for an answerable request", code)
			}
			// TideMail requires ok=false to carry an error and the same ID.
			if resp.OK || resp.Error == nil || resp.Error.Code != tt.code || resp.RequestID != "x1" || resp.Type != "response" || resp.API != 1 {
				t.Fatalf("resp = %+v (error %+v)", resp, resp.Error)
			}
		})
	}
}

func TestUnreadableRequestsExitNonZeroWithCleanStdout(t *testing.T) {
	for name, stdin := range map[string]string{
		"empty":         "",
		"whitespace":    "  \n",
		"invalid json":  `{"api":1,`,
		"not an object": `[]`,
		"no request id": `{"api":1,"type":"request","method":"ping"}`,
		"escape noise":  "\x1b[31m{",
		"too large":     `{"api":1,"type":"request","request_id":"r","method":"ping","data":"` + strings.Repeat("x", maxRequestBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := invoke(t, stdin)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if stdout != "" {
				t.Fatalf("stdout must stay empty when there is no request to answer, got %q", stdout)
			}
			if stderr == "" || strings.Count(stderr, "\n") != 1 || strings.ContainsRune(stderr, 0x1b) {
				t.Fatalf("stderr = %q", stderr)
			}
		})
	}
}

func TestRequestIDPreserved(t *testing.T) {
	for _, id := range []string{"a", "0123456789abcdef01234567", "weird-id_with.chars"} {
		_, stdout, _ := invoke(t, `{"api":1,"type":"request","request_id":"`+id+`","method":"ping"}`)
		if resp := decodeOnly(t, stdout); resp.RequestID != id {
			t.Errorf("request_id = %q, want %q", resp.RequestID, id)
		}
	}
}
