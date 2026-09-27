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

Plugins are installed separately under
`$XDG_CONFIG_HOME/tidemail/plugins/<id>/` or
`~/.config/tidemail/plugins/<id>/`. The repository does not auto-install or
execute plugins.

## Included plugins

- [TideMail Smart (JEV)](plugins/smart/README.md)

Only install plugins you trust. Review each manifest's permissions and the
plugin's privacy documentation before installation.
