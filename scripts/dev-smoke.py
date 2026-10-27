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


def redis(environment, *args):
    return subprocess.run([
        *environment.compose, 'exec', '-T', 'redis', 'redis-cli',
        '--no-auth-warning', '-a', 'muskdeer_redis_dev_only', *args,
    ], check=True, capture_output=True, text=True).stdout.strip()


def service_smoke(environment):
    path = environment.directory / 'cron.json'
    config = json.loads(path.read_text())
    config['monitoring']['address'] = '127.0.0.1:0'
    path.write_text(json.dumps(config))
    values = os.environ.copy()
    values.update({
        'MUSKDEER_DEV_POSTGRES_PASSWORD': 'muskdeer_dev_only',
        'MUSKDEER_DEV_REDIS_PASSWORD': 'muskdeer_redis_dev_only',
        'MUSKDEER_DEV_NACOS_PASSWORD': 'muskdeer_nacos_dev_only',
    })
    with tempfile.NamedTemporaryFile(mode='w+') as output, tempfile.TemporaryFile(mode='w+') as diagnostic:
        process = subprocess.Popen([
            str(ROOT / '.cache/bin/muskdeer'), '-role', 'cron', '-config', str(path),
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
                        if record.get('msg') == 'service.initialized':
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
    cache = ROOT / '.cache'
    cache.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='dev-smoke-', dir=cache) as directory:
        environment = Environment(Path(directory))
        try:
            first = environment.up()
            # A second up must preserve the existing Nacos admin/namespace.
            again = environment.up()
            if first != again:
                raise ValueError('idempotent development up changed bindings')
            for path in sorted(environment.directory.glob('*.json')):
                if path.name != 'endpoints.json':
                    subprocess.run([sys.executable, str(ROOT / 'scripts/go.py'), 'run', './cmd/doctor', '-config', str(path)], cwd=ROOT, check=True)
            service_smoke(environment)
            if redis(environment, 'SET', 'dev-smoke:retention', 'survives-down') != 'OK':
                raise ValueError('could not write owned retention probe')
            environment.down()
            environment.up()
            if redis(environment, 'GET', 'dev-smoke:retention') != 'survives-down':
                raise ValueError('normal dev-down lost persistent Redis data')
            if redis(environment, 'DEL', 'dev-smoke:retention') != '1':
                raise ValueError('could not remove owned retention probe')
            print(json.dumps({'project': environment.project, 'result': 'persistent development resource checks passed'}))
        finally:
            # Explicit deletion belongs only to this random smoke workspace;
            # the public dev-down command never contains --volumes.
            subprocess.run([*environment.compose, 'down', '--volumes'], check=True)


if __name__ == '__main__':
    sys.exit(main())
