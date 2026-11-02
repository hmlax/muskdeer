#!/usr/bin/env python3
"""Exercise persistent dev resources using a separate disposable workspace."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

from dev import Environment, ROOT


def service_smoke(environment):
    path = environment.directory / 'market.json'
    config = json.loads(path.read_text())
    config['monitoring']['address'] = '127.0.0.1:0'
    config['grpc']['address'] = '127.0.0.1:0'
    path.write_text(json.dumps(config))
    environment.config_store().publish('market.json', config, replace=True)
    values = os.environ.copy()
    for line in (environment.directory / 'environment.sh').read_text().splitlines():
        import shlex
        key, value = line.removeprefix('export ').split('=', 1)
        values[key] = shlex.split(value)[0] if value else ''
    with tempfile.NamedTemporaryFile(mode='w+') as output, tempfile.TemporaryFile(mode='w+') as diagnostic:
        process = subprocess.Popen([
            str(ROOT / '.cache/bin/market'),
        ], env=values, stdout=output, stderr=diagnostic)
        try:
            deadline = time.monotonic() + 8
            address = ''
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise ValueError('development service exited before readiness')
                for line in Path(output.name).read_text().splitlines(keepends=True):
                    if line.endswith('\n'):
                        record = json.loads(line)
                        if record.get('msg') == 'core.initialized':
                            address = record['monitoring_address']
                if address:
                    try:
                        with opener.open('http://' + address + '/readyz', timeout=1) as response:
                            if response.status == 200 and json.load(response).get('status') == 'ready':
                                break
                    except urllib.error.HTTPError as error:
                        if error.code != 503:
                            raise
                    except (urllib.error.URLError, TimeoutError):
                        pass  # Startup has a total budget; no business command is replayed.
                time.sleep(0.02)
            else:
                raise ValueError('development service did not become ready')
            process.terminate()
            if process.wait(timeout=5) != 0:
                raise ValueError('development service shutdown failed')
            diagnostic.seek(0)
            if diagnostic.read():
                raise ValueError('development service wrote unexpected diagnostics')
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)


def main():
    with tempfile.TemporaryDirectory(prefix='muskdeer-dev-attachment-') as directory:
        environment = Environment(Path(directory))
        try:
            first = environment.up()
            second = environment.up()
            if first['containers'] != second['containers']:
                raise ValueError('attachment replaced an existing infra container')
            service_smoke(environment)
            environment.down()
            from infra import inventory
            current = {service: inventory(service)[1]['Id'] for service in first['containers']}
            if current != first['containers']:
                raise ValueError('detaching changed shared infra containers')
            print('Existing infra, Nacos-only configuration and independent market passed')
        finally:
            store = environment.config_store()
            for role in ('market', 'strategy', 'execution', 'portfolio'):
                if store.get(role + '.json') is not None:
                    store.delete(role + '.json')
    return 0


if __name__ == '__main__':
    sys.exit(main())
