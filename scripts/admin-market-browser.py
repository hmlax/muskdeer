#!/usr/bin/env python3
"""Run this regression using the existing Docker infra services."""

import sys
from infra import run_tests


def run():
    return run_tests(['-tags=integration,browser', '-race', '-count=1', '-v', '-timeout=3m', '-run', '^TestAdminMarketBrowserReadsIndependentProcessesAndRetainsSourceOnFailure$', './internal/migrate'], needs=('postgres', 'redis', 'nacos', 'nats'))


if __name__ == '__main__':
    sys.exit(run())
