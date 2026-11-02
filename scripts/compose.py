#!/usr/bin/env python3
"""Prepare persistent local app resources on existing infra; never deploy infra."""

import argparse
import fcntl
import ipaddress
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import urllib.parse
import urllib.request

from infra import ROOT, endpoint, inventory, literal, values
from nacos_config import ConfigStore, remote_content

DIRECTORY = ROOT / '.cache/compose'
DATABASE = 'muskdeer_local'
NAMESPACE = 'muskdeer-local'
NETWORK = 'infra-network'
SUBNET = ipaddress.ip_network('172.31.240.0/24')
CORES = ('market', 'strategy', 'execution', 'portfolio')
ACCOUNT = 'muskdeer-local-unfunded'


def write(path, content):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_suffix(path.suffix + '.tmp')
    with open(temporary, 'w', opener=lambda name, flags: os.open(name, flags, 0o600)) as stream:
        stream.write(content)
    temporary.chmod(0o600)
    temporary.replace(path)


def run(arguments, **options):
    return subprocess.run(arguments, check=True, capture_output=True, text=True, timeout=30, **options).stdout


def validate_network(snapshots):
    for _, info in snapshots.values():
        if NETWORK not in info['NetworkSettings']['Networks']:
            raise ValueError('all infra containers must already belong to infra-network')
    identifiers = run(['docker', 'network', 'ls', '-q']).split()
    for network in json.loads(run(['docker', 'network', 'inspect', *identifiers])):
        if network['Name'] == 'muskdeer_app':
            if network.get('Labels', {}).get('com.docker.compose.project') != 'muskdeer':
                raise ValueError('application network is owned by another project')
            continue
        for item in network.get('IPAM', {}).get('Config', []) or []:
            subnet = item.get('Subnet')
            if subnet and ipaddress.ip_network(subnet).overlaps(SUBNET):
                raise ValueError('application subnet overlaps an existing Docker network')


def prepare_database(snapshot, state, fresh):
    name, info = snapshot
    owner = values(info).get('POSTGRES_USER', 'postgres')

    def sql(statement):
        return run(['docker', 'exec', '-i', name, 'psql', '-X', '-A', '-t', '-v',
                    'ON_ERROR_STOP=1', '-U', owner, '-d', 'postgres'], input=statement).strip()

    existing = sql("SELECT pg_get_userbyid(datdba)||'|'||coalesce(shobj_description(oid,'pg_database'),'') FROM pg_database WHERE datname='muskdeer_local';")
    role = sql("SELECT coalesce(shobj_description(oid,'pg_authid'),'') FROM pg_roles WHERE rolname='muskdeer_local';")
    role_exists = sql("SELECT rolname FROM pg_roles WHERE rolname='muskdeer_local';")
    marker = 'muskdeer-compose:' + state['ownership']
    if fresh and (existing or role_exists):
        raise ValueError('existing muskdeer_local database/role has no local ownership state; refusing adoption')
    if existing and existing != DATABASE + '|' + marker or role_exists and role != marker:
        raise ValueError('local application database/role ownership does not match this workspace')
    if not role_exists:
        sql('BEGIN; CREATE ROLE muskdeer_local LOGIN PASSWORD ' + literal(state['database_password']) +
            '; COMMENT ON ROLE muskdeer_local IS ' + literal(marker) + '; COMMIT;')
    if not existing:
        sql('CREATE DATABASE muskdeer_local OWNER muskdeer_local;')
        sql('COMMENT ON DATABASE muskdeer_local IS ' + literal(marker) + ';')


