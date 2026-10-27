#!/usr/bin/env python3
"""Compile exact Proto descriptors and check owned decimal/model conventions."""

import subprocess
import sys
import tempfile
from pathlib import Path

from go import run
from tools import protoc


ROOT = Path(__file__).resolve().parents[1]


def check():
    compiler = protoc()
    inputs = sorted((ROOT / 'deer/proto').rglob('*.proto')) + sorted((ROOT / 'api').rglob('*.proto'))
    if not inputs:
        raise ValueError('owned Proto schemas are missing')
    with tempfile.TemporaryDirectory(prefix='muskdeer-policy-') as directory:
        descriptor = Path(directory) / 'project.pb'
        subprocess.run([
            str(compiler), '-I.', '-Ithird_party', '-I' + str(compiler.parent.parent / 'include'),
            '--include_imports', '--descriptor_set_out=' + str(descriptor),
            *[path.relative_to(ROOT).as_posix() for path in inputs],
        ], cwd=ROOT, check=True)
        return run(['run', './cmd/contractcheck', '-descriptor', str(descriptor)])


if __name__ == '__main__':
    try:
        sys.exit(check())
    except (OSError, ValueError, subprocess.SubprocessError):
        print('Project source/Proto policy check failed', file=sys.stderr)
        sys.exit(1)
