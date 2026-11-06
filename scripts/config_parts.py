"""Typed-shape YAML document boundary for provisioning tools, not Go business data."""
import copy
import hashlib
import re
from pathlib import Path

import yaml

DATA_FIELDS = ('postgres', 'redis', 'jetstream', 'venue_connection')
SERVICE_FIELDS = ('name', 'instance_id', 'version', 'startup_timeout', 'shutdown_timeout',
                  'grpc', 'http', 'logging', 'grpc_security', 'grpc_clients', 'monitoring', 'admin_http')


class StrictLoader(yaml.SafeLoader):
    pass


def mapping(loader, node):
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node)
        if not isinstance(key, str) or key in result or key == '<<':
            raise ValueError('configuration mapping keys must be unique strings')
        result[key] = loader.construct_object(value_node)
    return result


def integer(loader, node):
    if not re.fullmatch(r'-?(0|[1-9][0-9]*)', node.value):
        raise ValueError('configuration integer must use decimal syntax')
    return int(node.value)


def no_float(loader, node):
    raise ValueError('decimal configuration values must be quoted strings')


StrictLoader.add_constructor('tag:yaml.org,2002:map', mapping)
StrictLoader.add_constructor('tag:yaml.org,2002:int', integer)
StrictLoader.add_constructor('tag:yaml.org,2002:float', no_float)


def loads(content):
    if len(content.encode()) > 1024 * 1024:
        raise ValueError('configuration exceeds 1 MiB')
    if any(isinstance(token, (yaml.tokens.AnchorToken, yaml.tokens.AliasToken)) for token in yaml.scan(content)):
        raise ValueError('configuration anchors and aliases are unsupported')
    value = yaml.load(content, Loader=StrictLoader)
    if not isinstance(value, dict):
        raise ValueError('configuration document must be a mapping')
    return value


def dumps(value):
    return yaml.safe_dump(value, sort_keys=False, allow_unicode=True)


def split(config):
    if set(config) - set(DATA_FIELDS) - set(SERVICE_FIELDS) - {'nacos'}:
        raise ValueError('unknown runtime configuration fields')
    data = {key: copy.deepcopy(config[key]) for key in ('name', 'version', *DATA_FIELDS) if key in config}
    service = {key: copy.deepcopy(config[key]) for key in SERVICE_FIELDS if key in config}
    registration = config.get('nacos', {}).get('registration')
    if registration is not None:
        service['registration'] = copy.deepcopy(registration)
    body = dumps(data)
    service['data_sha256'] = hashlib.sha256(body.encode()).hexdigest()
    return body, dumps(service)


def combine(data_body, service_body):
    data, service = loads(data_body), loads(service_body)
    if set(data) - {'name', 'version', *DATA_FIELDS} or set(service) - {*SERVICE_FIELDS, 'registration', 'data_sha256'}:
        raise ValueError('fields belong to the wrong configuration document')
    if service.get('data_sha256') != hashlib.sha256(data_body.encode()).hexdigest():
        raise ValueError('configuration pair digest differs')
    if not data.get('name') or data.get('name') != service.get('name') or not data.get('version') or data.get('version') != service.get('version'):
        raise ValueError('configuration document identities differ')
    result = {key: copy.deepcopy(value) for key, value in service.items() if key in SERVICE_FIELDS}
    result.update({key: copy.deepcopy(data[key]) for key in DATA_FIELDS if key in data})
    if service.get('registration') is not None:
        result['nacos'] = {'registration': copy.deepcopy(service['registration'])}
    return result


def load_directory(directory):
    directory = Path(directory)
    return combine((directory / 'data.yaml').read_bytes().decode(), (directory / 'service.yaml').read_bytes().decode())


def ids(prefix):
    return prefix + '.data.yaml', prefix + '.service.yaml'
