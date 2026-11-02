#!/usr/bin/env python3
"""Attach development configuration to existing Docker infra services."""

import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shlex

from infra import ROOT, endpoint, inventory, values


class Environment:
    def __init__(self, workspace=ROOT):
        self.workspace = Path(workspace).resolve()
        self.project = 'muskdeer-' + hashlib.sha256(str(self.workspace).encode()).hexdigest()[:12]
        self.directory = self.workspace / '.cache/dev'

    def up(self):
        snapshots = {service: inventory(service) for service in ('postgres', 'redis', 'nats', 'nacos')}
        addresses = {service: endpoint(service, info) for service, (_, info) in snapshots.items()}
        postgres = values(snapshots['postgres'][1])
        redis_args = snapshots['redis'][1]['Config'].get('Cmd', []) or []
        redis_password = os.environ.get('MUSKDEER_INFRA_REDIS_PASSWORD', '')
        if '--requirepass' in redis_args:
            redis_password = redis_args[redis_args.index('--requirepass') + 1]
        secrets = {
            'MUSKDEER_DEV_POSTGRES_PASSWORD': postgres['POSTGRES_PASSWORD'],
            'MUSKDEER_DEV_REDIS_PASSWORD': redis_password,
            'MUSKDEER_DEV_NACOS_PASSWORD': os.environ.get('MUSKDEER_INFRA_NACOS_PASSWORD', ''),
        }
        state = {'project': self.project, 'addresses': addresses,
                 'containers': {service: info['Id'] for service, (_, info) in snapshots.items()}}
        self.write('endpoints.json', json.dumps(state, indent=2) + '\n')
        self.write('environment.sh', ''.join('export ' + key + '=' + shlex.quote(value) + '\n' for key, value in secrets.items()))
        for index, role in enumerate(('market', 'strategy', 'execution', 'portfolio')):
            config = json.loads((ROOT / 'configs/local' / (role + '.json')).read_text())
            config['grpc']['address'] = '127.0.0.1:' + str(59002 + index)
            config['monitoring']['address'] = '127.0.0.1:' + str(59102 + index)
            config['postgres'] = {
                'host': '127.0.0.1', 'port': int(addresses['postgres'].rsplit(':', 1)[1]),
                'database': postgres.get('POSTGRES_DB', postgres.get('POSTGRES_USER', 'postgres')),
                'username': postgres.get('POSTGRES_USER', 'postgres'),
                'schema': self.project.replace('-', '_'),
                'password_env': 'MUSKDEER_DEV_POSTGRES_PASSWORD', 'ssl_mode': 'disable',
                'max_open_conns': 4, 'max_idle_conns': 2, 'max_lifetime': '300s',
                'max_idle_time': '60s', 'connect_timeout': '2s', 'statement_timeout': '2s',
            }
            config['redis'] = {
                'address': addresses['redis'], 'password_env': 'MUSKDEER_DEV_REDIS_PASSWORD',
                'tls_enabled': False, 'pool_size': 4, 'pool_timeout': '1s', 'connect_timeout': '2s',
                'command_timeout': '1s', 'max_idle_time': '60s', 'max_lifetime': '300s',
                'key_prefix': self.project + ':' + role,
            }
            # Discovery and business require an explicitly provisioned namespace,
            # credentials and TLS configuration. Attachment never grants them.
            self.write(role + '.json', json.dumps(config, indent=2) + '\n')
        print('Attached existing infra; no container or volume was created', flush=True)
        return state

    def down(self):
        state_path = self.directory / 'endpoints.json'
        if state_path.is_file():
            state = json.loads(state_path.read_text())
            state['attached'] = False
            self.write('endpoints.json', json.dumps(state, indent=2) + '\n')
        print('Development attachment detached; shared infra remains running', flush=True)

    def write(self, name, content):
        self.directory.mkdir(parents=True, exist_ok=True)
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
            yield


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['up', 'down', 'status'])
    action = parser.parse_args().action
    environment = Environment()
    with environment.lock():
        if action == 'up':
            environment.up()
        elif action == 'down':
            environment.down()
        else:
            for service in ('postgres', 'redis', 'nats', 'nacos'):
                name, info = inventory(service)
                print(name + ': running, ' + endpoint(service, info))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
