# tidemail-plugin-analytics

**TideMail Analytics** is a report plugin: a mail dashboard drawn from
TideMail's own cache, **in color**. It returns a TideMail structured view
(stat tiles, sparklines, a weekly heatmap, bar charts, a table), and TideMail
draws it in your theme: category bars in your category tag colors, busy days
warming toward the highlight color, attention tiles in your *important* color.
The plugin never chooses a color itself; recolor your theme or tags and the
dashboard follows.

On a TideMail build without structured views it falls back to the plain-text
dashboard below.

```text
╭────────────────────────────────────────────────────────╮
│ TideMail Analytics · last 30 days                      │
╰────────────────────────────────────────────────────────╯

 ┌───────────┐ ┌───────────┐ ┌───────────┐ ┌───────────┐
 │ RECEIVED  │ │   SENT    │ │ NEEDS YOU │ │  WAITING  │
 │    428    │ │    73     │ │    12     │ │     5     │
 └───────────┘ └───────────┘ └───────────┘ └───────────┘

▌Volume per day                            peak 31 · avg 14
  in  ▂▃▅▇▆▄▃▂▅▇█▆▅▃▂▁▂▄▆▇▅▄▃▂▃▅▆▇▅▃
  out ▁▁▂▁▃▂▁▁▂▃▂▁▁▁▁▁▁▂▂▃▂▁▁▁▁▂▂▃▁▁
▌Categories                                   999 received
  newsletter   ██████████████████████████  47% 471
  github       ████████████████▍           30% 298
…
```

Sections: received/sent/Needs You/Waiting tiles, volume sparklines, weekday
rhythm, categories (with your corrections applied), attention right now,
response times, top correspondents and domains, and your busiest
conversations.

## What it reads

It is a `report.run` plugin (TideMail's read-only query API) and asks only
for:

| Permission | Used for |
| --- | --- |
| `analytics_read` | counts and statistics: volume, categories, attention, response times, contacts |
| `threads_query` | the **Busiest conversations** section: message counts, latest subject, and participant count |

- **No message bodies**, no annotations of its own, no metadata events, no
  network, and it cannot change your mail. TideMail runs every query itself;
  the plugin never touches the database.
- It keeps nothing: each report starts from scratch and the only state is
  what TideMail echoes between rounds (the five busiest conversations, a
  page cursor).
- Turn **Busiest conversations** off in the plugin settings to use counts only.
  `threads_query` is still declared, but no thread query is made.

## Settings

| Setting | Values | Default |
| --- | --- | --- |
| Time range | `7d`, `30d`, `90d`, `365d` | `30d` |
| Busiest conversations | on / off | on |

## Build and install

Requires Go 1.26 and a TideMail build with report support (currently the
`experiment/plugin-system` branch).

```sh
go build -buildvcs=false -o tidemail-plugin-analytics .
mkdir -p ~/.config/tidemail/plugins/analytics
cp plugin.toml tidemail-plugin-analytics ~/.config/tidemail/plugins/analytics/
```

Restart TideMail, press `:`, open **Plugins (experimental)**, select
**TideMail Analytics**, and press `enter`.

## Develop

```sh
go test ./...
tidemail plugin validate .
tidemail plugin test .      # runs a full report against a synthetic mailbox
```

## How it works

TideMail runs a report in rounds. With conversations on, the plugin first
pages through `query.threads` (at most 5 pages of 500 conversations in the
range), keeping the five busiest, then asks for every `analytics.*`
statistic in one round, then answers with a structured view (or, when
TideMail's `context.views` is absent, the text dashboard). That is at most 7
of TideMail's 8 rounds. Colors are TideMail's: the view only says which bars
are categories and which numbers need attention.
