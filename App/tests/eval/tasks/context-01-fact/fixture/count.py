import os
for n in sorted(os.listdir('.')):
    print(n, os.path.getsize(n))
