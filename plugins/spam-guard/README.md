# Spam & Phishing Guard

A TideMail plugin that **warns** about phishing, spoofed senders, and scam
mail. It tags suspicious messages and explains why in the result card. It is
local and deterministic: it reads header metadata only (sender, Reply-To,
subject, whether there is an attachment, and the sender checks your mail server
recorded), makes no network requests, and never opens links or attachments.

It cannot move or delete mail (plugins can't); it only tells you to be careful.

## What you see

A flagged message gets two tags' worth of information:

| Annotation | Meaning |
| --- | --- |
| `category=suspicious` | the message looks like phishing or spam (shown as a red tag) |
| `risk=medium\|high` | how strong the evidence is |
| `claims_brand=…` | the company the message pretends to be, when it names one |
| `risk_signals=N` | how many separate warning signs were found |

Run it by hand (**Run plugin**, `p`) and the card lists the sender, who it
claims to be, where replies go, what your server said about SPF/DKIM/DMARC, and
up to six plain-language reasons. Run it on a message with no warning signs and
it says "Nothing suspicious found", which is **not** a guarantee.

Messages with no warning signs get **no annotations**, so ordinary mail is
never tagged. Turn on automatic processing in the plugin list to check new mail
as it arrives.

## What it looks for

Each sign adds points; a message is flagged when the total reaches the
threshold for your sensitivity setting.

- **Impersonation:** the sender's name says PayPal, Apple, Microsoft, a bank, a
  courier, and so on, but the address is not one of that company's; the name
  contains a different email address or website; the domain is a look-alike
  (`paypa1.com`, `rnicrosoft.com`), borrows a company's name, or mixes alphabets.
- **Machine-made senders:** a domain that looks randomly generated, an address
  that is mostly digits or the same as its domain, a name written in stylized
  lookalike letters, a name that is your own username.
- **Wording:** pressure and bait in the subject (account suspended, verify your
  account, you have won, gift cards, crypto, wire transfers, "(5) Picture").
- **Where it came from:** free-mail addresses with business-sounding names,
  Reply-To on another domain, a brand named in the subject but sent from a free
  address.
- **What your mail server recorded:** failed SPF, DKIM, or DMARC, and the
  server's own spam flag. These count for little on their own, because
  forwarding and mailing lists make honest mail fail. TideMail passes only the
  pass/fail values, as your server reported them; a server that doesn't strip a
  forged `Authentication-Results` header can be fooled.

Real mail from a company's own domains is never flagged on its name alone, and
a single weak sign never flags a message.

## Settings

**Sensitivity** (`low`, `normal`, `high`; default `normal`). Higher catches more
and may flag some genuine mail; lower flags only the clearest cases. Reclassify
(the plugin list or `p`) after changing it.

## How well does it work?

Checked against a real mailbox of about 1,900 messages at `normal`: no genuine,
trashed, or other-folder mail was flagged, and about a quarter of the messages
Gmail had already put in Spam were (most of the rest are honest newsletters it
misfiled, not scams). It is a set of heuristics, not a filter: it will miss
well-made phishing from a compromised real account, and it can flag the odd
genuine message such as a legitimate notice that names a company it is not from.

## Install

In TideMail, **Settings → Plugins → `b`**, select "Spam & Phishing Guard", `i`.
To build it yourself: `go build -o tidemail-plugin-spam-guard .` and copy the
binary and `plugin.toml` to `~/.config/tidemail/plugins/spam-guard/`.

## Development

`go test ./...`, then `tidemail plugin validate plugins/spam-guard` and
`tidemail plugin test plugins/spam-guard` from a TideMail checkout. Brand and
domain data live in `brands.go`; the signals are in `classify.go`. Genuine-mail
cases in `classify_test.go` must stay unflagged: add one for every false
positive you find.
