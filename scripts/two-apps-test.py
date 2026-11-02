#!/usr/bin/env python3
"""Run this regression using the existing Docker infra services."""

import sys
import os
from infra import run_tests


def run():
    os.environ["MUSKDEER_TWO_APPS_BROWSER"] = "1"
    return run_tests(['-tags=integration', '-race', '-count=1', '-v', '-timeout=3m', '-run', '^Test(OperatorProcessOwnsMarketExecutionStrategyAndPortfolio|TwoApps)', './internal/migrate'], needs=('postgres', 'redis', 'nacos', 'nats'))


if __name__ == '__main__':
    sys.exit(run())
