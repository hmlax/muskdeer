#!/usr/bin/env python3
"""Use a checksum-pinned, project-local Node/npm for the administration UI."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
UI = ROOT / 'web/admin'


def unpack_node(archive: Path, stage: Path, name: str) -> None:
    """Reject members outside the one expected toolchain before extracting."""
    with tarfile.open(archive, 'r:xz') as package:
        members = package.getmembers()
        if len(members) > 20000 or sum(member.size for member in members) > 512 * 1024 * 1024:
            raise ValueError('pinned Node extraction exceeded its bound')
        root = (stage / name).resolve()
        for member in members:
            path = PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not path.parts or path.parts[0] != name:
                raise ValueError('pinned Node archive has an unexpected root')
            if not (member.isfile() or member.isdir() or member.issym() or member.islnk()):
                raise ValueError('pinned Node archive has an unsupported member')
            if member.issym() or member.islnk():
                parent = stage / path.parent if member.issym() else stage
                target = (parent / member.linkname).resolve()
                if not target.is_relative_to(root):
                    raise ValueError('pinned Node archive link escapes its root')
        package.extractall(stage, members=members, filter='data')


def node_toolchain() -> tuple[Path, Path, str]:
    versions = json.loads((ROOT / 'tools/web-versions.json').read_text())
    operating_system = {'Darwin': 'darwin', 'Linux': 'linux'}.get(platform.system())
    architecture = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'x64'}.get(platform.machine())
    target = f'{operating_system}-{architecture}'
    expected = versions['archives'].get(target)
    if expected is None:
        raise ValueError('unsupported pinned Node platform')
    name = f'node-v{versions["node"]}-{target}'
    cache = ROOT / '.cache/web-tools'
    cache.mkdir(parents=True, exist_ok=True)
    installed = cache / name
    with (cache / 'install.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if not installed.exists():
            with tempfile.TemporaryDirectory(prefix='install-', dir=cache) as temporary:
                stage = Path(temporary)
                archive = stage / 'node.tar.xz'
                address = f'https://nodejs.org/dist/v{versions["node"]}/{name}.tar.xz'
                with urllib.request.urlopen(address, timeout=30) as response:
                    with archive.open('xb') as output:
                        total = 0
                        while chunk := response.read(1024 * 1024):
                            total += len(chunk)
                            if total > 100 * 1024 * 1024:
                                raise ValueError('pinned Node archive exceeded its download bound')
                            output.write(chunk)
                with archive.open('rb') as archive_input:
                    checksum = hashlib.file_digest(archive_input, 'sha256').hexdigest()
                if checksum != expected:
                    raise ValueError('pinned Node checksum mismatch')
                unpack_node(archive, stage, name)
                (stage / name).rename(installed)
    executable = installed / 'bin/node'
    actual = subprocess.check_output([str(executable), '--version'], text=True).strip()
    if actual != 'v' + versions['node']:
        raise ValueError('pinned Node version mismatch')
    npm = installed / 'lib/node_modules/npm/bin/npm-cli.js'
    actual_npm = subprocess.check_output([str(executable), str(npm), '--version'], text=True).strip()
    if actual_npm != versions['npm']:
        raise ValueError('pinned npm version mismatch')
    return executable, npm, actual


def run(action: str) -> int:
    node, npm, version = node_toolchain()
    print(f'{version}: pinned administration toolchain', flush=True)
    environment = os.environ.copy()
    environment['PATH'] = str(node.parent) + os.pathsep + environment.get('PATH', '')
    environment['npm_config_cache'] = str(ROOT / '.cache/npm')
    environment['npm_config_update_notifier'] = 'false'
    environment['PLAYWRIGHT_BROWSERS_PATH'] = str(ROOT / '.cache/playwright')
    commands = {
        'lock': ['install', '--package-lock-only', '--ignore-scripts'],
        'install': ['ci', '--ignore-scripts'],
        'build': ['run', 'build'],
        'check': ['run', 'check'],
        'browser-install': ['run', 'browser-install'],
        'browser-test': ['run', 'browser-test'],
        'browser-strategies': ['run', 'browser-strategies'],
        'browser-controls': ['run', 'browser-controls'],
        'browser-creation': ['run', 'browser-creation'],
    }
    if action == 'doctor':
        return 0
    return subprocess.run([str(node), str(npm), *commands[action]], cwd=UI, env=environment, check=False).returncode


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['doctor', 'lock', 'install', 'build', 'check', 'browser-install', 'browser-test', 'browser-strategies', 'browser-controls', 'browser-creation'])
    options = parser.parse_args()
    try:
        sys.exit(run(options.action))
    except (OSError, ValueError, tarfile.TarError, subprocess.SubprocessError) as error:
        print(f'Administration tooling failed: {error}', file=sys.stderr)
        sys.exit(1)
