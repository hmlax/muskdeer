#!/usr/bin/env python3
"""Run Go with the project's exact toolchain and repository-local caches."""

import os
from pathlib import Path
import re
import shutil
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]


def run(arguments: list[str]) -> int:
    version = (ROOT / '.go-version').read_text(encoding='utf-8').strip()
    if not re.fullmatch(r'1\.\d+\.\d+', version):
        raise ValueError('.go-version must contain an exact stable Go version')
    module = (ROOT / 'go.mod').read_text(encoding='utf-8')
    declared = re.search(r'^go\s+(\S+)\s*$', module, re.MULTILINE)
    if declared is None or declared.group(1) != version:
        raise ValueError('go.mod and .go-version must declare the same Go version')

    binary = shutil.which('go')
    if binary is None:
        raise ValueError('Go 1.21 or newer is required to bootstrap the pinned toolchain')
    environment = os.environ.copy()
    environment.update({
        'GOTOOLCHAIN': f'go{version}',
        'GOWORK': 'off',
        'GOMODCACHE': str(ROOT / '.cache' / 'gomod'),
        'GOCACHE': str(ROOT / '.cache' / 'gobuild'),
        'GOPATH': str(ROOT / '.cache' / 'gopath'),
        'GOBIN': str(ROOT / '.cache' / 'bin'),
    })
    probe = subprocess.run(
        [binary, 'env', 'GOVERSION'],
        cwd=ROOT,
        env=environment,
        text=True,
        stdout=subprocess.PIPE,
        check=True,
    )
    actual = probe.stdout.strip()
    if actual != f'go{version}':
        raise ValueError(f'expected go{version}, got {actual!r}; command was not executed')
    return subprocess.run(
        [binary, *arguments],
        cwd=ROOT,
        env=environment,
        check=False,
    ).returncode


if __name__ == '__main__':
    try:
        sys.exit(run(sys.argv[1:]))
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Go toolchain check failed: {error}', file=sys.stderr)
        sys.exit(1)
