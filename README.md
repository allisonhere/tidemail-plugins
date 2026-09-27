# tidemail-plugin-smart

A TideMail plugin that tags messages with **needs reply**, **urgent**,
**important**, and a **category**. TideMail shows the tags as badges in the
message list. It is an external plugin, not part of TideMail's mail logic, and
TideMail's plugin support is experimental.

Smart has three modes, chosen in TideMail's plugin settings:

| Mode | What happens | Network |
| --- | --- | --- |
| `local` | Fixed rules on the sender and subject (below). | none |
| `hybrid` (default) | Local rules settle the certain cases; TypeSafe's Jev model judges the rest. | only when Jev is asked |
| `jev` | Jev judges every enabled decision; local signals are passed to it as hints. | every message |

Without a TypeSafe API key, or if TypeSafe is unreachable, `hybrid` and `jev`
fall back to the local rules for that message. A TypeSafe problem never makes
the plugin fail, so TideMail never pauses automatic processing because of it.

- **Metadata only.** TideMail never gives the plugin message bodies
  (`message_body = false`).
- **Standard library only**, including the HTTP client.

## Build and install

Requires Go 1.26 and a TideMail build with plugin support (currently the
`experiment/plugin-system` branch).

```sh
go build -o tidemail-plugin-smart .

mkdir -p ~/.config/tidemail/plugins/smart
cp tidemail-plugin-smart ~/.config/tidemail/plugins/smart/
cp plugin.toml.example ~/.config/tidemail/plugins/smart/plugin.toml
```

Restart TideMail (plugins are discovered at startup).

### Settings

Open **Settings → Advanced → Plugin settings**, select **TideMail Smart**, and
press `s` (or press `s` on it in **Plugins (experimental)** in the command
palette):

| Setting | Default | Meaning |
| --- | --- | --- |
| Auto-process new mail | off | Classify newly arrived mail automatically (asks to confirm). |
| Classification mode | hybrid | `local`, `hybrid`, or `jev`. |
| TypeSafe / Jev | on | Master switch for Jev. |
| TypeSafe API key | not set | Stored in your system keychain by TideMail, never in `config.toml`. |
| Jev model | jev-latest | `jev-latest` follows new releases; `jev-1.13.0` pins a version. |
| Local rules first | on | In hybrid mode, keep certain local categories without asking Jev. |
| Jev: Needs reply / Urgency / Importance / Category | on | Which decisions Jev makes. Off means the local rule decides. |
| Test plugin configuration | | Checks the key and model with one tiny request. |

To use Jev, get an API key from TypeSafe, select the API key row, press
`Enter`, paste it, and press `Enter`. Then run **Test plugin configuration**:
it reports "TypeSafe / Jev connection successful" and the model, or what went
wrong ("TypeSafe rejected the API key"). The key is never shown.

Usage and cost are billed to your TypeSafe account; TideMail does not meter
them. Jev is priced per input token, so Smart keeps requests small.

### Running it

- **By hand:** press `:`, type `plug`, choose **Run plugin on current
  message**, and pick **TideMail Smart**.
- **Automatically:** turn on **Auto-process new mail**. New unread mail is
  classified as it arrives (TideMail never sends old mail or first-sync
  history).

Badges appear before the date:

| Badge | Meaning |
| --- | --- |
| `↩` (`R` with icons off) | needs reply |
| `!` | urgent |
| `◆` (`^`) | important |
| `#github`, `#shipping`, … | category |

**Message annotations** in the palette lists everything the plugin stored on
the message, with confidence. Running the plugin again on the same message
replaces its earlier tags; a message that no longer matches has them cleared.
TideMail shows at most three badges per row (needs reply, urgent, important,
category, in that order), so a message that is all four shows no `#category`
in the row.

## Jev (TypeSafe)

Smart calls TypeSafe's System One API directly:

```http
POST https://api.typesafe.ai/v1/systemone
Authorization: Bearer <your key>
Content-Type: application/json
```

Each message is **one** request with up to four questions, one per enabled
decision:

