#!/usr/bin/env python3
"""Format all maintained Go files, including integration-tagged tests."""

import argparse
from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]


def run(verify: bool) -> int:
    goroot = subprocess.check_output([
        sys.executable, str(ROOT / 'scripts/go.py'), 'env', 'GOROOT',
    ], text=True).strip()
    files = subprocess.check_output([
        'git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z', '--', '*.go',
    ], cwd=ROOT).decode().split('\0')
    files = [path for path in files if path]
    if not files:
        raise ValueError('no Go source files found')
    arguments = [str(Path(goroot) / 'bin/gofmt')]
    if not verify:
        return subprocess.run([*arguments, '-w', *files], cwd=ROOT, check=False).returncode
    unformatted = subprocess.check_output([*arguments, '-l', *files], cwd=ROOT, text=True)
    if unformatted:
        print(unformatted, end='')
        return 1
    return 0


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    arguments = parser.parse_args()
    try:
        sys.exit(run(arguments.check))
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Formatting failed: {error}', file=sys.stderr)
        sys.exit(1)
