#!/usr/bin/env python3
"""Check the built runtime in an isolated, read-only, non-root container."""

import json
from pathlib import Path
import subprocess
import tempfile
import time
import uuid


ROOT = Path(__file__).resolve().parents[1]

from infra import Infra
from nacos_config import ConfigStore


def inspect(name):
    return json.loads(subprocess.run(['docker', 'inspect', name], check=True, capture_output=True, text=True).stdout)[0]


def smoke(role, store):
    name = 'muskdeer-image-test-' + uuid.uuid4().hex[:12]
    with tempfile.TemporaryDirectory(prefix='image-smoke-', dir=ROOT / '.cache') as directory:
        path = Path(directory) / 'environment'
        data_id = role + '-' + uuid.uuid4().hex[:12] + '.json'
        config = json.loads((ROOT / ('configs/local/' + role + '.json')).read_text())
        config['instance_id'] = name
        store.publish(data_id, config)
        environment = store.environment(container=True) | {'NACOS_DATA_ID': data_id}
        path.write_text(''.join(key + '=' + value + '\n' for key, value in environment.items()))
        path.chmod(0o600)
        created = False
        try:
            subprocess.run([
                'docker', 'create', '--name', name, '--read-only', '--cap-drop', 'ALL',
                '--security-opt', 'no-new-privileges', '--network', 'infra-network',
                '--env-file', str(path), 'muskdeer-' + role + ':local',
            ], check=True)
            created = True
            subprocess.run(['docker', 'start', name], check=True)
            info = inspect(name)
            if info['Config']['User'] != '65532:65532' or not info['HostConfig']['ReadonlyRootfs'] or info['HostConfig']['NetworkMode'] != 'infra-network':
                raise ValueError('runtime isolation differs from the tested contract')
            if info['Config'].get('Cmd') or info['Mounts']:
                raise ValueError('core runtime must start without arguments or local configuration mounts')
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                result = subprocess.run([
                    'docker', 'exec', name, '/readycheck',
                ], check=False, capture_output=True, text=True, timeout=8)
                if result.returncode == 0:
                    break
                if not inspect(name)['State']['Running']:
                    raise ValueError('runtime exited before readiness')
                time.sleep(0.05)
            else:
                raise ValueError('runtime did not become ready within its test budget')
            subprocess.run(['docker', 'stop', '--time', '5', name], check=True)
            if inspect(name)['State']['ExitCode'] != 0:
                raise ValueError('SIGTERM did not exit the runtime successfully')
            logs = subprocess.run(['docker', 'logs', name], check=True, capture_output=True, text=True)
            records = [json.loads(line) for line in logs.stdout.splitlines()]
            messages = {record.get('msg') for record in records}
            if logs.stderr or not {'core.initialized', 'service.transports_stopped'} <= messages:
                raise ValueError('runtime did not produce its JSON lifecycle records')
            if not all(record.get('service') == role and record.get('instance') == name for record in records):
                raise ValueError('runtime JSON lost its process identity')
            print('Non-root read-only runtime: readiness, JSON logging and SIGTERM passed', flush=True)
        finally:
            if created:
                subprocess.run(['docker', 'rm', '-f', name], check=True)
            store.delete(data_id)


if __name__ == '__main__':
    with Infra(needs=('nacos',)) as infra:
        store = ConfigStore(namespace=infra.environment['MUSKDEER_TEST_NACOS_NAMESPACE'], group='MUSKDEER_IMAGE_TEST')
        for role in ['market', 'strategy', 'execution', 'portfolio']:
            smoke(role, store)
