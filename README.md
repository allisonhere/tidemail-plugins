# tidemail-plugin-smart

A small, local TideMail plugin that tags messages with **needs reply**,
**urgent**, **important**, and a **category**, using fixed rules on the sender
and subject. TideMail shows the tags as badges in the message list.

It exists to prove TideMail's experimental plugin system end to end. It is not
a smart classifier.

- **Fully local.** No network access, no API calls, no LLM. It declares
  `network = false`.
- **Metadata only.** It reads the sender and subject TideMail sends; it never
  asks for or sees message bodies (`message_body = false`).
- **Deterministic.** The same input always gives the same output. No learning,
  no config, no state kept between runs.
- **Standard library only.**

## Build and install

Requires Go 1.26 and a TideMail build with plugin support (currently the
`experiment/plugin-system` branch).

```sh
go build -o tidemail-plugin-smart .

mkdir -p ~/.config/tidemail/plugins/smart
cp tidemail-plugin-smart ~/.config/tidemail/plugins/smart/
cp plugin.toml.example ~/.config/tidemail/plugins/smart/plugin.toml
```

Restart TideMail (plugins are discovered at startup). Then:

1. Select a message.
2. Open the command palette (`:`), type `plug`, and choose
   **Run plugin on current message**.
3. Pick **TideMail Smart**.

A result window shows what the plugin returned. Close it, and the message row
shows badges before the date, for example:

| Badge | Meaning |
| --- | --- |
| `↩` (`R` with icons off) | needs reply |
| `!` | urgent |
| `◆` (`^`) | important |
| `#github`, `#shipping`, … | category |

**Message annotations** in the palette lists everything the plugin stored on
the message, with confidence. Running the plugin again on the same message
replaces its earlier tags; a message that no longer matches any rule has them
cleared.

TideMail shows at most three badges per row, in the order needs reply, urgent,
important, category, so a message that is all four shows no `#category` badge
in the row (it is still listed under **Message annotations**).

## What it returns

Only positive signals, one annotation per key at most:

| Key | Value | When |
| --- | --- | --- |
| `needs_reply` | `true` | the subject asks the reader to do or answer something |
| `urgency` | `high` | the subject says it is urgent or has a deadline |
| `importance` | `high` | security alerts, account warnings, bills that are due, meeting changes, support replies, explicit requests from a person |
| `category` | one of the categories below | the first rule that matches |

A key that does not apply is left out rather than sent as `false`: TideMail
only draws badges for positive values, and it replaces the plugin's whole set
on every run, so omission already clears a stale tag.

### Rules

All matching is on whole words of the lowercased subject (after removing
`Re:`/`Fwd:` prefixes), so `ups` never matches "groups" and `case` never
matches "showcase".

**Automated senders** are recognized by local parts such as `noreply`,
`no-reply`, `donotreply`, `do-not-reply`, `notifications`, `mailer-daemon`,
`bounce`, `alerts`, `newsletter`, and `digest`, and by `[bot]` display names.
This is a hint, not a verdict: it only switches off the softer reply rules.

**needs_reply**

| Signal | Confidence |
| --- | --- |
| "action required", "response required/needed", "reply needed" | 0.90 (0.70 from an automated sender) |
| "please confirm/review/respond/reply", "need your", "your thoughts", "can/could/would you" | 0.80 |
| "let me know", subject ends in `?` | 0.75 |
| `Re:` from a person | 0.60 |

Everything except the first row is ignored for automated senders, so a GitHub
notification titled "Can you review this PR?" is not marked.

**urgency**

| Signal | Confidence |
| --- | --- |
| "urgent", "critical", "emergency" | 0.95 |
| "asap", "immediately" | 0.90 |
| "time sensitive", "action required" | 0.85 |
| "deadline", "final notice", "due today", "expires today", "overdue" | 0.80 |
| "today" together with "due", "expires", "respond", "reply", "eod" | 0.70 |

