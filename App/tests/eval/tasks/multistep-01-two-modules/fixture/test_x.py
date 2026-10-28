from mathx import clamp
from textx import word_count

assert clamp(5, 0, 10) == 5
assert clamp(-3, 0, 10) == 0
assert clamp(99, 0, 10) == 10
assert word_count("a b  c") == 3
assert word_count("") == 0
print("all tests passed")
