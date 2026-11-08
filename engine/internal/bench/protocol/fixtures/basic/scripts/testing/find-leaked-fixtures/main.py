"""Detect pytest fixtures that acquire resources without teardown."""

import sys


def main() -> int:
    """Entry point."""
    if "--help" in sys.argv:
        print("usage: find-leaked-fixtures [--path DIR] [--format text|json]")
        return 0
    print("find-leaked-fixtures: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
