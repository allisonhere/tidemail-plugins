package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func call(t *testing.T, in string) (map[string]any, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(strings.NewReader(in), &out, &errOut)
	var resp map[string]any
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("stdout is not one JSON value: %q", out.String())
		}
	}
	return resp, code
}

func TestProtocol(t *testing.T) {
	resp, code := call(t, `{"api":1,"type":"request","request_id":"r1","method":"ping"}`)
	if code != 0 || resp["ok"] != true || resp["request_id"] != "r1" {
		t.Fatalf("ping: %v", resp)
	}
	resp, _ = call(t, `{"api":1,"type":"request","request_id":"r2","method":"nope"}`)
	if resp["ok"] == true || resp["error"].(map[string]any)["code"] != "unsupported_method" {
		t.Errorf("unknown method: %v", resp)
	}
	resp, _ = call(t, `{"api":2,"type":"request","request_id":"r3","method":"ping"}`)
	if resp["error"].(map[string]any)["code"] != "unsupported_api" {
		t.Errorf("wrong api: %v", resp)
	}
	resp, _ = call(t, `{"api":1,"type":"request","request_id":"r4","method":"message.metadata","data":"nope"}`)
	if resp["error"].(map[string]any)["code"] != "bad_metadata" {
		t.Errorf("bad metadata: %v", resp)
	}
	if _, code := call(t, ``); code != 1 {
		t.Error("an empty request has nothing to answer: exit 1")
	}
}

func TestMetadataEndToEndAndSettings(t *testing.T) {
	req := func(sens string) string {
		return `{"api":1,"type":"request","request_id":"m1","method":"message.received","settings":{"sensitivity":"` + sens + `"},` +
			`"data":{"id":5,"from":"PayPal <paypal@gmail.com>","subject":"Hi","has_attachment":false,"auth":{"spf":"pass"}}}`
	}
	resp, _ := call(t, req("normal"))
	data := resp["data"].(map[string]any)
	anns := data["annotations"].([]any)
	if len(anns) == 0 || data["presentation"] == nil {
		t.Fatalf("a brand claim from a free address should be flagged at normal: %v", resp)
	}
	resp, _ = call(t, req("low"))
	if got := resp["data"].(map[string]any)["annotations"].([]any); len(got) != 0 {
		t.Errorf("low sensitivity should clear it, got %v", got)
	}
	// Unknown extra fields from a newer TideMail are ignored.
	resp, _ = call(t, `{"api":1,"type":"request","request_id":"m2","method":"message.metadata","data":{"id":1,"from":"a@b.test","future_field":123}}`)
	if resp["ok"] != true {
		t.Errorf("extra fields should be ignored: %v", resp)
	}
}
