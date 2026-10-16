# TideMail Plugins

Official plugins for TideMail. Each directory under `plugins/` is an
independently buildable TideMail Plugin API v1 plugin with its own manifest,
version, tests, and documentation.

## Repository layout

```text
plugins/<id>/   plugin source and manifest
examples/       small protocol examples
docs/           contributor and security guidance
scripts/        repository-wide checks
```

## Development workflow

From the TideMail checkout, build the developer CLI and run:

```sh
tidemail plugin validate plugins/<id>
tidemail plugin test plugins/<id>
```

For a Go plugin:

```sh
cd plugins/<id>
go build -buildvcs=false -o <id> .
```

## Installing

In TideMail, open **Settings → Plugins** and press `b` to browse these plugins;
`i` installs, `u` updates, `x` removes. TideMail downloads the latest release
from this repository, checks its checksum, and shows each plugin's permissions
first. You can also build a plugin and copy it into
`$XDG_CONFIG_HOME/tidemail/plugins/<id>/` (or `~/.config/tidemail/plugins/<id>/`)
yourself. The repository never installs or runs anything on its own.

## Releasing

Bump the `version` in each changed plugin's `plugin.toml`, then push a tag:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

The Release workflow tests every plugin, builds Linux and macOS (amd64, arm64)
tarballs with `scripts/package.sh`, writes `catalog.json` and `SHA256SUMS` with
`scripts/catalog`, and publishes them. The catalog format is documented in
TideMail's
[distribution.md](https://github.com/allisonhere/tidemail/blob/main/docs/plugins/distribution.md).

## Included plugins

- [TideMail Smart (JEV)](plugins/smart/README.md)
- [TideMail Analytics](plugins/analytics/README.md): a read-only mail dashboard
  (report plugin)
- [Unsubscribe+](plugins/unsubscribe-plus/README.md): recognizes newsletters,
  automated senders, and sender value; local, offline, never unsubscribes
- [Spam & Phishing Guard](plugins/spam-guard/README.md): warns about phishing,
  spoofed senders, and scam mail; local, offline, never moves or deletes anything
- [PGP (GnuPG)](plugins/pgp/README.md): decrypt and verify received mail, sign
  and encrypt what you send, using your own GnuPG keyring

Only install plugins you trust. Review each manifest's permissions and the
plugin's privacy documentation before installation.
