#!/usr/bin/env python3
"""Build one executable with explicit main-package metadata and the pinned Go."""

from datetime import datetime, timezone
import os
from pathlib import Path
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]


def git_value(arguments, fallback):
    try:
        result = subprocess.run(['git', *arguments], cwd=ROOT, capture_output=True, text=True, check=False)
    except FileNotFoundError:
        return fallback
    return result.stdout.strip() if result.returncode == 0 else fallback


def metadata(name, environment):
    version = environment.get('VERSION') or git_value(['describe', '--tags', '--always', '--dirty'], 'dev')
    build = environment.get('BUILD')
    if not build:
        revision = git_value(['rev-parse', '--short=12', 'HEAD'], 'unknown')
        build = revision + '@' + datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    values = {'Name': environment.get('NAME') or name, 'Version': version, 'Build': build}
    for key, value in values.items():
        if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9._+:/@-]*', value):
            raise ValueError(f'{key} must be a nonempty build metadata token without whitespace or shell syntax')
    return values


def command(name, package, output, environment):
    values = metadata(name, environment)
    flags = ' '.join(f'-X main.{key}={value}' for key, value in values.items())
    return [sys.executable, str(ROOT / 'scripts/go.py'), 'build', '-trimpath',
            '-ldflags', flags, '-o', output, package]


def main(arguments):
    if len(arguments) != 3:
        raise ValueError('usage: build.py NAME PACKAGE OUTPUT')
    name, package, output = arguments
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    return subprocess.run(command(name, package, output, os.environ), cwd=ROOT, check=False).returncode


if __name__ == '__main__':
    try:
        sys.exit(main(sys.argv[1:]))
    except (OSError, ValueError) as error:
        print(f'Build failed: {error}', file=sys.stderr)
        sys.exit(1)
