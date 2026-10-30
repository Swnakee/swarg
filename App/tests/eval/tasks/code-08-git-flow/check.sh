#!/bin/sh
[ -d .git ] || { echo "FAIL: no git repository"; exit 1; }
subjects=$(git log --format=%s 2>/dev/null)
[ "$(echo "$subjects" | grep -c .)" = 2 ] || { echo "FAIL: commits: $(echo "$subjects" | tr '\n' '|')"; exit 1; }
[ "$(echo "$subjects" | tail -n1)" = "initial" ] || { echo "FAIL: first commit is '$(echo "$subjects" | tail -n1)'"; exit 1; }
[ "$(echo "$subjects" | head -n1)" = "fix: greeting typo" ] || { echo "FAIL: second commit is '$(echo "$subjects" | head -n1)'"; exit 1; }
dirty=$(git status --porcelain | grep -v '^?? .eval/')   # .eval/ is the runner's own folder
[ -z "$dirty" ] || { echo "FAIL: working tree not clean: $(echo "$dirty" | tr '\n' ' ')"; exit 1; }
[ "$(python3 greeting.py)" = "Hello, world!" ] || { echo "FAIL: greeting prints '$(python3 greeting.py)'"; exit 1; }
git show HEAD --stat | grep -q greeting.py || { echo "FAIL: the fix commit does not touch greeting.py"; exit 1; }
echo "PASS: two commits, clean tree, typo fixed"
