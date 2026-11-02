"""Explicit local configuration provisioning on the existing infra Nacos."""

import copy
import json
import os
import urllib.error
import urllib.parse
import urllib.request

from infra import ROOT, endpoint, inventory


def remote_content(config):
    result = copy.deepcopy(config)
    registration = result.pop('nacos', {}).get('registration')
    if registration:
        result['nacos'] = {'registration': registration}
    return result


class ConfigStore:
    def __init__(self, namespace='muskdeer-local', group='MUSKDEER_CORE', cluster='LOCAL'):
        self.container, self.info = inventory('nacos')
        self.address = endpoint('nacos', self.info)
        self.namespace, self.group, self.cluster = namespace, group, cluster
        path = ROOT / '.cache/infra/nacos-credentials.json'
        local = json.loads(path.read_text()) if path.exists() else {}
        self.username = os.environ.get('MUSKDEER_INFRA_NACOS_USERNAME', local.get('username', ''))
        self.password = os.environ.get('MUSKDEER_INFRA_NACOS_PASSWORD', local.get('password', ''))
        if not self.username or not self.password:
            raise ValueError('existing Nacos credentials are required')
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.token = None

    def request(self, method, path, values=None, missing=False):
        address = 'http://' + self.address + '/nacos' + path
        values = urllib.parse.urlencode(values or {})
        body = None
        if method in ('GET', 'DELETE'):
            address += '?' + values
        else:
            body = values.encode()
        headers = {} if self.token is None else {'Authorization': 'Bearer ' + self.token}
        try:
            with self.opener.open(urllib.request.Request(address, body, headers, method=method), timeout=5) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            if missing and error.code == 404:
                return None
            raise RuntimeError('Nacos configuration operation failed (HTTP ' + str(error.code) + ')') from None

    def login(self):
        if self.token is None:
            response = self.request('POST', '/v1/auth/login', {'username': self.username, 'password': self.password})
            self.token = response.get('accessToken')
            if not self.token:
                raise ValueError('Nacos authentication failed')

    def scope(self, data_id):
        return {'namespaceId': self.namespace, 'group': self.group, 'dataId': data_id}

    def ensure_namespace(self):
        self.login()
        result = self.request('GET', '/v2/console/namespace/list')
        if result.get('code') != 0:
            raise ValueError('Nacos namespace inventory failed')
        for item in result.get('data', []):
            if item['namespace'] == self.namespace:
                return
        result = self.request('POST', '/v2/console/namespace',
                              {'namespaceId': self.namespace, 'namespaceName': self.namespace})
        if result.get('code') != 0:
            raise ValueError('Nacos namespace preparation failed')

    def get(self, data_id):
        self.login()
        result = self.request('GET', '/v2/cs/config', self.scope(data_id), missing=True)
        if result is None:
            return None
        if result.get('code') != 0 or not isinstance(result.get('data'), str):
            raise ValueError('Nacos configuration response is invalid')
        return result['data']

    def publish(self, data_id, config, replace=False):
        if not replace and self.get(data_id) is not None:
            return False
        self.login()
        values = self.scope(data_id) | {'content': json.dumps(remote_content(config), ensure_ascii=False), 'type': 'json'}
        result = self.request('POST', '/v2/cs/config', values)
        if result.get('code') != 0 or result.get('data') is not True:
            raise ValueError('Nacos configuration publication was not acknowledged')
        return True

    def delete(self, data_id):
        self.login()
        result = self.request('DELETE', '/v2/cs/config', self.scope(data_id))
        if result.get('code') != 0 or result.get('data') is not True:
            raise ValueError('Nacos configuration cleanup was not acknowledged')

    def environment(self, container=False):
        return {
            'NACOS_ADDRESS': self.container + ':8848' if container else self.address,
            'NACOS_NAMESPACE': self.namespace, 'NACOS_GROUP': self.group, 'NACOS_CLUSTER': self.cluster,
            'NACOS_USERNAME': self.username, 'NACOS_PASSWORD': self.password,
            'NACOS_TLS_ENABLED': 'false', 'NACOS_ROOT_CERT_FILE': '',
            'NACOS_CONNECT_TIMEOUT': '5s', 'NACOS_COMMAND_TIMEOUT': '2s',
        }
