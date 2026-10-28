#!/usr/bin/env python3
"""Run infrastructure contracts against dedicated temporary test services."""

import os
import json
from pathlib import Path
import subprocess
import sys
import time
import uuid
import urllib.error
import urllib.parse
import urllib.request


ROOT = Path(__file__).resolve().parents[1]


def initialize_naming(address: str) -> None:
    """Provision only the fresh, isolated Compose Nacos test server."""
    base = 'http://' + address + '/nacos'

    def post(path: str, values: dict) -> dict:
        body = urllib.parse.urlencode(values).encode()
        request = urllib.request.Request(base + path, data=body, method='POST')
        with urllib.request.urlopen(request, timeout=5) as response:
            return json.load(response)

    post('/v1/auth/users/admin', {'password': 'muskdeer_nacos_test_only'})
    login = post('/v1/auth/login', {
        'username': 'nacos', 'password': 'muskdeer_nacos_test_only',
    })
    token = login.get('accessToken')
    if not isinstance(token, str) or not token:
        raise ValueError('isolated Nacos login did not return a token')
    for namespace in ['integration-naming', 'integration-other']:
        created = post('/v2/console/namespace', {
            'namespaceId': namespace, 'namespaceName': namespace, 'accessToken': token,
        })
        if created.get('code') != 0 or created.get('data') is not True:
            raise ValueError('isolated Nacos namespace initialization failed')
    request = urllib.request.Request(base + '/v1/console/server/state', headers={'Authorization': 'Bearer ' + token})
    with urllib.request.urlopen(request, timeout=5) as response:
        state = json.load(response)
    if state.get('version') != '2.5.2':
        raise ValueError('isolated Nacos server did not report version 2.5.2')
    verify_naming_ready(base, token)
    print('Nacos server 2.5.2: authenticated namespaces and naming lookup ready', flush=True)


def verify_naming_ready(base: str, token: str) -> None:
    """Bound startup admission by the actual one-second naming-read contract."""
    values = urllib.parse.urlencode({
        'namespaceId': 'integration-naming', 'groupName': 'MUSKDEER_TEST_READINESS',
        'clusterName': 'TEST', 'serviceName': 'deer-readiness.grpc', 'healthyOnly': 'true',
    })
    request = urllib.request.Request(base + '/v1/ns/instance/list?' + values,
                                     headers={'Authorization': 'Bearer ' + token})
    deadline = time.monotonic() + 5
    for _attempt in range(5):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        try:
            with urllib.request.urlopen(request, timeout=min(1, remaining)) as response:
                result = json.load(response)
            if not isinstance(result, dict) or result.get('hosts') != []:
                raise ValueError('isolated Nacos readiness returned unexpected instances')
            return
        except urllib.error.HTTPError:
            raise ValueError('isolated Nacos naming readiness was denied') from None
        except (urllib.error.URLError, TimeoutError):
            remaining = deadline - time.monotonic()
            if remaining > 0:
                time.sleep(min(0.05, remaining))
    raise ValueError('isolated Nacos naming lookup did not become ready')


def published_address(compose: list[str], service: str, port: int) -> str:
    """Read only this isolated project's Docker-assigned loopback binding."""
    result = subprocess.run(
        [*compose, 'port', service, str(port)], check=True,
        capture_output=True, text=True, timeout=10,
    )
    address = result.stdout.strip()
    host, separator, published = address.rpartition(':')
    if separator != ':' or host != '127.0.0.1' or not published.isascii() or not published.isdigit() or not 1 <= int(published) <= 65535:
        raise ValueError('isolated service did not return one loopback TCP binding')
    return address


