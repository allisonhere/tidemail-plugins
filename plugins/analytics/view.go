package main

import (
	"fmt"
	"strings"
	"time"
)

// buildView is the dashboard as a TideMail structured view: semantic blocks
// that TideMail draws in the user's theme colors (category bars in their tag
// colors). The plugin says what things are, never how they look.

type view struct {
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle,omitempty"`
	Blocks   []block `json:"blocks"`
}

type block struct {
	Type    string     `json:"type"`
	Title   string     `json:"title,omitempty"`
	Note    string     `json:"note,omitempty"`
	Items   []item     `json:"items,omitempty"`
	Kind    string     `json:"kind,omitempty"`
	Series  []series   `json:"series,omitempty"`
	Start   string     `json:"start,omitempty"`
	End     string     `json:"end,omitempty"`
	Columns []string   `json:"columns,omitempty"`
	Rows    []heatRow  `json:"rows,omitempty"`
	Cells   [][]string `json:"cells,omitempty"`
	Text    string     `json:"text,omitempty"`
}

type item struct {
	Label string  `json:"label"`
	Value string  `json:"value,omitempty"`
	Count float64 `json:"count,omitempty"`
	Note  string  `json:"note,omitempty"`
	Tone  string  `json:"tone,omitempty"`
}

type series struct {
	Label  string    `json:"label"`
	Values []float64 `json:"values"`
}

type heatRow struct {
	Label  string    `json:"label"`
	Values []float64 `json:"values"`
}

// Limits TideMail enforces on views (docs/plugins/views.md).
const (
	viewLabelMax    = 40
	viewBarsMax     = 24
	viewHeatRows    = 12
	viewTableRows   = 50
	viewCellMax     = 120
	categoriesShown = 10
)

func buildView(st state, s stats, ctx reportContext) *view {
	v := &view{
		Title:    "TideMail Analytics",
		Subtitle: fmt.Sprintf("last %s · %s", rangeName(st.Range), reportTime(ctx)),
	}
	add := func(b block) { v.Blocks = append(v.Blocks, b) }

	// Headline tiles.
	a := s.Attention
	needsTone, waitTone := "positive", "neutral"
	if a.NeedsYou > 0 {
		needsTone = "attention"
	}
	if a.Waiting > 0 {
		waitTone = "attention"
	}
	add(block{Type: "stats", Items: []item{
		{Label: "Received", Value: human(s.Volume.Received), Note: perUnit(s)},
		{Label: "Sent", Value: human(s.Volume.Sent), Note: replyShare(s)},
		{Label: "Needs you", Value: human(a.NeedsYou), Tone: needsTone, Note: fmt.Sprintf("%s dismissed", human(a.Dismissed))},
		{Label: "Waiting", Value: human(a.Waiting), Tone: waitTone, Note: fmt.Sprintf("%s snoozed", human(a.Snoozed+a.SnoozedTh))},
	}})

	// Volume.
	if n := len(s.Volume.Buckets); n > 0 {
		in := make([]float64, n)
		out := make([]float64, n)
		recv := make([]int, n)
		for i, b := range s.Volume.Buckets {
			in[i], out[i], recv[i] = float64(b.Received), float64(b.Sent), b.Received
		}
		unit := "day"
		if s.Volume.GroupBy == "week" {
			unit = "week"
		}
		add(block{Type: "sparkline", Title: "Volume per " + unit,
			Note:   fmt.Sprintf("peak %s · avg %s", human(maxOf(recv)), average(recv)),
			Series: []series{{Label: "in", Values: in}, {Label: "out", Values: out}},
			Start:  shortDate(s.Volume.Buckets[0].Start), End: shortDate(s.Volume.Buckets[n-1].Start)})
	}

	// Weekly rhythm heatmap, from daily buckets.
	if hm, ok := rhythm(s); ok {
		add(hm)
	}

	// Categories, in their tag colors.
	if total := s.Categories.Total; total > 0 {
		b := block{Type: "bars", Title: "Categories", Kind: "category", Note: human(total) + " received"}
		for i, c := range s.Categories.Categories {
			if i == categoriesShown {
				b.Note = fmt.Sprintf("%s received · %d more", human(total), len(s.Categories.Categories)-categoriesShown)
				break
			}
			b.Items = append(b.Items, item{Label: trunc(c.Category, viewLabelMax), Count: float64(c.Count),
				Note: fmt.Sprintf("%.0f%%", float64(c.Count)*100/float64(total))})
		}
		add(b)
	}

	// Response times.
	r := s.Responses
	if r.UserSamples+r.OtherSamples > 0 {
		var items []item
		if r.UserSamples > 0 {
			items = append(items, item{Label: "You reply", Value: duration(r.UserMedian), Tone: speedTone(r.UserMedian),
				Note: "avg " + duration(r.UserAverage) + " · " + countOf(r.UserSamples, "reply", "replies")})
		}
		if r.OtherSamples > 0 {
			items = append(items, item{Label: "They reply", Value: duration(r.OtherMedian),
				Note: "avg " + duration(r.OtherAverage) + " · " + countOf(r.OtherSamples, "reply", "replies")})
		}
		add(block{Type: "stats", Title: "Response times", Note: "median", Items: items})
	}

	// People.
	if people := s.Contacts.Correspondents; len(people) > 0 {
		b := block{Type: "bars", Title: "Top correspondents", Note: "in · out"}
		for _, c := range people[:min(len(people), viewBarsMax)] {
			who := c.Name
			if who == "" {
				who = c.Address
			}
			b.Items = append(b.Items, item{Label: trunc(who, viewLabelMax), Count: float64(c.Total),
				Note: fmt.Sprintf("%s · %s", human(c.Received), human(c.Sent))})
		}
		add(b)
	}
	if domains := s.Contacts.Domains; len(domains) > 0 {
		b := block{Type: "bars", Title: "Top domains"}
		for _, d := range domains[:min(len(domains), 6)] {
			b.Items = append(b.Items, item{Label: trunc(d.Domain, viewLabelMax), Count: float64(d.Total)})
		}
		add(b)
	}

	// Conversations.
	if st.Conversations {
		b := block{Type: "table", Title: "Busiest conversations", Note: human(st.Scanned) + " scanned",
			Columns: []string{"Msgs", "People", "Subject", "State"}}
		for _, t := range st.Top[:min(len(st.Top), viewTableRows)] {
			state := ""
			switch {
			case t.NeedsYou:
				state = "needs you"
			case t.Waiting:
				state = "waiting"
			}
			b.Cells = append(b.Cells, []string{fmt.Sprint(t.Messages), fmt.Sprint(t.People), trunc(orNone(t.Subject), viewCellMax), state})
		}
		if len(b.Cells) == 0 {
			add(block{Type: "text", Title: "Busiest conversations", Text: "No conversations with replies in this period."})
		} else {
			add(b)
		}
	}
	return v
}

