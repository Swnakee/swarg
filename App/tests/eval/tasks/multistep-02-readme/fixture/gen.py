import hashlib
import sys

try:
    cfg = open("config.ini").read().strip()
except OSError:
    sys.exit("gen.py: config.ini not found")
sig = hashlib.sha1(cfg.encode()).hexdigest()[:8]
open("out.txt", "w").write("MODE:%s sig=%s\n" % (cfg.split("=")[1].upper(), sig))
print("written out.txt")
