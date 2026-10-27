#!/usr/bin/env python3
"""Check the built runtime in an isolated, read-only, non-root container."""

import json
from pathlib import Path
import subprocess
import tempfile
import time
import uuid


ROOT = Path(__file__).resolve().parents[1]


def inspect(name):
    return json.loads(subprocess.run(['docker', 'inspect', name], check=True, capture_output=True, text=True).stdout)[0]


def main():
    name = 'muskdeer-image-test-' + uuid.uuid4().hex[:12]
    with tempfile.TemporaryDirectory(prefix='image-smoke-', dir=ROOT / '.cache') as directory:
        path = Path(directory) / 'service.json'
        config = json.loads((ROOT / 'configs/local/cron.json').read_text())
        config['instance_id'] = name
        path.write_text(json.dumps(config))
        path.chmod(0o644)  # This dependency-free fixture contains no credentials.
        created = False
        try:
            subprocess.run([
                'docker', 'create', '--name', name, '--read-only', '--cap-drop', 'ALL',
                '--security-opt', 'no-new-privileges', '--network', 'none',
                '--mount', f'type=bind,src={path},dst=/app/service.json,readonly',
                'muskdeer:local', '-role', 'cron', '-config', '/app/service.json',
            ], check=True)
            created = True
            subprocess.run(['docker', 'start', name], check=True)
            info = inspect(name)
            if info['Config']['User'] != '65532:65532' or not info['HostConfig']['ReadonlyRootfs'] or info['HostConfig']['NetworkMode'] != 'none':
                raise ValueError('runtime isolation differs from the tested contract')
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                result = subprocess.run([
                    'docker', 'exec', name, '/muskdeer', '-check-ready', '-config', '/app/service.json',
                ], check=False, capture_output=True, text=True, timeout=5)
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
            if logs.stderr or not {'service.initialized', 'service.transports_stopped'} <= messages:
                raise ValueError('runtime did not produce its JSON lifecycle records')
            if not all(record.get('service') == 'cron' and record.get('instance') == name for record in records):
                raise ValueError('runtime JSON lost its process identity')
            print('Non-root read-only runtime: readiness, JSON logging and SIGTERM passed', flush=True)
        finally:
            if created:
                subprocess.run(['docker', 'rm', '-f', name], check=True)


if __name__ == '__main__':
    main()
