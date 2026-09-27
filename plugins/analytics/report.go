package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// A report runs in rounds (TideMail's report.run):
//
//	round 1..n  conversations on: page through query.threads, keeping only
//	            the busiest few in state
//	next round  every statistic at once (analytics.*)
//	last round  render the dashboard
//
// TideMail allows 8 rounds; this plugin uses at most maxThreadPages + 2.

const (
	maxThreadPages = 5
	threadPageSize = 500
	topThreads     = 5
	topContacts    = 8
)

// state travels between rounds. TideMail echoes it back verbatim.
type state struct {
	Range         string   `json:"range"`
	Conversations bool     `json:"conversations"`
	From          string   `json:"from"` // start of the range, for thread filters
	Phase         string   `json:"phase"`
	Cursor        string   `json:"cursor,omitempty"`
	Pages         int      `json:"pages"`
	Scanned       int      `json:"scanned"`
	Top           []thread `json:"top,omitempty"`
}

const (
	phaseThreads = "threads"
	phaseStats   = "stats"
)

// thread is one of the busiest conversations.
type thread struct {
	Subject  string `json:"subject"`
	Messages int    `json:"messages"`
	People   int    `json:"people"`
	NeedsYou bool   `json:"needs_you"`
	Waiting  bool   `json:"waiting"`
}

var rangeDays = map[string]int{"7d": 7, "30d": 30, "90d": 90, "365d": 365}

// settingsOf reads this plugin's resolved settings; anything unexpected
// falls back to the defaults.
func settingsOf(settings map[string]any) (rng string, conversations bool) {
	rng, conversations = "30d", true
	if v, ok := settings["range"].(string); ok && rangeDays[v] > 0 {
		rng = v
	}
	if v, ok := settings["conversations"].(bool); ok {
		conversations = v
	}
	return rng, conversations
}

// step answers one report.run round.
func step(rr reportRequest, settings map[string]any) (reportStep, error) {
	if rr.Round <= 1 {
		rng, conversations := settingsOf(settings)
		now, err := time.Parse(time.RFC3339, rr.Context.Now)
		if err != nil {
			now = time.Now()
		}
		st := &state{Range: rng, Conversations: conversations,
			From: now.AddDate(0, 0, -rangeDays[rng]).Format(time.RFC3339)}
		if conversations {
			st.Phase = phaseThreads
			return reportStep{Queries: threadQuery(st), State: st}, nil
		}
		st.Phase = phaseStats
		return reportStep{Queries: statsQueries(st), State: st}, nil
	}

	var st state
	if err := json.Unmarshal(rr.State, &st); err != nil {
		return reportStep{}, fmt.Errorf("report state: %w", err)
	}
	switch st.Phase {
	case phaseThreads:
		var page threadsResult
		if err := json.Unmarshal(rr.Results["threads"], &page); err != nil {
			return reportStep{}, fmt.Errorf("threads result: %w", err)
		}
		st.Pages++
		st.Scanned += len(page.Threads)
		st.Top = busiest(st.Top, page.Threads)
		st.Cursor = page.NextCursor
		if st.Cursor != "" && st.Pages < maxThreadPages {
			return reportStep{Queries: threadQuery(&st), State: &st}, nil
		}
		st.Phase, st.Cursor = phaseStats, ""
		return reportStep{Queries: statsQueries(&st), State: &st}, nil
	case phaseStats:
		var s stats
		if err := s.decode(rr.Results); err != nil {
			return reportStep{}, err
		}
		return reportStep{Report: render(st, s, rr.Context)}, nil
	}
	return reportStep{}, fmt.Errorf("unknown report phase %q", st.Phase)
}

func threadQuery(st *state) map[string]query {
	q := query{
		"method": "query.threads", "scope": "all_cached", "limit": threadPageSize,
		"fields":  []string{"message_count", "latest_subject", "participants", "needs_you", "waiting_on_them"},
		"filters": map[string]any{"date_from": st.From},
	}
	if st.Cursor != "" {
		q["cursor"] = st.Cursor
	}
	return map[string]query{"threads": q}
}

