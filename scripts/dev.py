#!/usr/bin/env python3
"""Own a workspace-specific development Compose project; down retains data."""

import argparse
import contextlib
import fcntl
import hashlib
import json
from pathlib import Path
import shlex
import subprocess
import sys
import urllib.parse
import urllib.request

from integration import published_address


ROOT = Path(__file__).resolve().parents[1]
PASSWORD = 'muskdeer_nacos_dev_only'
NAMESPACE = 'muskdeer-development'
SERVICES = {'postgres': 5432, 'redis': 6379, 'nats': 4222, 'nacos': 8848}


class Environment:
    def __init__(self, workspace: Path = ROOT):
        self.workspace = workspace.resolve()
        self.project = 'muskdeer-dev-' + hashlib.sha256(str(self.workspace).encode()).hexdigest()[:12]
        self.compose = ['docker', 'compose', '-p', self.project, '-f', str(ROOT / 'compose.dev.yml')]
        self.directory = self.workspace / '.cache' / 'dev'

    def up(self) -> dict:
        # Never replace or migrate an existing database. Compose uses only
        # volumes belonging to the project derived from this workspace path.
        subprocess.run([*self.compose, 'up', '-d', '--wait', '--wait-timeout', '120'], check=True)
        addresses = {service: published_address(self.compose, service, port) for service, port in SERVICES.items()}
        provision_naming(addresses['nacos'])
        self.directory.mkdir(parents=True, exist_ok=True)
        state = {'project': self.project, 'addresses': addresses, 'namespace': NAMESPACE}
        self.write('endpoints.json', json.dumps(state, indent=2) + '\n')
        values = {
            'MUSKDEER_DEV_POSTGRES_PASSWORD': 'muskdeer_dev_only',
            'MUSKDEER_DEV_REDIS_PASSWORD': 'muskdeer_redis_dev_only',
            'MUSKDEER_DEV_NATS_TOKEN': 'muskdeer_nats_dev_only',
            'MUSKDEER_DEV_NACOS_PASSWORD': PASSWORD,
        }
        self.write('environment.sh', ''.join(f'export {key}={shlex.quote(value)}\n' for key, value in values.items()))
        for role in ['api', 'market', 'strategy', 'execution', 'portfolio', 'cron']:
            config = json.loads((ROOT / 'configs' / 'local' / (role + '.json')).read_text())
            config['postgres'] = {
                'host': '127.0.0.1', 'port': int(addresses['postgres'].rsplit(':', 1)[1]),
                'database': 'muskdeer_dev', 'username': 'muskdeer',
                'password_env': 'MUSKDEER_DEV_POSTGRES_PASSWORD', 'ssl_mode': 'disable',
                'max_open_conns': 4, 'max_idle_conns': 2, 'max_lifetime': '300s',
                'max_idle_time': '60s', 'connect_timeout': '2s', 'statement_timeout': '2s',
            }
            config['redis'] = {
                'address': addresses['redis'], 'password_env': 'MUSKDEER_DEV_REDIS_PASSWORD',
                'tls_enabled': False, 'pool_size': 4, 'pool_timeout': '1s', 'connect_timeout': '2s',
                'command_timeout': '1s', 'max_idle_time': '60s', 'max_lifetime': '300s',
                'key_prefix': 'development:' + role,
            }
            config['nacos'] = {
                'address': addresses['nacos'], 'tls_enabled': False, 'namespace_id': NAMESPACE,
                'group': 'MUSKDEER_DEV', 'cluster': 'LOCAL', 'username': 'nacos',
                'password_env': 'MUSKDEER_DEV_NACOS_PASSWORD', 'connect_timeout': '2s',
                'command_timeout': '2s', 'max_inflight': 4,
            }
            if 'grpc' in config:
                config['nacos']['registration'] = {'advertise_ip': '127.0.0.1', 'heartbeat_interval': '1s'}
            # JetStream is available, but no business stream is provisioned
            # before P03/P04 define its contract. Do not enable a false client.
            self.write(role + '.json', json.dumps(config, indent=2) + '\n')
        print(f'Development resources ready: {self.directory / "endpoints.json"}', flush=True)
        return state

    def down(self) -> None:
        # No --volumes and no state-file-provided project identifier.
        subprocess.run([*self.compose, 'down', '--timeout', '10'], check=True)
        print('Development containers stopped; named volumes retained', flush=True)

    def write(self, name: str, content: str) -> None:
        destination = self.directory / name
        temporary = destination.with_suffix(destination.suffix + '.tmp')
        temporary.write_text(content)
        temporary.chmod(0o600)
        temporary.replace(destination)

    @contextlib.contextmanager
    def lock(self):
        self.directory.mkdir(parents=True, exist_ok=True)
        with (self.directory / 'operation.lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            try:
                yield
            finally:
                fcntl.flock(lock, fcntl.LOCK_UN)


def provision_naming(address: str) -> None:
    base = 'http://' + address + '/nacos'
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(path: str, values=None, token=''):
        body = None if values is None else urllib.parse.urlencode(values).encode()
        headers = {'Authorization': 'Bearer ' + token} if token else {}
        call = urllib.request.Request(base + path, data=body, headers=headers)
        with opener.open(call, timeout=5) as response:
            return json.load(response)

    # Pinned 2.5.2 returns code=409 once admin exists; never change its password.
    initialized = request('/v1/auth/users/admin', {'password': PASSWORD})
    if initialized.get('code') not in (None, 409) or initialized.get('code') is None and initialized.get('username') != 'nacos':
        raise ValueError('development Nacos admin initialization failed')
    login = request('/v1/auth/login', {'username': 'nacos', 'password': PASSWORD})
    token = login.get('accessToken')
    if not isinstance(token, str) or not token:
        raise ValueError('development Nacos login failed')
    state = request('/v1/console/server/state', token=token)
    if state.get('version') != '2.5.2':
        raise ValueError('development Nacos version does not match 2.5.2')
    namespaces = request('/v2/console/namespace/list', token=token)
    if namespaces.get('code') != 0 or not isinstance(namespaces.get('data'), list):
        raise ValueError('development Nacos namespace lookup failed')
    if not any(item.get('namespace') == NAMESPACE for item in namespaces['data']):
        created = request('/v2/console/namespace', {'namespaceId': NAMESPACE, 'namespaceName': NAMESPACE}, token)
        if created.get('code') != 0 or created.get('data') is not True:
            raise ValueError('development Nacos namespace creation failed')


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['up', 'down', 'status'])
    args = parser.parse_args()
    environment = Environment()
    try:
        with environment.lock():
            if args.action == 'up':
                environment.up()
            elif args.action == 'down':
                environment.down()
            else:
                subprocess.run([*environment.compose, 'ps'], check=True)
    except (OSError, ValueError, subprocess.SubprocessError):
        print('Development resource operation failed; existing volumes are retained', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
