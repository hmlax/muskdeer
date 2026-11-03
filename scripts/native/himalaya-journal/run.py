#!/usr/bin/env python3
"""Test the command journal using only an isolated DB on existing infra MySQL."""
from pathlib import Path
import json
import os
import re
import subprocess
import sys
from urllib.parse import quote
from uuid import uuid4

ROOT = Path(__file__).resolve().parents[3]
ARTIFACTS = ROOT / '.cache/himalaya'


def main() -> int:
    inspected = subprocess.run(['docker', 'inspect', 'infra-mysql'], check=True, capture_output=True, text=True)
    values = json.loads(inspected.stdout)[0]['Config']['Env']
    config = dict(value.split('=', 1) for value in values if '=' in value)
    password = config.get('MYSQL_ROOT_PASSWORD')
    if not password:
        raise RuntimeError('existing infra MySQL has no usable test administrator credential')
    database = 'muskdeer_maker_test_' + uuid4().hex
    assert re.fullmatch(r'muskdeer_maker_test_[0-9a-f]{32}', database)
    environment = os.environ.copy()
    environment['MYSQL_PWD'] = password
    base = ['docker', 'exec', '-i', '-e', 'MYSQL_PWD', 'infra-mysql', 'mysql', '--user=root', '--batch']

    def sql(statement: str, selected: bool = False) -> None:
        result = subprocess.run(base + ([database] if selected else []), input=statement, env=environment, capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError('isolated MySQL setup/cleanup failed: ' + result.stderr.replace(password, '[redacted]'))

    sql(f'CREATE DATABASE `{database}`')
    try:
        sql((ROOT / 'deer/third_party/himalaya/order/001_maker_commands.sql').read_text(), True)
        environment['HIMALAYA_MAKER_TEST_DATABASE_URL'] = f'mysql://root:{quote(password, safe="")}@127.0.0.1:3306/{database}'
        environment['CARGO_TARGET_DIR'] = str(ARTIFACTS / 'native-target')
        result = subprocess.run([
            'rustup', 'run', '1.94.0', 'cargo', 'test', '--locked', '--offline',
            '--manifest-path', str(Path(__file__).with_name('Cargo.toml')),
        ], cwd=ROOT, env=environment, capture_output=True, text=True)
        ARTIFACTS.mkdir(parents=True, exist_ok=True)
        log = ARTIFACTS / 'native-journal.log'
        log.write_text((result.stdout + result.stderr).replace(password, '[redacted]'))
        log.chmod(0o600)
        output = result.stdout + '\n'.join(result.stderr.splitlines()[-10:])
        print(output.replace(password, '[redacted]')[-7000:])
        return result.returncode
    finally:
        sql(f'DROP DATABASE `{database}`')


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f'Native journal test failed: {error}', file=sys.stderr)
        sys.exit(1)
