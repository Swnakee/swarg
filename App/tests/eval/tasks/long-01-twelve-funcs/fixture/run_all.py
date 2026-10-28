import funcs

for i in range(40):
    print("banner line %02d ------------------------------------------------" % i)
names = sorted((n for n in dir(funcs) if n.startswith("f") and n[1:].isdigit()), key=lambda n: int(n[1:]))
for n in names:
    print("%s(3) = %s" % (n, getattr(funcs, n)(3)))
print("ALL OK (%d functions)" % len(names))