def prepare_namespace(snapshot, credential):
    base = 'http://' + endpoint('nacos', snapshot[1]) + '/nacos'
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    token = None

    def request(path, data=None):
        body = None if data is None else urllib.parse.urlencode(data).encode()
        headers = {} if token is None else {'Authorization': 'Bearer ' + token}
        with opener.open(urllib.request.Request(base + path, body, headers), timeout=5) as response:
            return json.load(response)

    login = request('/v1/auth/login', credential)
    token = login.get('accessToken')
    if not token:
        raise ValueError('Nacos authentication failed')
    namespaces = request('/v2/console/namespace/list')
    if namespaces.get('code') != 0:
        raise ValueError('Nacos namespace inventory failed')
    for namespace in namespaces.get('data', []):
        if namespace['namespace'] == NAMESPACE:
            if namespace['namespaceShowName'] != NAMESPACE:
                raise ValueError('existing Nacos namespace belongs to another application')
            return
    result = request('/v2/console/namespace', {'namespaceId': NAMESPACE, 'namespaceName': NAMESPACE})
    if result.get('code') != 0:
        raise ValueError('Nacos namespace creation failed')


def certificates():
    directory = DIRECTORY / 'config/certs'
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    ca_key = DIRECTORY / 'ca.key'
    ca_cert = directory / 'ca.pem'
    if not ca_key.exists():
        run(['openssl', 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256',
             '-nodes', '-days', '365', '-subj', '/CN=Muskdeer local development CA',
             '-keyout', str(ca_key), '-out', str(ca_cert)])
        ca_key.chmod(0o600)
    if not ca_cert.exists():
        raise ValueError('existing local CA certificate is missing; refusing implicit replacement')
    run(['openssl', 'x509', '-in', str(ca_cert), '-checkend', '86400', '-noout'])
    for role in CORES:
        certificate = directory / (role + '.pem')
        key = directory / (role + '.key')
        if certificate.exists() and key.exists():
            run(['openssl', 'x509', '-in', str(certificate), '-checkend', '86400', '-noout'])
            continue
        request = DIRECTORY / (role + '.csr')
        extension = DIRECTORY / (role + '.ext')
        write(extension, 'subjectAltName=DNS:' + role + '.grpc\nextendedKeyUsage=serverAuth,clientAuth\n')
        run(['openssl', 'req', '-new', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256',
             '-nodes', '-subj', '/CN=' + role + '.grpc', '-keyout', str(key), '-out', str(request)])
        key.chmod(0o600)
        run(['openssl', 'x509', '-req', '-in', str(request), '-CA', str(ca_cert), '-CAkey', str(ca_key),
             '-set_serial', str(secrets.randbits(128)), '-days', '364', '-extfile', str(extension),
             '-out', str(certificate)])


