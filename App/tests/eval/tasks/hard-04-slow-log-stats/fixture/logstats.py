"""Statistics over event logs.

An event is a tuple (ts, user, kind): ts is an int number of seconds, user and kind are strings.
Input lists are sorted by ts (non-decreasing).
"""


def dedupe(events, window):
    """Drop repeated events.

    An event is dropped if a KEPT earlier event has the same (user, kind) and
    event.ts - kept.ts < window. Otherwise it is kept (and later events are compared with it).
    Returns the kept events in their original order.
    """
    kept = []
    for ev in events:
        ts, user, kind = ev
        dup = False
        for k in kept:
            if k[1] == user and k[2] == kind and ts - k[0] < window:
                dup = True
        if not dup:
            kept.append(ev)
    return kept


def rolling_unique(events, window):
    """For each event i return the number of distinct users among the events j <= i
    with ts_j > ts_i - window (the event itself is always included).

    Returns a list of ints with the same length as events.
    """
    out = []
    for i, (ts, _, _) in enumerate(events):
        users = set()
        for j in range(i + 1):
            if events[j][0] > ts - window:
                users.add(events[j][1])
        out.append(len(users))
    return out
