#!/bin/sh
# Container entry: the root filesystem is read-only, $HOME is a tmpfs. Puts the baked
# auto-approve config into the writable config dir (sessions and traces are saved next to it)
# unless SWARG_ASK=1, then runs the command.
if [ "${SWARG_ASK:-0}" != 1 ]; then
    mkdir -p "$HOME/.config/swarg" && cp /etc/swarg-sandbox/swarg/config.json "$HOME/.config/swarg/config.json"
fi
# Prompt variant files put into /work by run.sh (SWARG_PROMPT_DIR) become the blocks dir.
if [ -d /work/.swarg-blocks ]; then
    mkdir -p "$HOME/.config/swarg/blocks" && cp -R /work/.swarg-blocks/. "$HOME/.config/swarg/blocks/" \
        && rm -rf /work/.swarg-blocks || echo "entry.sh: prompt blocks not installed or not removed from /work" >&2
fi
exec "$@"
