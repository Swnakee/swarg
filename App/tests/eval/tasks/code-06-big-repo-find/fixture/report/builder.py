from .body import render_body
from .footer import render_footer
from .header import render_header


def build_report(rows):
    return "\n".join([render_header(), render_body(rows), render_footer()])