def config(role, snapshots, credential, nats, resources=None):
    index = CORES.index(role)
    resources = resources or {}
    result = json.loads((ROOT / 'configs/local' / (role + '.json')).read_text())
    result.update({'instance_id': role + '-compose', 'version': 'infra-core-local',
                   'startup_timeout': '15s', 'shutdown_timeout': '10s'})
    result['grpc']['address'] = '0.0.0.0:' + str(59002 + index)
    result['monitoring']['address'] = '127.0.0.1:' + str(59102 + index)
    result['postgres'] = {
        'host': snapshots['postgres'][0], 'port': 5432, 'database': DATABASE, 'username': DATABASE,
        'password_env': 'MUSKDEER_POSTGRES_PASSWORD', 'schema': 'public', 'ssl_mode': 'disable',
        'max_open_conns': 8, 'max_idle_conns': 2, 'max_lifetime': '300s', 'max_idle_time': '60s',
        'connect_timeout': '3s', 'statement_timeout': '3s',
    }
    result['redis'] = {
        'address': snapshots['redis'][0] + ':6379', 'password_env': 'MUSKDEER_REDIS_PASSWORD',
        'key_prefix': 'muskdeer:local:' + role, 'tls_enabled': False, 'pool_size': 4,
        'pool_timeout': '1s', 'connect_timeout': '3s', 'command_timeout': '1s',
        'max_idle_time': '60s', 'max_lifetime': '300s',
    }
    result['nacos'] = {
        'address': snapshots['nacos'][0] + ':8848', 'namespace_id': NAMESPACE, 'group': 'MUSKDEER_CORE',
        'cluster': 'LOCAL', 'username': credential['username'], 'password_env': 'MUSKDEER_NACOS_PASSWORD',
        'connect_timeout': '5s', 'command_timeout': '2s', 'tls_enabled': False, 'max_inflight': 8,
        'registration': {'advertise_ip': '172.31.240.' + str(10 + index), 'heartbeat_interval': '1s'},
    }
    tls = {'certificate_file': '/app/config/certs/' + role + '.pem',
           'private_key_file': '/app/config/certs/' + role + '.key', 'root_cert_file': '/app/config/certs/ca.pem'}
    peers = {'market': ['strategy.grpc'], 'strategy': ['strategy.grpc'],
             'execution': ['strategy.grpc', 'portfolio.grpc'], 'portfolio': ['execution.grpc']}
    result['grpc_security'] = {'mtls': tls, 'allowed_peer_services': peers[role]}
    result['business'] = {'enabled': True, 'execution_venue': 'simulator',
                          'poll_interval': '0.5s', 'operation_timeout': '1s'}
    if role == 'market':
        result['business']['market'] = {'lease_ttl': '5s', 'feeds': [
            {'feed_id': resources[key], 'replay_bid': '99.99', 'replay_ask': '100.01',
             'replay_quantity': '1', 'replay_interval': '0.1s'}
            for key in ('binance_feed_id', 'okx_feed_id') if key in resources]}
    if role in ('execution', 'portfolio'):
        result['jetstream'] = {
            'address': snapshots['nats'][0] + ':4222', 'username': nats['username'],
            'password_env': 'MUSKDEER_NATS_PASSWORD', 'stream': 'MUSKDEER_LOCAL',
            'subject_prefix': 'muskdeer.local.core', 'tls_enabled': False, 'connect_timeout': '3s',
            'command_timeout': '2s', 'max_inflight': 8, 'max_message_bytes': 65536,
        }
    if role in ('strategy', 'portfolio'):
        result['grpc_clients'] = {'mtls': tls, 'allowed_services': ['execution.grpc'],
                                  'connect_timeout': '2s', 'request_timeout': '2s', 'max_inflight': 16,
                                  'max_request_bytes': 1048576, 'max_response_bytes': 1048576}
    if role == 'strategy':
        result['grpc_clients']['allowed_services'].append('market.grpc')
        result['business']['strategy'] = {'account_ids': [ACCOUNT], 'lease_ttl': '5s'}
    if role == 'execution':
        result['business']['execution_lease_ttl'] = '5s'
        result['business']['risk'] = {'account_ids': [ACCOUNT]}
        result['business']['simulator'] = {'account_ids': [ACCOUNT], 'cancel_delay': '0.1s'}
    if role == 'portfolio':
        result['business']['portfolio_consumer'] = 'portfolio_local'
    return result


