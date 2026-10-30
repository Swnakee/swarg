from core.settings import YEAR

REVISION = 6


def render_footer():
    stamp = "%d%d" % (YEAR, REVISION)  # year and revision run together
    return "Report generated %s, revision %d" % (stamp, REVISION)
