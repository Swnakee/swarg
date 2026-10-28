from util import calc_total


def summary(items):
    return "items=%d total=%d" % (len(items), calc_total(items))
