#!/usr/bin/env python3
"""Check that shared code imports no service implementation and each process owns its graph."""

import json
from pathlib import Path
import subprocess
import sys

from proto_layout import validate_layout

ROOT = Path(__file__).resolve().parents[1]
MODULE = 'github.com/hmlax/muskdeer/'
SERVICES = ('market', 'strategy', 'execution', 'portfolio', 'cron')
REMOVED = ('api', 'web', 'cmd/operator', 'deer/api.go', 'deer/testbusiness',
           'deer/testentry', 'deer/testservice', 'configs/local/operator')


def check_packages(packages):
    failures = []
    prefixes = tuple(MODULE + name + '/' for name in SERVICES)
    for package in packages:
        name = package['ImportPath']
        if name.startswith(MODULE + 'deer/') or name == MODULE + 'deer':
            for imported in package.get('Imports', []):
                if imported.startswith(prefixes):
                    failures.append(f'{name} imports service implementation {imported}')
        for service in SERVICES:
            if name != MODULE + service + '/cmd':
                continue
            dependencies = set(package.get('Deps', []))
            if MODULE + service + '/internal/app' not in dependencies:
                failures.append(f'{service} does not own its application graph')
            if service != 'cron' and MODULE + service + '/business' not in dependencies:
                failures.append(f'{service} does not assemble its own business')
            for other in SERVICES:
                if other != service and MODULE + other + '/business' in dependencies:
                    failures.append(f'{service} assembles {other} business')
            for dependency in dependencies:
                if '/testbusiness' in dependency or '/testservice' in dependency:
                    failures.append(f'{service} imports regression fixture {dependency}')
    return failures


def run():
    validate_layout(ROOT)
    failures = [f'removed entry still exists: {name}' for name in REMOVED if (ROOT / name).exists()]
    output = subprocess.check_output([sys.executable, str(ROOT / 'scripts/go.py'),
                                      'list', '-json', './...'], cwd=ROOT, text=True)
    decoder = json.JSONDecoder()
    packages = []
    while output.strip():
        package, end = decoder.raw_decode(output.lstrip())
        packages.append(package)
        output = output.lstrip()[end:]
    failures.extend(check_packages(packages))
    if failures:
        raise ValueError('\n'.join(failures))
    print('Shared packages and five independent service dependency graphs verified')


if __name__ == '__main__':
    try:
        run()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Service layout failed: {error}', file=sys.stderr)
        sys.exit(1)
