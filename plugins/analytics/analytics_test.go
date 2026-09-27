package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func call(t *testing.T, in string) (response, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(strings.NewReader(in), &out, &errOut)
	var resp response
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("stdout is not one JSON value: %q", out.String())
		}
	}
	return resp, code
}

func TestProtocol(t *testing.T) {
	resp, code := call(t, `{"api":1,"type":"request","request_id":"r1","method":"ping"}`)
	if code != 0 || !resp.OK || resp.RequestID != "r1" {
		t.Fatalf("ping = %+v", resp)
	}
	for _, in := range []string{
		`{"api":1,"type":"request","request_id":"r","method":"message.metadata","data":{}}`,
		`{"api":2,"type":"request","request_id":"r","method":"ping"}`,
		`{"api":1,"type":"request","request_id":"r","method":"report.run","data":{"round":2,"state":"nope"}}`,
	} {
		if resp, code := call(t, in); code != 0 || resp.OK || resp.Error == nil || resp.RequestID != "r" {
			t.Fatalf("%s: %+v", in, resp)
		}
	}
	if _, code := call(t, `not json`); code != 1 {
		t.Fatal("unreadable input must exit non-zero")
	}
}

var ctx = reportContext{Now: "2026-09-27T12:00:00Z", Timezone: "UTC"}

