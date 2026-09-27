package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const testKey = "ts-test-SECRET-KEY-9999"

// fakeTypeSafe is an httptest System One server. It records every request
// and answers with status and answers (or raw, when set).
type fakeTypeSafe struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
	status   int
	answers  map[string]any
	raw      string
	delay    time.Duration
}

type recordedRequest struct {
	header http.Header
	body   []byte
	parsed jevRequestJSON
}

type jevRequestJSON struct {
	State     map[string]any            `json:"state"`
	Model     string                    `json:"model"`
	Questions map[string]map[string]any `json:"questions"`
}

func newFake(t *testing.T) *fakeTypeSafe {
	t.Helper()
	f := &fakeTypeSafe{status: http.StatusOK, answers: map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed jevRequestJSON
		_ = json.Unmarshal(body, &parsed)
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{header: r.Header.Clone(), body: body, parsed: parsed})
		status, raw, delay := f.status, f.raw, f.delay
		answers := map[string]any{}
		for id := range parsed.Questions {
			if a, ok := f.answers[id]; ok {
				answers[id] = a
			}
		}
		f.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(status)
		if raw != "" {
			_, _ = io.WriteString(w, raw)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]int{"input_tokens": 300}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTypeSafe) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeTypeSafe) last(t *testing.T) recordedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("no request was made")
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeTypeSafe) smart(stderr io.Writer) smart {
	return smart{jev: jevClient{endpoint: f.srv.URL, http: &http.Client{Timeout: 300 * time.Millisecond}}, apiKey: testKey, stderr: stderr}
}

// Answer builders.
func noul(p float64) map[string]any { return map[string]any{"type": "noul", "noul": p} }

func choice(c string, p float64) map[string]any {
	probs := map[string]float64{}
	rest := (1 - p) / float64(len(categoryOptions)-1)
	for _, o := range categoryOptions {
		probs[o] = rest
	}
	probs[c] = p
	return map[string]any{"type": "choice", "choice": c, "probabilities": probs, "confidence": p}
}

func score(p ...float64) map[string]any {
	probs := map[string]float64{}
	sc := 0.0
	for i, v := range p {
		probs[string(rune('0'+i))] = v
		sc += float64(i) * v
	}
	return map[string]any{"type": "score", "score": sc, "probabilities": probs, "confidence": 0.8}
}

func hybridOpts() options {
	o := defaultOptions()
	o.APIKey = testKey
	return o
}

func personalRequest() MessageMetadata {
	return MessageMetadata{
		ID: 7, From: "Susan <susan@example.com>", To: "me@example.com", CC: "boss@example.com",
		Subject: "Re: can you review the contract before Friday?", Date: "2026-09-26T10:00:00Z",
		HasAttachment: true, AccountName: "Work", MailboxName: "INBOX", Flags: []string{"\\Recent"},
	}
}

func githubNotification() MessageMetadata {
	return MessageMetadata{From: "Jane <notifications@github.com>", Subject: "Re: [org/repo] Can you look at this? (#12)"}
}

func sortedKeys[V any](m map[string]V) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestLocalModeMakesNoRequests(t *testing.T) {
	f := newFake(t)
	o := hybridOpts()
	o.Mode = modeLocal
	got := f.smart(io.Discard).classify(context.Background(), personalRequest(), o)
	if f.count() != 0 {
		t.Fatal("local mode made a network request")
	}
	if !reflect.DeepEqual(got, Classify(personalRequest())) {
		t.Fatalf("local mode = %v", got)
	}
}

func TestHybridSkipsJevForObviousServiceMail(t *testing.T) {
	f := newFake(t)
	got := annotationMap(f.smart(io.Discard).classify(context.Background(), githubNotification(), hybridOpts()))
	if f.count() != 0 {
		t.Fatal("hybrid called Jev for a known GitHub notification")
	}
	if got["category"] != "github" {
		t.Fatalf("got %v", got)
	}
}

