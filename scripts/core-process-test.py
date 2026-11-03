#!/usr/bin/env python3
"""Verify independent core commands and data on the existing infra services."""

import os
import sys

from infra import run_tests


if __name__ == '__main__':
    os.environ['MUSKDEER_CORE_TEST_BINARIES'] = '1'
    sys.exit(run_tests([
        '-tags=integration', '-race', '-count=1', '-v', '-timeout=3m', '-run',
        '^Test(CoreLocal.*|IndependentBusinessProcessesPublishConsumeAndReconcile|StrategyCommandsDiscoverTakeOverQuoteAndCompleteStop)$',
        './deer/migrate',
    ]))
