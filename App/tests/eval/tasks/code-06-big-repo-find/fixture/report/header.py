from core.settings import TITLE, YEAR
from util.text import underline


def render_header():
    return underline("%s %d" % (TITLE, YEAR))
