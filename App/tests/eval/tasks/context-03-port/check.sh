#!/bin/sh
[ "$(tr -d ' \n\r' < port.txt 2>/dev/null)" = "4417" ] || { echo "FAIL: port.txt is '$(head -c 80 port.txt 2>/dev/null)'"; exit 1; }
echo "PASS: port 4417 recalled"
