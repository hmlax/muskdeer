#!/usr/bin/env python3
"""Run infrastructure contracts against dedicated temporary test services."""

import os
import json
import re
from pathlib import Path
import subprocess
import sys
import time
import uuid
import urllib.error
import urllib.parse
import urllib.request


ROOT = Path(__file__).resolve().parents[1]


def migration_batches(inventory: str) -> list[list[str]]:
    """Partition Go's complete runnable inventory, without widening deadlines."""
    names = []
    for line in inventory.splitlines():
        if line.startswith(('Test', 'Example', 'Fuzz')):
            if not re.fullmatch(r'(?:Test|Example|Fuzz)\w*', line):
                raise ValueError('migration inventory contains an invalid runnable name')
            names.append(line)
        elif line and not line.startswith(('Benchmark', 'ok\t', 'ok ', '?\t', '? ')):
            raise ValueError('migration inventory contains unexpected output')
    if not names or len(names) != len(set(names)):
        raise ValueError('migration inventory is empty or contains duplicate entries')
    names.sort()
    return [names[index::2] for index in range(min(2, len(names)))]


def run_migration_tests(environment: dict[str, str]) -> int:
    """Keep every test and race check within two sequential ten-minute batches."""
    command = [sys.executable, str(ROOT / 'scripts/go.py'), 'test',
               '-tags=integration', '-race', '-count=1']
    inventory = subprocess.run(
        [*command, '-list', '.', './deer/migrate'],
        cwd=ROOT, env=environment, capture_output=True, text=True, check=False,
    )
    if inventory.returncode:
        print(inventory.stderr, file=sys.stderr, end='')
        return inventory.returncode
    batches = migration_batches(inventory.stdout)
    for index, names in enumerate(batches, 1):
        print(f'Migration integration batch {index}/{len(batches)}: '
              f'{len(names)} runnable entries; race enabled; deadline 10m', flush=True)
        pattern = '^(' + '|'.join(re.escape(name) for name in names) + ')$'
        result = subprocess.run(
            [*command, '-v', '-timeout=10m', '-run', pattern, './deer/migrate'],
            cwd=ROOT, env=environment, check=False,
        )
        if result.returncode:
            return result.returncode
    return 0


def run() -> int:
    from infra import Infra
    with Infra() as infra:
        environment = infra.environment
        for package in ['./deer/persistenceprobe', './deer/infrastructureprobe']:
            result = subprocess.run([
                sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-tags=integration',
                '-race', '-count=1', '-v', package,
            ], cwd=ROOT, env=environment, check=False)
            if result.returncode:
                return result.returncode
        return run_migration_tests(environment)


if __name__ == '__main__':
    try:
        sys.exit(run())
    except (OSError, ValueError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        print(f'Infrastructure integration failed: {error}', file=sys.stderr)
        sys.exit(1)
