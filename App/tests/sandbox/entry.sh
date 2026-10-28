#!/bin/sh
# Container entry: the root filesystem is read-only, $HOME is a tmpfs. Puts the baked
# auto-approve config into the writable config dir (sessions and traces are saved next to it)
# unless SWARG_ASK=1, then runs the command.
if [ "${SWARG_ASK:-0}" != 1 ]; then
    mkdir -p "$HOME/.config/swarg" && cp /etc/swarg-sandbox/swarg/config.json "$HOME/.config/swarg/config.json"
fi
exec "$@"
