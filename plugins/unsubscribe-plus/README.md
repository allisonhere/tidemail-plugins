# Unsubscribe+

A TideMail plugin that recognizes **newsletter and list mail**, flags
**automated senders**, rates **sender value**, and (when TideMail provides
list headers) describes **how you could unsubscribe**. It is local,
deterministic, and read-only.

## What it detects

| Annotation | Meaning | When |
| --- | --- | --- |
| `category=newsletter` | a recommendation that this is newsletter or list mail | strong list evidence (see below) |
| `newsletter=true` | the same finding as a plain fact | same |
| `automated_sender=true` | sent by a robot, not a person | `noreply`, `no-reply`, `donotreply`, `notifications`, `updates`, `alerts`, `mailer`, `bounce(s)`, `postmaster`, `mailer-daemon`, `receipts` local parts; `Auto-Submitted`, `Precedence: bulk/list` |
| `sender_value=low` | bulk or marketing mail | only newsletters |
| `sender_value=high` | automated security mail (fraud alerts, password resets, sign-in codes) | security wording from an automated sender |
| `sender_value=normal` | other automated mail: receipts, shipping, GitHub, calendar | automated, not a newsletter |
| `unsubscribe_available=true` | the message offers an unsubscribe option | valid `List-Unsubscribe` entry |
| `unsubscribe_method=https\|http\|mailto` | the best option, HTTPS preferred | same |
| `unsubscribe_one_click=true` | RFC 8058 one-click unsubscribe | `List-Unsubscribe-Post: List-Unsubscribe=One-Click` with an HTTPS option |

Mail with no evidence gets **no annotations** at all. Negatives such as
`newsletter=false` are never sent, and every annotation carries a heuristic
confidence (a strength, not a calibrated probability).

### Newsletter evidence

Each of these alone marks list mail: a `List-ID` header, a valid
`List-Unsubscribe` header, `Precedence: bulk` or `list`, a newsletter platform
sending domain (Substack, beehiiv, Mailchimp, ConvertKit, Buttondown, Ghost,
SendFox), or a sender named `newsletter`/`digest`. Weaker signals must combine:
a `marketing`/`promotions`/`deals` sender plus a promotional or digest subject
("30% off", "weekly", "roundup", "issue #12"). A subject alone never
classifies, so a colleague's "weekly sync notes" stays untouched.

### Transactional protection

Mail about **security** (sign-in, password, verification, fraud), **billing**
(invoice, receipt, payment, order), **shipping** (shipped, delivered,
tracking), **support** (tickets, cases), **calendar** (invitations,
meetings), and **work** (GitHub/GitLab repository mail, review requests, CI
runs, assignments) is never marked newsletter or low value, even when it is
automated or carries list headers. GitHub notifications, for example, get
`automated_sender=true` and `sender_value=normal`, not `category=newsletter`.

## What you see

Run it with `p` (or **Run plugin** in the command palette). TideMail shows a
result card in plain words, drawn in your theme:

```text
● This looks like a newsletter

List or marketing mail with low expected priority.

Type         Newsletter
Priority     Low
Unsubscribe  Not checked

Why
• The sender is a newsletter or digest address

Confidence   High
```

Security mail reads **Automated message · Type Security alert · Priority
High · Newsletter No**; receipts, shipping, and GitHub mail read **Automated
message** with **Priority Normal**. Press `d` for the raw annotations.

"Unsubscribe: Not checked" means TideMail did not share list headers (Plugin
API v1 does not yet); with headers it reads **Available** (plus the method,
e.g. *One-click secure link*) or **Not found**. There is no Unsubscribe button:
Unsubscribe+ only describes the option.

## What it does not do

- **It never unsubscribes you.** It does not open, fetch, or post to an
  unsubscribe link, and it never sends a `mailto:` message. Classification is
  entirely offline.
- **It never puts unsubscribe links in annotations**, only the method. A
  future TideMail **Unsubscribe** action is meant to show the destination and
  ask for explicit confirmation first.
- It does not move, delete, archive, or flag mail, and does not overwrite
  other plugins' annotations.
- It makes no network requests and sends nothing to external services.

## What data it reads

Only the header metadata TideMail sends every metadata plugin (sender,
recipients, reply-to, subject, date, flags, account, and folder). It never
receives message bodies. Its manifest asks for `message_metadata` and
`annotations` only: no `network`, no `message_body`, no query permissions.

> **Limitation in v0.1.** TideMail's Plugin API v1 does not yet send list
> headers (`List-ID`, `List-Unsubscribe`, `List-Unsubscribe-Post`,
> `Precedence`, `Auto-Submitted`). Until a safe-header capability exists,
> Unsubscribe+ uses the sender and subject heuristics only, and
> `unsubscribe_*` annotations never appear. The header logic is implemented
> and tested, and turns on without other changes once TideMail provides it.

## With TideMail Smart and your corrections

Unsubscribe+ only recommends. If Smart says `category=notification` and
Unsubscribe+ says `category=newsletter`, TideMail's effective classification
decides what is shown, and **your correction always wins**: use **Correct
classification** (command palette or Message annotations) to set the category,
and rerunning either plugin will not undo it.

## False positives

Heuristics can be wrong. Likely cases:

- a company that sends account notices from a `newsletter@` or `digest@`
  address (flagged as a newsletter);
- a small shop's `deals@` address answering a question with "sale" in the
  subject (flagged);
- a newsletter whose subject mentions an invoice, a password, or security
  (protected, so it is *not* flagged);
- a newsletter from a personal-looking address with no list headers (missed
  until TideMail provides list headers).

Correct the category in TideMail, and the correction sticks.

## Build and install

```sh
go build -buildvcs=false -o tidemail-plugin-unsubscribe-plus .
mkdir -p ~/.config/tidemail/plugins/unsubscribe-plus
cp plugin.toml tidemail-plugin-unsubscribe-plus ~/.config/tidemail/plugins/unsubscribe-plus/
```

Restart TideMail. Run it on mail with **Run plugin** in the command palette,
or turn on automatic processing for new mail in **Plugins (experimental)**
(select it, press `a`).

## Develop

```sh
go test ./...                 # fixture corpus, protections, protocol, privacy checks
go test -bench . -run '^$'    # ~8 µs per message
tidemail plugin validate .
tidemail plugin test .
```
