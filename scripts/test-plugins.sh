#!/usr/bin/env bash
set -euo pipefail

: "${TIDEMAIL_BIN:=tidemail}"

for manifest in plugins/*/plugin.toml; do
    plugin_dir="${manifest%/plugin.toml}"
    "$TIDEMAIL_BIN" plugin validate "$plugin_dir"
    "$TIDEMAIL_BIN" plugin test "$plugin_dir"
done
