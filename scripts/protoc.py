#!/usr/bin/env python3
"""Install the version-locked official protoc archive into the project cache."""

import hashlib
import json
from pathlib import Path, PurePosixPath
import platform
import subprocess
import sys
import urllib.request
import zipfile


ROOT = Path(__file__).resolve().parents[1]


def install() -> None:
    versions = json.loads((ROOT / 'tools/versions.json').read_text())
    version = versions['protoc']
    destination = ROOT / '.cache/protoc' / version
    binary = destination / 'bin/protoc'
    if binary.exists():
        actual = subprocess.check_output([str(binary), '--version'], text=True).strip()
        if actual != 'libprotoc ' + version:
            raise ValueError(f'cached protoc version mismatch: {actual!r}')
        print(actual)
        return
    key = platform.system() + '-' + platform.machine()
    archive = versions['protoc_archives'].get(key)
    if archive is None:
        raise ValueError(f'no locked protoc archive for {key}')
    downloaded = ROOT / '.cache/downloads' / archive['filename']
    downloaded.parent.mkdir(parents=True, exist_ok=True)
    if not downloaded.exists():
        url = f'https://github.com/protocolbuffers/protobuf/releases/download/v{version}/' + archive['filename']
        with urllib.request.urlopen(url, timeout=30) as response:
            downloaded.write_bytes(response.read())
    if hashlib.sha256(downloaded.read_bytes()).hexdigest() != archive['sha256']:
        raise ValueError('protoc archive checksum does not match tools/versions.json')
    with zipfile.ZipFile(downloaded) as package:
        for entry in package.infolist():
            relative = PurePosixPath(entry.filename)
            if relative.is_absolute() or '..' in relative.parts:
                raise ValueError('unsafe path in protoc archive')
        package.extractall(destination)
    binary.chmod(0o755)
    actual = subprocess.check_output([str(binary), '--version'], text=True).strip()
    if actual != 'libprotoc ' + version:
        raise ValueError(f'installed protoc version mismatch: {actual!r}')
    print(actual)


if __name__ == '__main__':
    try:
        install()
    except (OSError, ValueError, subprocess.CalledProcessError, zipfile.BadZipFile) as error:
        print(f'Protoc installation failed: {error}', file=sys.stderr)
        sys.exit(1)