func mustStep(t *testing.T, rr reportRequest, settings map[string]any) reportStep {
	t.Helper()
	s, err := step(rr, settings)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func stateJSON(t *testing.T, st *state) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestFirstRoundFollowsSettings(t *testing.T) {
	s := mustStep(t, reportRequest{Round: 1, Context: ctx}, map[string]any{"range": "90d", "conversations": true})
	q := s.Queries["threads"]
	if q == nil || s.State.Phase != phaseThreads || s.State.Range != "90d" {
		t.Fatalf("round 1 = %+v", s)
	}
	if q["filters"].(map[string]any)["date_from"] != "2026-06-29T12:00:00Z" {
		t.Fatalf("thread filter = %v", q["filters"])
	}

	s = mustStep(t, reportRequest{Round: 1, Context: ctx}, map[string]any{"range": "bogus", "conversations": false})
	if s.State.Phase != phaseStats || s.State.Range != "30d" || len(s.Queries) != 5 || s.Queries["threads"] != nil {
		t.Fatalf("counts-only round 1 = %+v", s)
	}
	if s.Queries["volume"]["group_by"] != "day" {
		t.Fatalf("volume query = %v", s.Queries["volume"])
	}
	s = mustStep(t, reportRequest{Round: 1, Context: ctx}, map[string]any{"range": "365d", "conversations": false})
	if s.Queries["volume"]["group_by"] != "week" {
		t.Fatal("a year groups by week")
	}
}

func threadsPage(n int, cursor string, base int) json.RawMessage {
	var rows []string
	for i := 0; i < n; i++ {
		rows = append(rows, fmt.Sprintf(`{"message_count":%d,"latest_subject":"S%d","participants":[{"address":"a@x"},{"address":"b@x"}],"needs_you":%t}`, base+i, base+i, i == 0))
	}
	return json.RawMessage(fmt.Sprintf(`{"threads":[%s],"next_cursor":%q}`, strings.Join(rows, ","), cursor))
}

func TestThreadPagingIsBounded(t *testing.T) {
	st := mustStep(t, reportRequest{Round: 1, Context: ctx}, nil).State
	round := 1
	for {
		round++
		s := mustStep(t, reportRequest{Round: round, Context: ctx, State: stateJSON(t, st),
			Results: map[string]json.RawMessage{"threads": threadsPage(500, "more", round*1000)}}, nil)
		st = s.State
		if len(stateJSON(t, st)) > 4<<10 {
			t.Fatalf("state grew to %d bytes", len(stateJSON(t, st)))
		}
		if st.Phase == phaseStats {
			if len(s.Queries) != 5 {
				t.Fatalf("stats round = %+v", s.Queries)
			}
			break
		}
		if s.Queries["threads"]["cursor"] != "more" {
			t.Fatal("next page must pass the cursor")
		}
	}
	if st.Pages != maxThreadPages || st.Scanned != maxThreadPages*500 || round+2 > 8 {
		t.Fatalf("pages %d scanned %d rounds %d", st.Pages, st.Scanned, round+2)
	}
	if len(st.Top) != topThreads || st.Top[0].Messages < st.Top[1].Messages || st.Top[0].People != 2 {
		t.Fatalf("top = %+v", st.Top)
	}
}

func TestBusiestSkipsSingleMessages(t *testing.T) {
	top := busiest(nil, []threadRow{{MessageCount: 1, LatestSubject: "solo"}, {MessageCount: 4, LatestSubject: "chat", WaitingOnThem: true}})
	if len(top) != 1 || top[0].Subject != "chat" || !top[0].Waiting {
		t.Fatalf("top = %+v", top)
	}
}

const statsResults = `{
 "volume":{"group_by":"day","received":1234,"sent":56,"buckets":[
   {"start":"2026-09-21","received":10,"sent":1},{"start":"2026-09-22","received":30,"sent":2},
   {"start":"2026-09-23","received":0,"sent":0},{"start":"2026-09-24","received":5,"sent":0},
   {"start":"2026-09-25","received":7,"sent":3},{"start":"2026-09-26","received":2,"sent":0},
   {"start":"2026-09-27","received":1,"sent":1}]},
 "categories":{"total":1234,"categories":[{"category":"newsletter","count":600},{"category":"a-very-long-category","count":400},{"category":"none","count":234}]},
 "attention":{"needs_you_current":12,"needs_you_dismissed_current":3,"waiting_current":5,"snoozed_messages_current":2,"snoozed_threads_current":1},
 "responses":{"user_sample_count":41,"median_user_response_s":5400,"average_user_response_s":20160,"other_sample_count":1,"median_other_response_s":90061,"average_other_response_s":90061},
 "contacts":{"correspondents":[{"address":"ann@example.com","name":"Ann Lee With A Very Long Display Name","received":40,"sent":22,"total":62},{"address":"bob@example.com","received":1,"sent":0,"total":1}],
   "domains":[{"domain":"example.com","total":63},{"domain":"github.example","total":20},{"domain":"news.example","total":9},{"domain":"shop.example","total":4},{"domain":"x.example","total":1}]}
}`

func TestRenderDashboard(t *testing.T) {
	var results map[string]json.RawMessage
	if err := json.Unmarshal([]byte(statsResults), &results); err != nil {
		t.Fatal(err)
	}
	st := state{Range: "7d", Conversations: true, Phase: phaseStats, Scanned: 812,
		Top: []thread{{Subject: "Quarterly plan", Messages: 9, People: 3, NeedsYou: true}, {Subject: "", Messages: 4, Waiting: true}}}
	s := mustStep(t, reportRequest{Round: 3, Context: ctx, State: stateJSON(t, &st), Results: results}, nil)
	out := s.Report
	for _, want := range []string{
		"TideMail Analytics · last 7 days", "RECEIVED", "1,234", "NEEDS YOU", "peak 30 · avg 7.9",
		"Mon", "newsletter", "49%", "a-very-long…", "you reply   1h 30m", "they reply  1d 1h", "(1 reply)",
		"Ann Lee With A Ve…", "bob@example.com", "domains: example.com 63", "812 scanned",
		"9 msgs  Quarterly plan ↩", "(no subject) …", "snoozed 3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if n := utf8.RuneCountInString(l); n > width {
			t.Errorf("line longer than %d columns (%d): %q", width, n, l)
		}
		for _, r := range l {
			if unicode.IsControl(r) {
				t.Fatalf("control character in %q", l)
			}
		}
	}
	if s.Queries != nil || s.State != nil {
		t.Fatal("the last round only reports")
	}
}

func TestMissingResultFails(t *testing.T) {
	st := state{Range: "30d", Phase: phaseStats}
	if _, err := step(reportRequest{Round: 2, Context: ctx, State: stateJSON(t, &st), Results: map[string]json.RawMessage{}}, nil); err == nil {
		t.Fatal("missing results must fail")
	}
}

func TestChartHelpers(t *testing.T) {
	if got := sparkline([]int{0, 1, 2, 4, 8}, 10); got != "▁▂▂▄█" { // non-zero never looks like zero
		t.Fatalf("sparkline = %q", got)
	}
	if got := sparkline(make([]int, 100), 10); utf8.RuneCountInString(got) != 10 {
		t.Fatalf("downsampled sparkline = %q", got)
	}
	if got := bar(1, 8, 1); got != "▏" {
		t.Fatalf("bar = %q", got)
	}
	if got := bar(3, 4, 4); got != "███ " {
		t.Fatalf("bar = %q", got)
	}
	if got := bar(1, 1000, 4); got != "▏   " {
		t.Fatalf("tiny values stay visible: %q", got)
	}
	for sec, want := range map[int64]string{30: "30s", 600: "10m", 3600: "1h", 5400: "1h 30m", 86400: "1d", 90061: "1d 1h"} {
		if got := duration(sec); got != want {
			t.Errorf("duration(%d) = %q, want %q", sec, got, want)
		}
	}
	if human(1234567) != "1,234,567" || human(12) != "12" {
		t.Fatal("human")
	}
	if got := joinWrapped("d: ", []string{"aaaa", "bbbb", "cccc"}, " · ", 14); strings.Join(got, "|") != "d: aaaa · bbbb|   cccc" {
		t.Fatalf("joinWrapped = %q", got)
	}
}

