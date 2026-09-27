package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// The dashboard is plain text: box drawing, block elements, and sparklines.
// TideMail strips escape sequences and owns colors, so shape carries the
// design. Lines stay within width so the result window never wraps them.

const width = 58

var (
	sparkBlocks = []rune("▁▂▃▄▅▆▇█")
	barEighths  = []rune(" ▏▎▍▌▋▊▉")
)

func render(st state, s stats, ctx reportContext) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	// Title.
	title := fmt.Sprintf("TideMail Analytics · last %s", rangeName(st.Range))
	line("╭%s╮", strings.Repeat("─", width-2))
	line("│ %s │", pad(title, width-4))
	line("╰%s╯", strings.Repeat("─", width-2))
	line("")

	// Tiles.
	tiles := []struct{ label, value string }{
		{"RECEIVED", human(s.Volume.Received)},
		{"SENT", human(s.Volume.Sent)},
		{"NEEDS YOU", human(s.Attention.NeedsYou)},
		{"WAITING", human(s.Attention.Waiting)},
	}
	var top, mid, val, bot []string
	for _, t := range tiles {
		top = append(top, "┌"+strings.Repeat("─", 11)+"┐")
		mid = append(mid, "│"+center(t.label, 11)+"│")
		val = append(val, "│"+center(t.value, 11)+"│")
		bot = append(bot, "└"+strings.Repeat("─", 11)+"┘")
	}
	for _, row := range [][]string{top, mid, val, bot} {
		line(" %s", strings.Join(row, " "))
	}
	line("")

	// Volume sparklines.
	received := make([]int, len(s.Volume.Buckets))
	sent := make([]int, len(s.Volume.Buckets))
	for i, bk := range s.Volume.Buckets {
		received[i], sent[i] = bk.Received, bk.Sent
	}
	if len(received) > 0 {
		unit := "day"
		if s.Volume.GroupBy == "week" {
			unit = "week"
		}
		peak := maxOf(received)
		line("%s", section("Volume per "+unit, fmt.Sprintf("peak %s · avg %s", human(peak), average(received))))
		line("  in  %s", sparkline(received, width-8))
		line("  out %s", sparkline(sent, width-8))
		first, last := s.Volume.Buckets[0].Start, s.Volume.Buckets[len(s.Volume.Buckets)-1].Start
		line("      %s%s", first, leftPad(last, min(width-8, len(received))-utf8.RuneCountInString(first)))
		line("")
	}

	// Weekday rhythm, from daily buckets.
	if s.Volume.GroupBy == "day" && len(s.Volume.Buckets) >= 7 {
		var days [7]int
		for _, bk := range s.Volume.Buckets {
			if t, err := time.Parse(time.DateOnly, bk.Start); err == nil {
				days[(int(t.Weekday())+6)%7] += bk.Received
			}
		}
		line("%s", section("By weekday", "received"))
		peak := maxOf(days[:])
		for i, name := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
			line("  %s %s %s", name, bar(days[i], peak, 40), human(days[i]))
		}
		line("")
	}

	// Categories.
	if total := s.Categories.Total; total > 0 {
		line("%s", section("Categories", human(total)+" received"))
		peak := 0
		for _, c := range s.Categories.Categories {
			peak = max(peak, c.Count)
		}
		for i, c := range s.Categories.Categories {
			if i == 8 {
				line("  … %d more", len(s.Categories.Categories)-8)
				break
			}
			pct := float64(c.Count) * 100 / float64(total)
			line("  %s %s %3.0f%% %s", pad(trunc(c.Category, 12), 12), bar(c.Count, peak, 26), pct, human(c.Count))
		}
		line("")
	}

	// Attention.
	a := s.Attention
	line("%s", section("Attention now", ""))
	line("  needs you %s · waiting %s · snoozed %s · dismissed %s",
		human(a.NeedsYou), human(a.Waiting), human(a.Snoozed+a.SnoozedTh), human(a.Dismissed))
	line("")

	// Response times.
	r := s.Responses
	if r.UserSamples+r.OtherSamples > 0 {
		line("%s", section("Response times", "median · average"))
		if r.UserSamples > 0 {
			line("  you reply   %s · %s  (%s)", pad(duration(r.UserMedian), 7), pad(duration(r.UserAverage), 7), countOf(r.UserSamples, "reply", "replies"))
		}
		if r.OtherSamples > 0 {
			line("  they reply  %s · %s  (%s)", pad(duration(r.OtherMedian), 7), pad(duration(r.OtherAverage), 7), countOf(r.OtherSamples, "reply", "replies"))
		}
		line("")
	}

	// Contacts.
	if people := s.Contacts.Correspondents; len(people) > 0 {
		line("%s", section("Top correspondents", "in · out"))
		peak := people[0].Total
		for _, c := range people {
			who := c.Name
			if who == "" {
				who = c.Address
			}
			line("  %s %s %s · %s", pad(trunc(who, 18), 18), bar(c.Total, peak, 20), leftPad(human(c.Received), 4), human(c.Sent))
		}
		var domains []string
		for i, d := range s.Contacts.Domains {
			if i == 4 {
				break
			}
			domains = append(domains, fmt.Sprintf("%s %s", d.Domain, human(d.Total)))
		}
		for _, l := range joinWrapped("domains: ", domains, " · ", width-2) {
			line("  %s", l)
		}
		line("")
	}

	// Busiest conversations.
	if st.Conversations {
		line("%s", section("Busiest conversations", fmt.Sprintf("%s scanned", human(st.Scanned))))
		if len(st.Top) == 0 {
			line("  no conversations with replies in this period")
		}
		for _, t := range st.Top {
			mark := ""
			switch {
			case t.NeedsYou:
				mark = " ↩"
			case t.Waiting:
				mark = " …"
			}
			line("  %s  %s%s", leftPad(fmt.Sprintf("%d msgs", t.Messages), 8), trunc(orNone(t.Subject), width-16), mark)
		}
		line("  ↩ needs you  … waiting on them")
		line("")
	}

	line("%s", strings.Repeat("─", width))
	line("read-only · counts from TideMail's cache · %s", reportTime(ctx))
	return strings.TrimRight(b.String(), "\n")
}

