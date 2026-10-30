from . import storage

HELP = """usage: python3 -m inv <command> [args]
  add <name> <qty>   add qty of an item
  list               print every item
"""


def main(argv):
    if not argv or argv[0] in ("-h", "--help"):
        print(HELP, end="")
        return 0
    cmd, args = argv[0], argv[1:]
    if cmd == "add" and len(args) == 2:
        storage.add(args[0], int(args[1]))
        print("added %s x%s" % (args[0], args[1]))
        return 0
    if cmd == "list":
        for it in storage.load():
            print("%s %d" % (it.name, it.qty))
        return 0
    print("unknown command: %s" % cmd)
    print(HELP, end="")
    return 2