def prepare(source):
    if os.getuid() == 0:
        raise ValueError('run local preparation as a non-root user')
    snapshots = {service: inventory(service) for service in ('postgres', 'redis', 'nacos', 'nats')}
    validate_network(snapshots)
    credential = json.loads((ROOT / '.cache/infra/nacos-credentials.json').read_text())
    if not credential.get('username') or not credential.get('password'):
        raise ValueError('local Nacos username/password are required')
    nats = json.loads((ROOT / '.cache/infra/nats-local-credentials.json').read_text())
    if nats.get('username') != 'muskdeer-local' or not nats.get('password'):
        raise ValueError('dedicated persistent muskdeer-local NATS credentials are required; never use test account')
    redis_args = snapshots['redis'][1]['Config'].get('Cmd', []) or []
    redis_password = os.environ.get('MUSKDEER_INFRA_REDIS_PASSWORD', '')
    if '--requirepass' in redis_args:
        redis_password = redis_args[redis_args.index('--requirepass') + 1]
    if not redis_password:
        raise ValueError('existing Redis password is required')
    state_path = DIRECTORY / 'credentials.json'
    fresh = not state_path.exists()
    state = json.loads(state_path.read_text()) if not fresh else {
        'database_password': secrets.token_hex(32), 'ownership': secrets.token_hex(16),
    }
    # Retain credentials after partial setup so a retry never resets a password.
    state['source'] = str(Path(source or state.get('source', ROOT)).resolve())
    write(state_path, json.dumps(state, indent=2) + '\n')
    prepare_database(snapshots['postgres'], state, fresh)
    prepare_namespace(snapshots['nacos'], credential)
    certificates()
    resources_path = DIRECTORY / 'resources.json'
    resources = json.loads(resources_path.read_text()) if resources_path.exists() else None
    store = ConfigStore(NAMESPACE)
    for role in CORES:
        own_config = DIRECTORY / 'config' / role
        desired = config(role, snapshots, credential, nats, resources)
        write(DIRECTORY / 'nacos-drafts' / (role + '.json'), json.dumps(remote_content(desired), indent=2) + '\n')
        if role != 'market' or resources is not None:
            store.publish(role + '.json', desired)
        for name in ('ca.pem', role + '.pem', role + '.key'):
            write(own_config / 'certs' / name, (DIRECTORY / 'config/certs' / name).read_text())
    environment = {'MUSKDEER_POSTGRES_PASSWORD': state['database_password'],
                   'MUSKDEER_REDIS_PASSWORD': redis_password, 'MUSKDEER_NACOS_PASSWORD': credential['password'],
                   'MUSKDEER_NATS_PASSWORD': nats['password']}
    # Compose single-quoted values disable interpolation of credential characters.
    def env_value(value):
        if '\n' in value or '\r' in value or "'" in value:
            raise ValueError('local env value contains unsupported characters')
        return "'" + value + "'"
    write(DIRECTORY / 'secrets.env', ''.join(key + '=' + env_value(value) + '\n' for key, value in environment.items()))
    write(DIRECTORY / 'nacos.env', ''.join(key + '=' + env_value(value) + '\n' for key, value in store.environment(container=True).items()))
    compose_environment = {'MUSKDEER_RUNTIME_DIR': str(DIRECTORY), 'MUSKDEER_UID': str(os.getuid()),
                           'MUSKDEER_GID': str(os.getgid()), 'MUSKDEER_BUILD_CONTEXT': state['source']}
    write(DIRECTORY / 'compose.env', ''.join(key + '=' + env_value(value) + '\n' for key, value in compose_environment.items()))
    print('Prepared existing infra: isolated muskdeer_local database and muskdeer-local namespace; no infra container created')


def bootstrap():
    compose = ['docker', 'compose', '--env-file', str(DIRECTORY / 'compose.env'), '-f', str(ROOT / 'compose.yml')]
    subprocess.run([*compose, 'run', '--rm', '--no-deps', '--entrypoint', '/migrate', 'tools',
                    '-apply'], check=True, timeout=60)
    output = run([*compose, 'run', '--rm', '--no-deps', '--entrypoint', '/coreinit', 'tools'])
    resources = json.loads(output)
    required = ('account_id', 'instrument_id', 'binance_feed_id', 'okx_feed_id')
    if set(resources) != set(required) or resources['account_id'] != ACCOUNT or any(
            not isinstance(resources[key], str) or not resources[key].isdigit() or int(resources[key]) <= 0
            for key in required if key != 'account_id'):
        raise ValueError('local core resource manifest is invalid')
    write(DIRECTORY / 'resources.json', json.dumps(resources, indent=2) + '\n')
    prepare(None)
    print('Core schema, replay feeds and unfunded disabled account ready; no administrator provisioned')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['prepare', 'bootstrap'])
    parser.add_argument('--source', help='explicit Docker build source (default: repository)')
    args = parser.parse_args()
    DIRECTORY.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (DIRECTORY / 'operation.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.action == 'prepare':
            prepare(args.source)
        else:
            bootstrap()


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        # Subprocess stderr can contain configuration; never echo it or secrets.
        print('Local Compose setup failed: ' + (str(error) if isinstance(error, ValueError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