func TestHybridAsksJevForAmbiguousMailInOneRequest(t *testing.T) {
	f := newFake(t)
	f.answers = map[string]any{
		qNeedsReply: noul(0.91), qUrgency: score(0.2, 0.5, 0.25, 0.05),
		qImportance: noul(0.83), qCategory: choice("personal", 0.8),
	}
	var stderr bytes.Buffer
	anns := f.smart(&stderr).classify(context.Background(), personalRequest(), hybridOpts())
	if f.count() != 1 {
		t.Fatalf("requests = %d, want exactly one", f.count())
	}
	req := f.last(t)
	if got := sortedKeys(req.parsed.Questions); !reflect.DeepEqual(got, []string{"category", "importance", "needs_reply", "urgency"}) {
		t.Fatalf("questions = %v", got)
	}
	// The contract fixture: a personal request needs a reply and is important,
	// but is not urgent enough (P(urgent)+P(critical) = 0.30).
	got := annotationMap(anns)
	want := map[string]string{"needs_reply": "true", "importance": "high", "category": "personal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("annotations = %v", got)
	}
	checkContract(t, "hybrid", anns)
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestJevModeAsksEvenForKnownServices(t *testing.T) {
	f := newFake(t)
	f.answers = map[string]any{
		qNeedsReply: noul(0.1), qUrgency: score(0.9, 0.1, 0, 0),
		qImportance: noul(0.2), qCategory: choice("github", 0.95),
	}
	o := hybridOpts()
	o.Mode = modeJev
	got := annotationMap(f.smart(io.Discard).classify(context.Background(), githubNotification(), o))
	if f.count() != 1 || len(f.last(t).parsed.Questions) != 4 {
		t.Fatal("jev mode should ask all enabled questions")
	}
	if !reflect.DeepEqual(got, map[string]string{"category": "github"}) {
		t.Fatalf("got %v", got)
	}
}

func TestJevAnswerMapping(t *testing.T) {
	tests := []struct {
		name    string
		answers map[string]any
		want    map[string]string
	}{
		{"below thresholds", map[string]any{qNeedsReply: noul(0.71), qUrgency: score(0.3, 0.2, 0.3, 0.2), qImportance: noul(0.5), qCategory: choice("billing", 0.49)},
			// P(urgent)+P(critical) = 0.5 < 0.6; the unsure category keeps
			// the local one, and there is none.
			map[string]string{}},
		{"urgent and critical", map[string]any{qNeedsReply: noul(0.1), qUrgency: score(0, 0.1, 0.4, 0.5), qImportance: noul(0.1), qCategory: choice("none", 0.9)},
			map[string]string{"urgency": "high"}},
		{"none clears category", map[string]any{qNeedsReply: noul(0), qUrgency: score(1, 0, 0, 0), qImportance: noul(0), qCategory: choice("none", 0.8)},
			map[string]string{}},
		{"category chosen", map[string]any{qNeedsReply: noul(0), qUrgency: score(1, 0, 0, 0), qImportance: noul(0), qCategory: choice("billing", 0.7)},
			map[string]string{"category": "billing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			f.answers = tt.answers
			meta := MessageMetadata{From: "Pat <pat@acme.io>", Subject: "Quarterly numbers"}
			anns := f.smart(io.Discard).classify(context.Background(), meta, hybridOpts())
			if got := annotationMap(anns); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			checkContract(t, tt.name, anns)
		})
	}
}

func TestHybridKeepsStrongLocalCategory(t *testing.T) {
	f := newFake(t)
	f.answers = map[string]any{qNeedsReply: noul(0.9), qUrgency: score(1, 0, 0, 0), qImportance: noul(0.1), qCategory: choice("personal", 0.99)}
	// A person writing from a GitHub address: not automated, so Jev is asked,
	// but the certain local category is kept and not even asked about.
	meta := MessageMetadata{From: "Octo Cat <octocat@github.com>", Subject: "Can you join the call?"}
	got := annotationMap(f.smart(io.Discard).classify(context.Background(), meta, hybridOpts()))
	if _, asked := f.last(t).parsed.Questions[qCategory]; asked {
		t.Fatal("category should not be asked when the local rules are certain")
	}
	if got["category"] != "github" || got["needs_reply"] != "true" {
		t.Fatalf("got %v", got)
	}
	// With local rules first off, Jev decides the category too.
	o := hybridOpts()
	o.LocalRulesFirst = false
	got = annotationMap(f.smart(io.Discard).classify(context.Background(), meta, o))
	if got["category"] != "personal" {
		t.Fatalf("got %v", got)
	}
}

func TestJevOverridesFuzzyLocalJudgment(t *testing.T) {
	meta := MessageMetadata{From: "Deals <deals@marketing-example.com>", Subject: "Can you believe these deals?"}
	if annotationMap(Classify(meta))["needs_reply"] != "true" {
		t.Fatal("setup: the local rules should wrongly flag this marketing question")
	}
	f := newFake(t)
	f.answers = map[string]any{qNeedsReply: noul(0.04), qUrgency: score(0.95, 0.05, 0, 0), qImportance: noul(0.03), qCategory: choice("newsletter", 0.85)}
	got := annotationMap(f.smart(io.Discard).classify(context.Background(), meta, hybridOpts()))
	if _, ok := got["needs_reply"]; ok || got["category"] != "newsletter" {
		t.Fatalf("got %v", got)
	}
}

func TestMissingKeyFallsBackWithoutNetwork(t *testing.T) {
	f := newFake(t)
	s := f.smart(io.Discard)
	o := hybridOpts()
	o.APIKey = ""
	if got := s.classify(context.Background(), personalRequest(), o); !reflect.DeepEqual(got, Classify(personalRequest())) || f.count() != 0 {
		t.Fatalf("got %v, requests %d", got, f.count())
	}
	o = hybridOpts()
	o.JevEnabled = false
	s.classify(context.Background(), personalRequest(), o)
	if f.count() != 0 {
		t.Fatal("Jev turned off but a request was made")
	}
}

func TestAPIFailuresFallBackLocally(t *testing.T) {
	cases := map[string]func(f *fakeTypeSafe){
		"401":       func(f *fakeTypeSafe) { f.status = http.StatusUnauthorized; f.raw = `{"detail":"bad key"}` },
		"402":       func(f *fakeTypeSafe) { f.status = http.StatusPaymentRequired; f.raw = `{}` },
		"429":       func(f *fakeTypeSafe) { f.status = http.StatusTooManyRequests; f.raw = `{}` },
		"500":       func(f *fakeTypeSafe) { f.status = http.StatusInternalServerError; f.raw = `oops` },
		"529":       func(f *fakeTypeSafe) { f.status = 529; f.raw = `{}` },
		"timeout":   func(f *fakeTypeSafe) { f.delay = time.Second },
		"malformed": func(f *fakeTypeSafe) { f.raw = `{"answers": "nope"` },
		"wrong type": func(f *fakeTypeSafe) {
			f.answers = map[string]any{qNeedsReply: choice("none", 1), qUrgency: score(1, 0, 0, 0), qImportance: noul(0), qCategory: choice("none", 1)}
		},
		"out of range": func(f *fakeTypeSafe) {
			f.answers = map[string]any{qNeedsReply: noul(1.7), qUrgency: score(1, 0, 0, 0), qImportance: noul(0), qCategory: choice("none", 1)}
		},
		"unknown category": func(f *fakeTypeSafe) {
			f.answers = map[string]any{qNeedsReply: noul(0), qUrgency: score(1, 0, 0, 0), qImportance: noul(0), qCategory: map[string]any{"type": "choice", "choice": "spam", "probabilities": map[string]float64{"spam": 1}}}
		},
		"missing answer": func(f *fakeTypeSafe) { f.answers = map[string]any{qNeedsReply: noul(0.9)} },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			setup(f)
			var stderr bytes.Buffer
			got := f.smart(&stderr).classify(context.Background(), personalRequest(), hybridOpts())
			if !reflect.DeepEqual(got, Classify(personalRequest())) {
				t.Fatalf("fallback = %v, want local %v", got, Classify(personalRequest()))
			}
			if !strings.Contains(stderr.String(), "using local rules") || strings.Contains(stderr.String(), testKey) {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestFallbackIsNotAProcessFailure(t *testing.T) {
	f := newFake(t)
	f.status = http.StatusUnauthorized
	f.raw = `{}`
	var out, errOut bytes.Buffer
	req := `{"api":1,"type":"request","request_id":"r1","method":"message.received","settings":{"mode":"hybrid"},` +
		`"data":{"id":1,"from":"Susan <susan@example.com>","subject":"Can you review this?"}}`
	code := f.smart(&errOut).run(strings.NewReader(req), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	resp := decodeOnly(t, out.String())
	if !resp.OK || resp.RequestID != "r1" || !strings.Contains(string(resp.Data), "needs_reply") {
		t.Fatalf("resp = %+v data %s", resp, resp.Data)
	}
}

func TestReceivedAndMetadataClassifyIdentically(t *testing.T) {
	f := newFake(t)
	f.answers = map[string]any{qNeedsReply: noul(0.9), qUrgency: score(0, 0, 0.7, 0.3), qImportance: noul(0.8), qCategory: choice("personal", 0.9)}
	data := `{"id":1,"from":"Susan <susan@example.com>","subject":"Urgent: can you sign today?"}`
	var outs []string
	for _, method := range []string{methodMessageMetadata, methodMessageReceived} {
		var out bytes.Buffer
		req := `{"api":1,"type":"request","request_id":"r","method":"` + method + `","data":` + data + `}`
		if code := f.smart(io.Discard).run(strings.NewReader(req), &out, io.Discard); code != 0 {
			t.Fatalf("%s exit %d", method, code)
		}
		outs = append(outs, out.String())
	}
	if outs[0] != outs[1] {
		t.Fatalf("results differ:\n%s\n%s", outs[0], outs[1])
	}
}

func TestDisabledQuestionsAreNotSent(t *testing.T) {
	f := newFake(t)
	f.answers = map[string]any{qNeedsReply: noul(0.9), qImportance: noul(0.9)}
	o := hybridOpts()
	o.AskUrgency, o.AskCategory = false, false
	meta := MessageMetadata{From: "Pat <pat@acme.io>", Subject: "URGENT: sign this"}
	got := annotationMap(f.smart(io.Discard).classify(context.Background(), meta, o))
	if q := sortedKeys(f.last(t).parsed.Questions); !reflect.DeepEqual(q, []string{"importance", "needs_reply"}) {
		t.Fatalf("questions = %v", q)
	}
	// Urgency was not asked, so the local rule decides it.
	if got["urgency"] != "high" {
		t.Fatalf("got %v", got)
	}

	o.AskNeedsReply, o.AskImportance = false, false
	before := f.count()
	f.smart(io.Discard).classify(context.Background(), meta, o)
	if f.count() != before {
		t.Fatal("no questions enabled but a request was made")
	}
}

func TestRequestShapeAndPrivacy(t *testing.T) {
	f := newFake(t)
	f.answers = map[string]any{qNeedsReply: noul(0.9), qUrgency: score(1, 0, 0, 0), qImportance: noul(0.9), qCategory: choice("personal", 0.9)}
	o := hybridOpts()
	o.Model = modelPinned
	f.smart(io.Discard).classify(context.Background(), personalRequest(), o)
	req := f.last(t)
	if got := req.header.Get("Authorization"); got != "Bearer "+testKey {
		t.Fatalf("Authorization = %q", got)
	}
	if req.header.Get("Content-Type") != "application/json" {
		t.Fatal("wrong content type")
	}
	if strings.Contains(string(req.body), testKey) {
		t.Fatal("the API key appeared in the request body")
	}
	if req.parsed.Model != modelPinned {
		t.Fatalf("model = %q", req.parsed.Model)
	}
	// Only these fields are sent about the message.
	want := []string{"automated_sender", "from", "has_attachment", "is_reply_in_thread", "local_category", "subject"}
	if got := sortedKeys(req.parsed.State); !reflect.DeepEqual(got, want) && !reflect.DeepEqual(got, remove(want, "local_category")) {
		t.Fatalf("state keys = %v", got)
	}
	for _, leaked := range []string{"me@example.com", "boss@example.com", "Work", "INBOX", "2026-09-26", "Recent"} {
		if strings.Contains(string(req.body), leaked) {
			t.Fatalf("request contains %q, which should not be sent", leaked)
		}
	}
	// No idempotency header: TypeSafe does not document one.
	if req.header.Get("Idempotency-Key") != "" {
		t.Fatal("unexpected Idempotency-Key")
	}
}

func remove(s []string, v string) []string {
	var out []string
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func TestTestConnection(t *testing.T) {
	f := newFake(t)
	f.answers = map[string]any{"ping": noul(0.99)}
	res := f.smart(io.Discard).testConnection(context.Background(), hybridOpts())
	if !res.OK || !strings.Contains(res.Message, "connection successful") || !strings.Contains(res.Message, "Model: jev-latest (jev-1.13.0)") {
		t.Fatalf("res = %+v", res)
	}
	if len(f.last(t).parsed.Questions) != 1 {
		t.Fatal("the test should send one tiny question")
	}

	f.status = http.StatusUnauthorized
	f.raw = `{"detail":"invalid key ` + testKey + `"}`
	res = f.smart(io.Discard).testConnection(context.Background(), hybridOpts())
	if res.OK || res.Message != "TypeSafe rejected the API key" {
		t.Fatalf("res = %+v", res)
	}

	f.status = http.StatusUnprocessableEntity
	res = f.smart(io.Discard).testConnection(context.Background(), hybridOpts())
	if res.OK || !strings.Contains(res.Message, "did not accept model") {
		t.Fatalf("res = %+v", res)
	}

	o := hybridOpts()
	o.APIKey = ""
	if res := f.smart(io.Discard).testConnection(context.Background(), o); res.OK || !strings.Contains(res.Message, "not set") {
		t.Fatalf("res = %+v", res)
	}
	o.Mode = modeLocal
	if res := f.smart(io.Discard).testConnection(context.Background(), o); !res.OK || !strings.Contains(res.Message, "Local mode") {
		t.Fatalf("res = %+v", res)
	}
}

func TestTestConnectionNeverExposesKey(t *testing.T) {
	f := newFake(t)
	f.status = http.StatusInternalServerError
	f.raw = `server error mentioning ` + testKey
	var out, errOut bytes.Buffer
	req := `{"api":1,"type":"request","request_id":"t","method":"plugin.test","settings":{"mode":"hybrid"}}`
	f.smart(&errOut).run(strings.NewReader(req), &out, &errOut)
	if strings.Contains(out.String(), testKey) || strings.Contains(errOut.String(), testKey) {
		t.Fatalf("key exposed: %s %s", out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "connection failed") {
		t.Fatalf("out = %s", out.String())
	}
}

func TestParseOptions(t *testing.T) {
	o := parseOptions(nil, "")
	if o != defaultOptions() {
		t.Fatalf("defaults = %+v", o)
	}
	o = parseOptions(map[string]any{
		"mode": "jev", "jev_model": "jev-1.13.0", "jev_enabled": false, "local_rules_first": false,
		"jev_needs_reply": false, "jev_urgency": "no", // wrong type keeps the default
	}, "k")
	if o.Mode != modeJev || o.Model != modelPinned || o.JevEnabled || o.LocalRulesFirst || o.AskNeedsReply || !o.AskUrgency || o.APIKey != "k" {
		t.Fatalf("parsed = %+v", o)
	}
	if o := parseOptions(map[string]any{"mode": "cloud", "jev_model": "gpt-5"}, ""); o.Mode != modeHybrid || o.Model != modelLatest {
		t.Fatalf("invalid values should fall back: %+v", o)
	}
}

func TestClassifierVersionIsNotAnAnnotation(t *testing.T) {
	for _, a := range Classify(personalRequest()) {
		if strings.Contains(a.Key+a.Value, classifierVersion) || strings.HasPrefix(a.Key, "jev") || strings.Contains(a.Key, "api") {
			t.Fatalf("operational detail leaked into annotations: %+v", a)
		}
	}
}
