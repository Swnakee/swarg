#!/bin/sh
[ -f site/style.css ] || { echo "FAIL: site/style.css missing$( [ -f style.css ] && echo ' (style.css written outside site/)')"; exit 1; }
n=$(wc -l < site/style.css)
[ "$n" -ge 250 ] || { echo "FAIL: $n lines, want >= 250"; exit 1; }
for sel in ':root' 'header' '.logo' '.nav a:hover' '.hero h1' '.btn' '.cards' '.card h2' 'input:focus' 'button' 'footer' '.dark' '@media (max-width: 768px)' '@media (max-width: 480px)'; do
  grep -qF "$sel" site/style.css || { echo "FAIL: selector '$sel' missing"; exit 1; }
done
o=$(tr -cd '{' < site/style.css | wc -c); c=$(tr -cd '}' < site/style.css | wc -c)
[ "$o" = "$c" ] || { echo "FAIL: braces unbalanced ($o open, $c close): file cut off?"; exit 1; }
echo "PASS: $n lines, all selectors, balanced"