func statsQueries(st *state) map[string]query {
	group := "day"
	if st.Range == "365d" {
		group = "week"
	}
	return map[string]query{
		"volume":     {"method": "analytics.volume", "range": st.Range, "group_by": group},
		"categories": {"method": "analytics.categories", "range": st.Range},
		"attention":  {"method": "analytics.attention"},
		"responses":  {"method": "analytics.response_times", "range": st.Range},
		"contacts":   {"method": "analytics.contacts", "range": st.Range, "limit": topContacts},
	}
}

// busiest merges a page into the running top list.
func busiest(top []thread, page []threadRow) []thread {
	for _, r := range page {
		if r.MessageCount < 2 {
			continue // a single message is not a conversation
		}
		top = append(top, thread{
			Subject: r.LatestSubject, Messages: r.MessageCount, People: len(r.Participants),
			NeedsYou: r.NeedsYou, Waiting: r.WaitingOnThem,
		})
	}
	sort.SliceStable(top, func(i, j int) bool { return top[i].Messages > top[j].Messages })
	if len(top) > topThreads {
		top = top[:topThreads]
	}
	return top
}

// Query results this plugin reads. Unknown fields are ignored, as TideMail
// may add more.

type threadsResult struct {
	Threads    []threadRow `json:"threads"`
	NextCursor string      `json:"next_cursor"`
}

type threadRow struct {
	MessageCount  int    `json:"message_count"`
	LatestSubject string `json:"latest_subject"`
	Participants  []struct {
		Address string `json:"address"`
	} `json:"participants"`
	NeedsYou      bool `json:"needs_you"`
	WaitingOnThem bool `json:"waiting_on_them"`
}

type stats struct {
	Volume struct {
		GroupBy  string `json:"group_by"`
		Received int    `json:"received"`
		Sent     int    `json:"sent"`
		Buckets  []struct {
			Start    string `json:"start"`
			Received int    `json:"received"`
			Sent     int    `json:"sent"`
		} `json:"buckets"`
	}
	Categories struct {
		Total      int `json:"total"`
		Categories []struct {
			Category string `json:"category"`
			Count    int    `json:"count"`
		} `json:"categories"`
	}
	Attention struct {
		NeedsYou  int `json:"needs_you_current"`
		Dismissed int `json:"needs_you_dismissed_current"`
		Waiting   int `json:"waiting_current"`
		Snoozed   int `json:"snoozed_messages_current"`
		SnoozedTh int `json:"snoozed_threads_current"`
	}
	Responses struct {
		UserSamples  int   `json:"user_sample_count"`
		UserMedian   int64 `json:"median_user_response_s"`
		UserAverage  int64 `json:"average_user_response_s"`
		OtherSamples int   `json:"other_sample_count"`
		OtherMedian  int64 `json:"median_other_response_s"`
		OtherAverage int64 `json:"average_other_response_s"`
	}
	Contacts struct {
		Correspondents []contact `json:"correspondents"`
		Domains        []struct {
			Domain string `json:"domain"`
			Total  int    `json:"total"`
		} `json:"domains"`
	}
}

type contact struct {
	Address  string `json:"address"`
	Name     string `json:"name"`
	Received int    `json:"received"`
	Sent     int    `json:"sent"`
	Total    int    `json:"total"`
}

func (s *stats) decode(results map[string]json.RawMessage) error {
	for name, dst := range map[string]any{
		"volume": &s.Volume, "categories": &s.Categories, "attention": &s.Attention,
		"responses": &s.Responses, "contacts": &s.Contacts,
	} {
		raw, ok := results[name]
		if !ok {
			return fmt.Errorf("missing %s result", name)
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return fmt.Errorf("%s result: %w", name, err)
		}
	}
	return nil
}
