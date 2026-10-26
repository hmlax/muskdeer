#!/usr/bin/env python3
"""Install or check version-locked protobuf tools in the project cache."""

import argparse
import json
from pathlib import Path
import shutil
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]


def protoc() -> Path:
    versions = json.loads((ROOT / 'tools/versions.json').read_text())
    binary = ROOT / '.cache/protoc' / versions['protoc'] / 'bin/protoc'
    if binary.is_file():
        return binary
    installed = shutil.which('protoc')
    if installed is None:
        raise ValueError('protoc is missing; run make tools')
    return Path(installed).resolve()


def check(install: bool = False) -> None:
    versions = json.loads((ROOT / 'tools/versions.json').read_text())
    actual = subprocess.check_output([str(protoc()), '--version'], text=True).strip()
    if actual != 'libprotoc ' + versions['protoc']:
        raise ValueError(f'expected libprotoc {versions["protoc"]}, got {actual!r}')
    for plugin in versions['plugins']:
        if install:
            subprocess.run([
                sys.executable, str(ROOT / 'scripts/go.py'), 'install',
                plugin['package'] + '@' + plugin['version'],
            ], check=True)
        binary = ROOT / '.cache/bin' / plugin['name']
        metadata = subprocess.check_output([
            sys.executable, str(ROOT / 'scripts/go.py'), 'version', '-m', str(binary),
        ], text=True)
        expected = f'\tmod\t{plugin["module"]}\t{plugin["version"]}\t'
        if expected not in metadata:
            raise ValueError(f'{plugin["name"]} does not match tools/versions.json; run make tools')
        print(f'{plugin["name"]} {plugin["version"]}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--install', action='store_true')
    arguments = parser.parse_args()
    try:
        check(arguments.install)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Tool verification failed: {error}', file=sys.stderr)
        sys.exit(1)
