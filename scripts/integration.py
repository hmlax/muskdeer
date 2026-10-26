#!/usr/bin/env python3
"""Run the persistence contract against a dedicated temporary PostgreSQL."""

import os
from pathlib import Path
import subprocess
import sys
import uuid


ROOT = Path(__file__).resolve().parents[1]


def run() -> int:
    project = 'muskdeer-p01-' + uuid.uuid4().hex[:10]
    compose = ['docker', 'compose', '-p', project, '-f', str(ROOT / 'compose.test.yml')]
    started = False
    try:
        started = True
        subprocess.run([*compose, 'up', '-d', '--wait', '--wait-timeout', '45'], check=True)
        subprocess.run([*compose, 'exec', '-T', 'postgres', 'postgres', '--version'], check=True)
        environment = os.environ.copy()
        environment['MUSKDEER_TEST_POSTGRES_DSN'] = (
            'host=127.0.0.1 port=55439 user=muskdeer password=muskdeer_test_only '
            'dbname=muskdeer_p01 sslmode=disable'
        )
        environment['MUSKDEER_TEST_POSTGRES_PASSWORD'] = 'muskdeer_test_only'
        result = subprocess.run([
            sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-tags=integration',
            '-count=1', '-v', './internal/persistenceprobe',
        ], cwd=ROOT, env=environment, check=False)
        if result.returncode:
            return result.returncode
        return subprocess.run([
            sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-tags=integration',
            '-race', '-count=1', '-v', './internal/infrastructureprobe',
        ], cwd=ROOT, env=environment, check=False).returncode
    finally:
        if started:
            subprocess.run([*compose, 'down', '--volumes'], check=True)


if __name__ == '__main__':
    try:
        sys.exit(run())
    except (OSError, subprocess.CalledProcessError) as error:
        print(f'Persistence integration failed: {error}', file=sys.stderr)
        sys.exit(1)
