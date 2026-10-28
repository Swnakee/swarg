#!/bin/sh
python3 -c "
from strutil import is_palindrome as p, shout, reverse
assert p('Racecar') is True
assert p('A man, a plan, a canal: Panama') is True
assert p('hello') is False
assert p('') is True
assert p('ab') is False
assert shout('a') == 'A!' and reverse('ab') == 'ba'
" 2>&1 || { echo "FAIL: is_palindrome missing or wrong"; exit 1; }
echo "PASS: is_palindrome works, old functions kept"
