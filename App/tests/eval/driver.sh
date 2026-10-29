#!/bin/sh
# Runs INSIDE the sandbox container as the command (see run.sh). POSIX sh.
# Input : /work/.eval/{turn-NN.txt,check.sh} put there by eval.py prep.
# Output: /work/.eval/{output.txt,turns.txt,trace.txt,traces/,time.txt,result.txt,check_exit.txt}
# The turn files and check.sh are moved to /tmp/eval before the agent starts, so the
# model cannot find the answer key in /work. Results are copied back at the end.
T=/tmp/eval
mkdir -p "$T" && cp -R /work/.eval/. "$T"/ && rm -rf /work/.eval

# A fresh config dir per task. The sandbox entry put the auto-approve config under
# $HOME/.config/swarg (or $XDG_CONFIG_HOME/swarg); carry it over.
SRC_CFG="${XDG_CONFIG_HOME:-$HOME/.config}"
export HOME=/tmp/home
export XDG_CONFIG_HOME=/tmp/home/.config
mkdir -p "$XDG_CONFIG_HOME"
if [ -n "$SRC_CFG" ] && [ -d "$SRC_CFG/swarg" ]; then cp -R "$SRC_CFG/swarg" "$XDG_CONFIG_HOME/"; fi
# SWARG_EVAL_CONFIG (eval.py prep) replaces the config; it must keep tools.auto_approve true
if [ -f "$T/config.json" ]; then mkdir -p "$XDG_CONFIG_HOME/swarg" && cp "$T/config.json" "$XDG_CONFIG_HOME/swarg/config.json"; fi

cd /work || exit 1
: > "$T/output.txt"
: > "$T/turns.txt"
START=$(date +%s)
n=0
for f in "$T"/turn-*.txt; do
    n=$((n + 1))
    printf '\n=== turn %s ===\n' "$n" >> "$T/output.txt"
    if [ "$n" -eq 1 ]; then
        swarg --base-url "$SWARG_BASE_URL" --model "$SWARG_MODEL" < "$f" >> "$T/output.txt" 2>&1
    else
        swarg --continue --base-url "$SWARG_BASE_URL" --model "$SWARG_MODEL" < "$f" >> "$T/output.txt" 2>&1
    fi
    echo "turn $n exit=$?" >> "$T/turns.txt"
done
END=$(date +%s)

swarg trace < /dev/null > "$T/trace.txt" 2>&1

mkdir -p /work/.eval/traces
find "$XDG_CONFIG_HOME/swarg" -name '*.trace.jsonl' -exec cp {} /work/.eval/traces/ \; 2>/dev/null
cp "$T/output.txt" "$T/turns.txt" "$T/trace.txt" /work/.eval/
echo "$START $END" > /work/.eval/time.txt
sh "$T/check.sh" > /work/.eval/result.txt 2>&1
echo $? > /work/.eval/check_exit.txt
exit 0
