#!/usr/bin/env python3
"""Borrow existing infra containers; own only per-run logical test resources."""

import fcntl
import json
import os
from pathlib import Path
import secrets
import signal
import sys
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
PORTS = {'postgres': 5432, 'redis': 6379, 'nats': 4222, 'nacos': 8848}


def inventory(service):
    name = os.environ.get('MUSKDEER_INFRA_' + service.upper() + '_CONTAINER', 'infra-' + service)
    result = subprocess.run(['docker', 'inspect', name], check=True, capture_output=True, text=True, timeout=10)
    value = json.loads(result.stdout)[0]
    if not value['State']['Running']:
        raise ValueError('existing infra service is not running: ' + service)
    return name, value


def endpoint(service, info):
    bindings = info['NetworkSettings']['Ports'].get(str(PORTS[service]) + '/tcp') or []
    for item in bindings:
        if item['HostIp'] in ('127.0.0.1', '0.0.0.0'):
            return '127.0.0.1:' + item['HostPort']
    raise ValueError('existing infra service needs a local published port: ' + service)


def values(info):
    return dict(item.split('=', 1) for item in info['Config'].get('Env', []) or [])


def literal(value):
    return "'" + value.replace('\\', '\\\\').replace("'", "\\'") + "'"


class Infra:
    def __init__(self, needs=('postgres', 'redis', 'nacos', 'nats')):
        self.needs = needs
        self.environment = os.environ.copy()
        self.names = {}
        self.info = {}
        self.database = None
        self.namespaces = []
        self.lock = None
        self.nacos_token = None
        self.nats_ready = False
        self.nacos_probe_user = None
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def __enter__(self):
        # These regressions include fixed stream names. Serialize only Muskdeer
        # test runs, never lock or stop another application's services.
        self.lock = open(Path(tempfile.gettempdir()) / 'muskdeer-infra-tests.lock', 'a')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        try:
            for service in self.needs:
                self.names[service], self.info[service] = inventory(service)
                self.environment['MUSKDEER_TEST_' + service.upper() + '_ADDRESS'] = endpoint(service, self.info[service])
            if 'postgres' in self.needs:
                self.prepare_postgres()
            if 'redis' in self.needs:
                self.prepare_redis()
            if 'nacos' in self.needs:
                self.prepare_nacos()
            if 'nats' in self.needs:
                self.prepare_nats()
            print('Using existing infra: ' + ', '.join(self.names.values()), flush=True)
            return self
        except BaseException as error:
            self.__exit__(type(error), error, error.__traceback__)
            raise

    def sql(self, statement):
        user = values(self.info['postgres']).get('POSTGRES_USER', 'postgres')
        subprocess.run(['docker', 'exec', '-i', self.names['postgres'], 'psql', '-X', '-v', 'ON_ERROR_STOP=1',
                        '-U', user, '-d', 'postgres'], input=statement, check=True,
                       capture_output=True, text=True, timeout=15)

    def prepare_postgres(self):
        config = values(self.info['postgres'])
        user = config.get('POSTGRES_USER', 'postgres')
        password = config.get('POSTGRES_PASSWORD', '')
        if not password:
            raise ValueError('existing infra PostgreSQL password is unavailable')
        database = 'muskdeer_test_' + secrets.token_hex(8)
        self.sql('CREATE DATABASE "' + database + '";')
        self.database = database
        port = self.environment['MUSKDEER_TEST_POSTGRES_ADDRESS'].rsplit(':', 1)[1]
        self.environment.update({
            'MUSKDEER_TEST_POSTGRES_DATABASE': database,
            'MUSKDEER_TEST_POSTGRES_USERNAME': user,
            'MUSKDEER_TEST_POSTGRES_PASSWORD': password,
            'MUSKDEER_TEST_POSTGRES_PORT': port,
            'MUSKDEER_TEST_POSTGRES_DSN': f'host=127.0.0.1 port={port} user={literal(user)} password={literal(password)} dbname={database} sslmode=disable',
        })

    def prepare_redis(self):
        arguments = self.info['redis']['Config'].get('Cmd', []) or []
        password = os.environ.get('MUSKDEER_INFRA_REDIS_PASSWORD', '')
        if '--requirepass' in arguments:
            password = arguments[arguments.index('--requirepass') + 1]
        if not password:
            raise ValueError('existing infra Redis credential is unavailable')
        self.environment['MUSKDEER_TEST_REDIS_PASSWORD'] = password
        self.environment['MUSKDEER_TEST_REDIS_PREFIX'] = 'muskdeer:test:' + secrets.token_hex(8)

    def nacos(self, path, data=None, method=None, authenticated=True):
        body = None if data is None else urllib.parse.urlencode(data).encode()
        headers = {'Authorization': 'Bearer ' + self.nacos_token} if self.nacos_token and authenticated else {}
        request = urllib.request.Request('http://' + self.environment['MUSKDEER_TEST_NACOS_ADDRESS'] + '/nacos' + path,
                                         data=body, headers=headers, method=method)
        with self.opener.open(request, timeout=5) as response:
            return json.load(response)

    def prepare_nacos(self):
        path = ROOT / '.cache/infra/nacos-credentials.json'
        config = json.loads(path.read_text()) if path.is_file() else {}
        user = os.environ.get('MUSKDEER_INFRA_NACOS_USERNAME', config.get('username', 'nacos'))
        password = os.environ.get('MUSKDEER_INFRA_NACOS_PASSWORD', config.get('password', ''))
        if not password:
            raise ValueError('existing infra Nacos credentials are required')
        login = self.nacos('/v1/auth/login', {'username': user, 'password': password})
        self.nacos_token = login.get('accessToken')
        if not self.nacos_token:
            raise ValueError('existing infra Nacos credentials are required')
        self.environment['MUSKDEER_TEST_NACOS_USERNAME'] = user
        self.environment['MUSKDEER_TEST_NACOS_PASSWORD'] = password
        probe = 'muskdeer-auth-' + secrets.token_hex(8)
        probe_password = secrets.token_urlsafe(32)
        result = self.nacos('/v1/auth/users', {'username': probe, 'password': probe_password})
        if result.get('code') not in (0, 200):
            raise ValueError('temporary Nacos auth-probe identity creation failed')
        self.nacos_probe_user = probe
        self.environment['MUSKDEER_TEST_NACOS_REJECT_USERNAME'] = probe
        # Nacos 2.4 refreshes its user cache asynchronously. Observe a real
        # unauthenticated login before testing rejection against this identity.
        deadline = time.monotonic() + 30
        while True:
            try:
                ready = self.nacos('/v1/auth/login', {'username': probe, 'password': probe_password}, authenticated=False)
                if ready.get('accessToken') and not ready.get('globalAdmin'):
                    break
            except urllib.error.HTTPError as error:
                if error.code not in (403, 500):
                    raise
            if time.monotonic() >= deadline:
                raise ValueError('temporary Nacos identity cache did not become ready')
            time.sleep(0.2)


        for suffix, key in [('main', 'MUSKDEER_TEST_NACOS_NAMESPACE'), ('other', 'MUSKDEER_TEST_NACOS_OTHER_NAMESPACE')]:
            namespace = 'muskdeer-test-' + secrets.token_hex(8) + '-' + suffix
            result = self.nacos('/v2/console/namespace', {'namespaceId': namespace, 'namespaceName': namespace})
            if result.get('code') != 0:
                raise ValueError('test namespace creation failed')
            self.namespaces.append(namespace)
            self.environment[key] = namespace

    def prepare_nats(self):
        path = ROOT / '.cache/infra/nats-test-credentials.json'
        config = json.loads(path.read_text()) if path.is_file() else {}
        username = os.environ.get('MUSKDEER_INFRA_NATS_USERNAME', config.get('username', ''))
        password = os.environ.get('MUSKDEER_INFRA_NATS_PASSWORD', config.get('password', ''))
        if not username or not password:
            raise ValueError('existing infra NATS needs dedicated Muskdeer test account credentials')
        self.environment.update({'MUSKDEER_TEST_NATS_USERNAME': username, 'MUSKDEER_TEST_NATS_PASSWORD': password})
        self.environment.pop('MUSKDEER_TEST_NATS_TOKEN', None)
        self.nats_ready = True
        self.clean_nats()

    def clean_nats(self):
        subprocess.run([sys.executable, str(ROOT / 'scripts/go.py'), 'run', './deer/testinfra'],
                       cwd=ROOT, env=self.environment, check=True, timeout=30)

    def __exit__(self, exception_type, exception, traceback):
        failures = []
        try:
            if self.nats_ready:
                try:
                    self.clean_nats()
                except BaseException as error:
                    failures.append(error)
            if self.nacos_probe_user:
                try:
                    result = self.nacos('/v1/auth/users?' + urllib.parse.urlencode({'username': self.nacos_probe_user}), method='DELETE')
                    if result.get('code') not in (0, 200):
                        raise ValueError('temporary Nacos identity cleanup failed')
                except BaseException as error:
                    failures.append(error)
            for namespace in reversed(self.namespaces):
                try:
                    result = self.nacos('/v2/console/namespace?' + urllib.parse.urlencode({'namespaceId': namespace}), method='DELETE')
                    if result.get('code') != 0:
                        raise ValueError('test namespace cleanup failed')
                except BaseException as error:
                    failures.append(error)
            if self.database:
                try:
                    self.sql('DROP DATABASE "' + self.database + '" WITH (FORCE);')
                except BaseException as error:
                    failures.append(error)
        finally:
            if self.lock:
                self.lock.close()
                self.lock = None
        if failures:
            if exception_type is not None:
                print('Logical test resource cleanup failed; original failure preserved', file=sys.stderr)
            else:
                raise RuntimeError('logical test resource cleanup failed') from failures[0]
        return False


def run_tests(arguments, needs=('postgres', 'redis', 'nacos', 'nats'), timeout=240):
    with Infra(needs) as infra:
        process = subprocess.Popen([sys.executable, str(ROOT / 'scripts/go.py'), 'test', *arguments],
                                   cwd=ROOT, env=infra.environment, start_new_session=True)
        try:
            return process.wait(timeout=timeout)
        except BaseException:
            for sig in (signal.SIGTERM, signal.SIGKILL):
                try:
                    os.killpg(process.pid, sig)
                except ProcessLookupError:
                    pass
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    continue
            raise
