import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
import ratelimit  # noqa: E402


class Smoke(unittest.TestCase):
    def test_module_has_bucket(self):
        self.assertTrue(hasattr(ratelimit, "TokenBucket"))


if __name__ == "__main__":
    unittest.main()