| Question | Type | Becomes an annotation when |
| --- | --- | --- |
| Does the email ask the recipient personally for a reply, decision, confirmation, or review? (marketing questions, newsletters, receipts, shipping, and automated notices count as no) | Noul | P(yes) ≥ 0.72 → `needs_reply=true` |
| How time-sensitive is it: routine, soon, urgent, critical? | Score | P(urgent) + P(critical) ≥ 0.60 → `urgency=high` |
| Is it important enough to prioritize? (security, account or payment problems, meeting changes, personal requests: yes; routine receipts, newsletters, promotions: no) | Noul | P(yes) ≥ 0.72 → `importance=high` |
| Which category: github, receipt, shipping, security, calendar, newsletter, support, social, billing, notification, personal, or none? | Choice | chosen with P ≥ 0.50 → `category=<choice>`; `none` → no category |

The thresholds are starting points, not calibrated on real mail. The
confidence stored with an annotation is Jev's probability for that answer; it
is not a promise of accuracy.

### What is sent to TypeSafe

Only this, per message:

```json
{
  "from": "Susan <susan@example.com>",
  "subject": "Re: can you review the contract before Friday?",
  "has_attachment": true,
  "is_reply_in_thread": true,
  "automated_sender": false,
  "local_category": "personal"
}
```

`is_reply_in_thread`, `automated_sender`, and `local_category` are worked out
locally from the sender and subject.

**Never sent:** the body or HTML (Smart never receives them), raw headers,
attachments, recipients (`to`, `cc`, `reply_to`), account and folder names,
dates, flags, message IDs, passwords, OAuth tokens, any other API key, or
TideMail's configuration. The TypeSafe key is sent only in the
`Authorization` header.

### How local and Jev combine

- **Hybrid, certain cases:** mail from a known service address (GitHub,
  UPS/FedEx/USPS/DHL, Facebook/LinkedIn/X/Instagram/Reddit/Mastodon/Bluesky/
  Threads) or an explicit security notice keeps its local category. If that
  sender is also automated, Jev is not called at all.
- **Otherwise:** for each decision Jev answered, Jev decides, including
  deciding "no" (so a marketing "Can you believe these deals?" loses the local
  needs-reply guess). Decisions Jev was not asked keep the local result. If
  Jev is unsure of a category (P < 0.5), the local category stays.
- **Failures:** a missing key, a rejected key (401/403), billing (402),
  rate limiting (429), overload (529), server errors, timeouts (3.5 s, below
  TideMail's 5 s plugin limit), or a malformed answer all mean: use the local
  rules for this message and write one line to stderr. No retries, so a slow
  or failing API never delays mail for long.

Smart does not send an `Idempotency-Key`: TypeSafe does not document one, and
Smart does not retry. The internal classifier version is `smart-v2`; it
appears in diagnostics, never in annotations.

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

### Local rules

These always run, and are all that runs in `local` mode. All matching is on whole words of the lowercased subject (after removing
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

For local rules, confidence is the strength of the rule that fired, fixed per
rule. It is not a calibrated probability. When several rules fire for one key,
the strongest wins. For Jev decisions it is Jev's probability for the answer.

## Protocol

TideMail plugin protocol v1: one JSON request on stdin, one JSON response on
stdout, then exit.

- `ping` → `{"message": "pong"}`
- `message.metadata` (manual runs) and `message.received` (automatic runs) →
  `{"annotations": [...]}`, classified identically (an empty array when
  nothing applies)
- `plugin.test` → `{"ok": true|false, "message": "..."}`

Settings arrive in the request's `settings` object. The API key arrives only
in the `TIDEMAIL_SECRET_JEV_API_KEY` environment variable, which TideMail sets
for this plugin's process alone.

Unsupported API versions, request types, methods, or malformed metadata get a
normal `ok: false` response with an `error` code (`unsupported_api`,
`bad_request`, `unsupported_method`, `bad_metadata`) and the same
`request_id`. Input that cannot be read as a request at all (empty, invalid
JSON, no `request_id`) has no ID to answer, so the plugin writes one line to
stderr and exits 1; TideMail shows that as a failed run.

stdout only ever holds the one response.

## Limits

- Only the subject and sender are used, so accuracy is limited, with or
  without Jev. Newsletter detection in particular cannot see
  `List-Unsubscribe` headers or the body.
- Local rules match English keywords only. Jev handles other languages, less
  well than English.
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
`jev_test.go` runs every TypeSafe path against a local `httptest` server;
tests never call the real API (the key variable is cleared in `TestMain`).