func statsFixture(t *testing.T) stats {
	t.Helper()
	var results map[string]json.RawMessage
	if err := json.Unmarshal([]byte(statsResults), &results); err != nil {
		t.Fatal(err)
	}
	var s stats
	if err := s.decode(results); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestViewOnlyWhenTideMailDrawsViews(t *testing.T) {
	var results map[string]json.RawMessage
	_ = json.Unmarshal([]byte(statsResults), &results)
	st := state{Range: "30d", Phase: phaseStats}
	old := mustStep(t, reportRequest{Round: 2, Context: ctx, State: stateJSON(t, &st), Results: results}, nil)
	if old.View != nil || old.Report == "" {
		t.Fatal("TideMail without views gets the text dashboard")
	}
	withViews := ctx
	withViews.Views = 1
	s := mustStep(t, reportRequest{Round: 2, Context: withViews, State: stateJSON(t, &st), Results: results}, nil)
	if s.View == nil || s.Report != "" {
		t.Fatal("TideMail with views gets a view")
	}
}

func TestViewBlocksStayWithinTideMailLimits(t *testing.T) {
	st := state{Range: "7d", Conversations: true, Scanned: 812,
		Top: []thread{{Subject: strings.Repeat("long subject ", 20), Messages: 9, People: 3, NeedsYou: true}, {Messages: 4, Waiting: true}}}
	v := buildView(st, statsFixture(t), ctx)
	raw, _ := json.Marshal(v)
	if bytes.Contains(raw, []byte("color")) || bytes.Contains(raw, []byte("\\u001b")) {
		t.Fatal("views carry semantics only")
	}
	types := map[string]bool{}
	checkText := func(s string, max int) {
		if utf8.RuneCountInString(s) > max {
			t.Fatalf("%q longer than %d", s, max)
		}
	}
	checkText(v.Title, 80)
	checkText(v.Subtitle, 80)
	if len(v.Blocks) > 32 {
		t.Fatalf("%d blocks", len(v.Blocks))
	}
	for _, b := range v.Blocks {
		types[b.Type] = true
		checkText(b.Title, 80)
		checkText(b.Note, 80)
		for _, it := range b.Items {
			checkText(it.Label, 40)
			checkText(it.Value, 24)
			checkText(it.Note, 40)
		}
		for _, r := range b.Rows {
			if len(r.Values) > 60 {
				t.Fatal("heatmap row too wide")
			}
		}
		if len(b.Rows) > 12 || len(b.Items) > 24 || len(b.Cells) > 50 {
			t.Fatalf("block %s over limits", b.Type)
		}
		for _, row := range b.Cells {
			if len(row) != len(b.Columns) {
				t.Fatal("table row width")
			}
			for _, c := range row {
				checkText(c, 120)
			}
		}
	}
	for _, want := range []string{"stats", "sparkline", "heatmap", "bars", "table"} {
		if !types[want] {
			t.Errorf("view has no %s block", want)
		}
	}
	if v.Blocks[0].Items[2].Tone != "attention" {
		t.Fatal("Needs You with mail waiting is flagged for attention")
	}
	if b := v.Blocks[3]; b.Kind != "category" || b.Items[0].Label != "newsletter" || b.Items[0].Note != "49%" {
		t.Fatalf("category bars = %+v", b)
	}
}

func TestRhythmStartsWeeksOnMonday(t *testing.T) {
	// The fixture's buckets are Monday Sep 21 to Sunday Sep 27: one week.
	b, ok := rhythm(statsFixture(t))
	if !ok || len(b.Rows) != 1 || b.Rows[0].Label != "Sep 21" || b.Rows[0].Values[1] != 30 || b.Rows[0].Values[6] != 1 {
		t.Fatalf("rhythm = %+v", b)
	}
}
