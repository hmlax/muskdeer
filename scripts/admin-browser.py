#!/usr/bin/env python3
"""Run this regression using the existing Docker infra services."""

import sys
from infra import run_tests


def run():
    return run_tests(['-tags=integration,browser', '-race', '-count=1', '-v', '-timeout=2m', '-run', '^TestAdmin(CommandAndAPIProcessLoginSessionAndLogout|BrowserUsesActualCLIAndAPI)$', './internal/migrate'], needs=('postgres',))


if __name__ == '__main__':
    sys.exit(run())