// section is a heading with a right-aligned note.
func section(title, note string) string {
	if note == "" {
		return "▌" + title
	}
	return "▌" + title + leftPad(note, width-1-utf8.RuneCountInString(title))
}

// sparkline draws values in at most cols columns, summing neighbours when
// there are more values than columns.
func sparkline(values []int, cols int) string {
	values = downsample(values, cols)
	peak := maxOf(values)
	var b strings.Builder
	for _, v := range values {
		i := 0
		if peak > 0 && v > 0 {
			i = max(1, v*(len(sparkBlocks)-1)/peak)
		}
		b.WriteRune(sparkBlocks[i])
	}
	return b.String()
}

func downsample(values []int, cols int) []int {
	if cols <= 0 || len(values) <= cols {
		return values
	}
	group := (len(values) + cols - 1) / cols
	var out []int
	for i := 0; i < len(values); i += group {
		sum := 0
		for _, v := range values[i:min(i+group, len(values))] {
			sum += v
		}
		out = append(out, sum)
	}
	return out
}

// bar draws value/peak as a bar of cols columns with eighth-block precision.
func bar(value, peak, cols int) string {
	if peak <= 0 || value <= 0 {
		return strings.Repeat(" ", cols)
	}
	eighths := value * cols * 8 / peak
	if eighths == 0 {
		eighths = 1 // anything non-zero stays visible
	}
	full, part := eighths/8, eighths%8
	s := strings.Repeat("█", full)
	if part > 0 {
		s += string(barEighths[part])
	}
	return pad(s, cols)
}

func maxOf(values []int) int {
	m := 0
	for _, v := range values {
		m = max(m, v)
	}
	return m
}

// average is the mean, with one decimal below 10 so quiet periods do not
// read as zero.
func average(values []int) string {
	if len(values) == 0 {
		return "0"
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	mean := float64(sum) / float64(len(values))
	if mean < 10 {
		return fmt.Sprintf("%.1f", mean)
	}
	return human(int(mean + 0.5))
}

// human formats n with thousands separators.
func human(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return s
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// duration formats seconds as the two largest units: 45s, 12m, 1h 30m, 2d 4h.
func duration(sec int64) string {
	d := time.Duration(sec) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", sec)
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 {
			return fmt.Sprintf("%dh %dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	}
	days := int(d.Hours()) / 24
	if h := int(d.Hours()) % 24; h > 0 {
		return fmt.Sprintf("%dd %dh", days, h)
	}
	return fmt.Sprintf("%dd", days)
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return human(n) + " " + many
}

func rangeName(r string) string {
	switch r {
	case "7d":
		return "7 days"
	case "90d":
		return "90 days"
	case "365d":
		return "year"
	}
	return "30 days"
}

func reportTime(ctx reportContext) string {
	if t, err := time.Parse(time.RFC3339, ctx.Now); err == nil {
		return t.Format("Jan 2, 15:04")
	}
	return "now"
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no subject)"
	}
	return s
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:max(0, n-1)]) + "…"
}

func pad(s string, n int) string {
	return s + strings.Repeat(" ", max(0, n-utf8.RuneCountInString(s)))
}

func leftPad(s string, n int) string {
	return strings.Repeat(" ", max(0, n-utf8.RuneCountInString(s))) + s
}

func center(s string, n int) string {
	gap := max(0, n-utf8.RuneCountInString(s))
	return strings.Repeat(" ", gap/2) + s + strings.Repeat(" ", gap-gap/2)
}

// joinWrapped joins items with sep after prefix, breaking lines only
// between items so no item is split.
func joinWrapped(prefix string, items []string, sep string, n int) []string {
	if len(items) == 0 {
		return nil
	}
	var out []string
	cur := prefix + items[0]
	for _, it := range items[1:] {
		if utf8.RuneCountInString(cur+sep+it) > n {
			out = append(out, cur)
			cur = strings.Repeat(" ", utf8.RuneCountInString(prefix)) + it
			continue
		}
		cur += sep + it
	}
	return append(out, cur)
}