// rhythm lays daily received counts out as weeks × weekdays.
func rhythm(s stats) (block, bool) {
	if s.Volume.GroupBy != "day" || len(s.Volume.Buckets) < 7 {
		return block{}, false
	}
	var rows []heatRow
	var cur *heatRow
	for _, b := range s.Volume.Buckets {
		t, err := time.Parse(time.DateOnly, b.Start)
		if err != nil {
			continue
		}
		wd := (int(t.Weekday()) + 6) % 7 // Monday first
		if cur == nil || wd == 0 {
			monday := t.AddDate(0, 0, -wd)
			rows = append(rows, heatRow{Label: monday.Format("Jan 2"), Values: make([]float64, 7)})
			cur = &rows[len(rows)-1]
		}
		cur.Values[wd] = float64(b.Received)
	}
	if len(rows) > viewHeatRows {
		rows = rows[len(rows)-viewHeatRows:]
	}
	return block{Type: "heatmap", Title: "Weekly rhythm", Note: "received · week of",
		Columns: []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}, Rows: rows}, true
}

func perUnit(s stats) string {
	recv := make([]int, len(s.Volume.Buckets))
	for i, b := range s.Volume.Buckets {
		recv[i] = b.Received
	}
	unit := "day"
	if s.Volume.GroupBy == "week" {
		unit = "wk"
	}
	return average(recv) + " / " + unit
}

func replyShare(s stats) string {
	if s.Volume.Received == 0 {
		return ""
	}
	return fmt.Sprintf("%.0f%% of in", float64(s.Volume.Sent)*100/float64(s.Volume.Received))
}

// speedTone praises quick replies (under four hours) without nagging.
func speedTone(median int64) string {
	if median > 0 && median < 4*3600 {
		return "positive"
	}
	return "neutral"
}

func shortDate(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return day
	}
	return strings.TrimSpace(t.Format("Jan 2"))
}
