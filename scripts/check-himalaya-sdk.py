#!/usr/bin/env python3
"""Verify the unchanged supplied SDK snapshot independently of its source tree."""
from pathlib import Path
import hashlib
import json
import sys

ROOT = Path(__file__).resolve().parents[1]


def main() -> None:
    lock = json.loads((ROOT / 'deer/third_party/lukla-go.snapshot.json').read_text())
    encoded = json.dumps(lock['files'], sort_keys=True, separators=(',', ':')).encode()
    if hashlib.sha256(encoded).hexdigest() != lock['sha256']:
        raise ValueError('SDK snapshot manifest digest differs')
    folder = ROOT / 'deer/third_party/lukla-go'
    for name, digest in lock['files'].items():
        path = (folder / name).resolve()
        if not path.is_relative_to(folder) or not path.is_file():
            raise ValueError(f'SDK snapshot file missing: {name}')
        if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise ValueError(f'SDK source was modified: {name}')
    actual = {str(path.relative_to(folder)) for path in folder.rglob('*') if path.is_file()}
    if actual != set(lock['files']):
        raise ValueError('SDK snapshot contains unrecorded files')
    print(f'Provided Himalaya Go SDK verified: {len(actual)} unchanged files')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError) as error:
        print(f'SDK verification failed: {error}', file=sys.stderr)
        sys.exit(1)
