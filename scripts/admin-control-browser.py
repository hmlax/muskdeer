#!/usr/bin/env python3
"""Verify actual funded grid management through owned browser/business processes."""

import subprocess
import sys
import uuid

from integration import ROOT, capture_diagnostics, test_environment
from soak import stop_owned_process


def run() -> int:
    compose = ['docker', 'compose', '-p', 'muskdeer-control-browser-' + uuid.uuid4().hex[:10],
               '-f', str(ROOT / 'compose.test.yml')]
    try:
        subprocess.run([*compose, 'up', '-d', '--wait', '--wait-timeout', '120'], check=True)
        environment = test_environment(compose)
        process = subprocess.Popen([
            sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-tags=integration,browser',
            '-race', '-count=1', '-v', '-timeout=3m', '-run',
            '^TestAdminControlBrowserRunsFundedGridAndKeepsOriginalRequestKey$', './internal/migrate',
        ], cwd=ROOT, env=environment, start_new_session=True)
        try:
            code = process.wait(timeout=240)
        except BaseException:
            stop_owned_process(process)
            raise
        if code:
            stop_owned_process(process)
            capture_diagnostics(compose)
        return code
    finally:
        subprocess.run([*compose, 'down', '--volumes'], check=True)


if __name__ == '__main__':
    try:
        sys.exit(run())
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f'Control browser verification failed: {error}', file=sys.stderr)
        sys.exit(1)