def capture_diagnostics(compose: list[str]) -> None:
    """Preserve bounded evidence before removing only this disposable project."""
    try:
        diagnostic = subprocess.run([*compose, 'logs', '--no-color', '--tail', '80'], check=False)
        if diagnostic.returncode:
            print('Test container diagnostics failed; preserving the original error', file=sys.stderr)
        # Nacos writes GC and naming diagnostics to files rather than stdout.
        # These are fixed paths inside this run's disposable test container.
        subprocess.run([
            *compose, 'exec', '-T', 'nacos', 'tail', '-n', '60',
            '/home/nacos/logs/nacos_gc.log.0.current',
            '/home/nacos/logs/naming-server.log',
        ], check=False)
    except OSError:
        print('Test container diagnostics unavailable; preserving the original error', file=sys.stderr)


def test_environment(compose: list[str]) -> dict[str, str]:
    """Prepare and address only the caller-owned disposable infrastructure."""
    subprocess.run([*compose, 'exec', '-T', 'postgres', 'postgres', '--version'], check=True)
    subprocess.run([*compose, 'exec', '-T', 'redis', 'redis-server', '--version'], check=True)
    subprocess.run([*compose, 'exec', '-T', 'nats', 'nats-server', '--version'], check=True)
    postgres_address = published_address(compose, 'postgres', 5432)
    redis_address = published_address(compose, 'redis', 6379)
    nats_address = published_address(compose, 'nats', 4222)
    nacos_address = published_address(compose, 'nacos', 8848)
    initialize_naming(nacos_address)
    environment = os.environ.copy()
    postgres_port = postgres_address.rsplit(':', 1)[1]
    environment['MUSKDEER_TEST_POSTGRES_DSN'] = (
        f'host=127.0.0.1 port={postgres_port} user=muskdeer password=muskdeer_test_only '
        'dbname=muskdeer_p01 sslmode=disable'
    )
    environment['MUSKDEER_TEST_POSTGRES_PASSWORD'] = 'muskdeer_test_only'
    environment['MUSKDEER_TEST_POSTGRES_PORT'] = postgres_port
    environment['MUSKDEER_TEST_REDIS_PASSWORD'] = 'muskdeer_redis_test_only'
    environment['MUSKDEER_TEST_REDIS_ADDRESS'] = redis_address
    environment['MUSKDEER_TEST_NATS_TOKEN'] = 'muskdeer_nats_test_only'
    environment['MUSKDEER_TEST_NATS_ADDRESS'] = nats_address
    environment['MUSKDEER_TEST_NACOS_PASSWORD'] = 'muskdeer_nacos_test_only'
    environment['MUSKDEER_TEST_NACOS_ADDRESS'] = nacos_address
    return environment


def run() -> int:
    project = 'muskdeer-p01-' + uuid.uuid4().hex[:10]
    compose = ['docker', 'compose', '-p', project, '-f', str(ROOT / 'compose.test.yml')]
    started = False
    try:
        started = True
        subprocess.run([*compose, 'up', '-d', '--wait', '--wait-timeout', '120'], check=True)
        environment = test_environment(compose)
        result = subprocess.run([
            sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-tags=integration',
            '-count=1', '-v', './internal/persistenceprobe',
        ], cwd=ROOT, env=environment, check=False)
        if result.returncode:
            capture_diagnostics(compose)
            return result.returncode
        result = subprocess.run([
            sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-tags=integration',
            '-race', '-count=1', '-v', './internal/infrastructureprobe', './internal/migrate',
        ], cwd=ROOT, env=environment, check=False)
        if result.returncode:
            capture_diagnostics(compose)
        return result.returncode
    except (OSError, ValueError, subprocess.CalledProcessError, subprocess.TimeoutExpired):
        # These are disposable services with explicit test credentials. Capture
        # only this project's bounded logs before finally removes the evidence.
        capture_diagnostics(compose)
        raise
    finally:
        if started:
            subprocess.run([*compose, 'down', '--volumes'], check=True)


if __name__ == '__main__':
    try:
        sys.exit(run())
    except (OSError, ValueError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        print(f'Infrastructure integration failed: {error}', file=sys.stderr)
        sys.exit(1)
