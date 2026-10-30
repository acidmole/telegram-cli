#!/bin/sh
# Builds telegram-cli from this checkout when the code has changed, then
# runs it with the given arguments. go build is the staleness check: when
# nothing changed it finishes in a moment and leaves the binary alone.
set -e
# Resolve symlinks, so a link to this script on PATH still finds the checkout.
dir=$(dirname "$(readlink -f "$0")")
(cd "$dir" && go build -o telegram-cli .)
exec "$dir/telegram-cli" "$@"
