#!/usr/bin/env python3
"""Generate or compare the four service injectors using the pinned Wire module."""

import argparse
import json
from pathlib import Path
import re
import sys

from go import run


ROOT = Path(__file__).resolve().parents[1]
SERVICES = ('market', 'strategy', 'execution', 'portfolio')


def generate(verify=False):
    version = json.loads((ROOT / 'tools/versions.json').read_text())['wire']
    module = re.search(r'^\s*github\.com/google/wire\s+(\S+)', (ROOT / 'go.mod').read_text(), re.MULTILINE)
    if module is None or module.group(1) != version:
        raise ValueError('go.mod Wire version must match tools/versions.json')
    packages = []
    for service in SERVICES:
        path = ROOT / service / 'cmd'
        if not (path / 'wire.go').is_file():
            raise ValueError('missing service injector: ' + service)
        if verify and not (path / 'wire_gen.go').is_file():
            raise ValueError('missing generated service injector: ' + service)
        packages.append('./' + service + '/cmd')
    return run(['run', '-mod=readonly', 'github.com/google/wire/cmd/wire',
                'diff' if verify else 'gen', *packages])


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    arguments = parser.parse_args()
    try:
        sys.exit(generate(arguments.check))
    except (OSError, ValueError) as error:
        print('Wire generation failed: ' + str(error), file=sys.stderr)
        sys.exit(1)
