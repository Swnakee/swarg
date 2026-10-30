from data.loader import load_rows
from report.builder import build_report


def main():
    rows = load_rows()
    print(build_report(rows))