"today" on its own never counts, so "Your package is arriving today" is not
urgent.

**importance**

| Signal | Confidence |
| --- | --- |
| category `security`; "action required"; "account suspended/locked/will be/has been"; "final notice" | 0.85 |
| category `billing` with "due", "overdue", "failed", or "declined" | 0.80 |
| category `calendar` with "rescheduled", "canceled", "updated invitation", or "moved" | 0.75 |
| category `support` that is a reply or mentions a response/resolution; "please confirm/review/respond" or "need your" from a person | 0.70 |

Receipts, newsletters, and shipping updates are never important on their own.

**category** (first match wins, in this order)

| # | Category | Signals | Confidence |
| --- | --- | --- | --- |
| 1 | `security` | security alert, new login/sign-in, sign-in attempt, suspicious activity, password changed/reset, verification code, 2FA, two-factor, new device | 0.90 |
| 2 | `calendar` | subject starting "Invitation:"; invitation, meeting, appointment, rescheduled, calendar, RSVP | 0.95 / 0.85 |
| 3 | `github` | sender at `github.com`; the word "GitHub"; "pull request", "workflow run" | 0.98 / 0.90 / 0.70 |
| 4 | `billing` | invoice, payment due, past due, amount due, overdue, billing, statement, payment failed/declined | 0.85 |
| 5 | `receipt` | receipt, order confirmation/confirmed, payment received, thank(s) you for your purchase/order | 0.85 |
| 6 | `shipping` | sender at ups.com, fedex.com, usps.com, dhl.com; shipped, out for delivery, tracking number, delivery update, on its way, delivered, shipment, UPS, FedEx, USPS, DHL | 0.90 / 0.85 |
| 7 | `support` | support request/ticket/case, help desk, incident; "case" or "ticket" followed by a number; sender `support@` or `help@` | 0.85 / 0.80 / 0.75 |
| 8 | `newsletter` | sender containing `newsletter` or `digest`; newsletter, digest, weekly update/roundup, monthly update, "this week in"; sender `news@` | 0.90 / 0.85 / 0.75 |
| 9 | `social` | sender at Facebook, LinkedIn, X/Twitter, Instagram, Reddit, Mastodon, Bluesky, Threads; mentioned you, tagged you, new follower, friend request, commented on your | 0.85 / 0.75 |
| 10 | `notification` | fallback for automated senders | 0.65 |
| 11 | `personal` | a person writing from a consumer mail provider (Gmail, Outlook, Yahoo, iCloud, Proton, Fastmail, HEY) | 0.60 |

Security comes first so a GitHub or bank security alert is `#security`, and
billing before receipt so "receipt and invoice" is `#billing`. Mail from a
person at a company address gets no category rather than a guess.

### Confidence

Confidence is the strength of the rule that fired, fixed per rule. It is not a
calibrated probability. When several rules fire for one key, the strongest
wins.

## Protocol

TideMail plugin protocol v1: one JSON request on stdin, one JSON response on
stdout, then exit.

- `ping` → `{"message": "pong"}`
- `message.metadata` → `{"annotations": [...]}` (an empty array when nothing
  applies)

Unsupported API versions, request types, methods, or malformed metadata get a
normal `ok: false` response with an `error` code (`unsupported_api`,
`bad_request`, `unsupported_method`, `bad_metadata`) and the same
`request_id`. Input that cannot be read as a request at all (empty, invalid
JSON, no `request_id`) has no ID to answer, so the plugin writes one line to
stderr and exits 1; TideMail shows that as a failed run.

stdout only ever holds the one response.

## Limits

- Only the subject and sender are used, so accuracy is limited. Newsletter
  detection in particular cannot see `List-Unsubscribe` headers or the body.
- English keywords only.
- Automated-sender detection is pattern based and can miss or misfire.

## Development

```sh
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
golangci-lint run
```

`classifier_test.go` includes a contract test that checks every annotation the
classifier can produce against TideMail's validation and badge rules.
