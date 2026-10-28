#!/usr/bin/env python3
"""Exercise the actual management API/page against an owned disposable database."""

import os
import subprocess
import sys
import uuid

from integration import ROOT, published_address
from soak import stop_owned_process


def run() -> int:
    compose = ['docker', 'compose', '-p', 'muskdeer-admin-' + uuid.uuid4().hex[:10],
               '-f', str(ROOT / 'compose.test.yml')]
    try:
        subprocess.run([*compose, 'up', '-d', '--wait', '--wait-timeout', '60', 'postgres'], check=True)
        port = published_address(compose, 'postgres', 5432).rsplit(':', 1)[1]
        environment = os.environ.copy()
        environment.update({
            'MUSKDEER_TEST_POSTGRES_DSN': f'host=127.0.0.1 port={port} user=muskdeer password=muskdeer_test_only dbname=muskdeer_p01 sslmode=disable',
            'MUSKDEER_TEST_POSTGRES_PASSWORD': 'muskdeer_test_only',
            'MUSKDEER_TEST_POSTGRES_PORT': port,
        })
        process = subprocess.Popen([
            sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-tags=integration,browser',
            '-race', '-count=1', '-v', '-timeout=2m', '-run',
            '^TestAdmin(CommandAndAPIProcessLoginSessionAndLogout|BrowserUsesActualCLIAndAPI)$',
            './internal/migrate',
        ], cwd=ROOT, env=environment, start_new_session=True)
        try:
            code = process.wait(timeout=180)
        except BaseException:
            stop_owned_process(process)
            raise
        if code:
            stop_owned_process(process)
            subprocess.run([*compose, 'logs', '--no-color', '--tail', '40', 'postgres'], check=False)
        return code
    finally:
        subprocess.run([*compose, 'down', '--volumes'], check=True)


if __name__ == '__main__':
    try:
        sys.exit(run())
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f'Management browser verification failed: {error}', file=sys.stderr)
        sys.exit(1)
