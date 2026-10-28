#!/bin/sh
set -e
python3 gen_version.py > version.txt
test -s version.txt
echo "build ok"
