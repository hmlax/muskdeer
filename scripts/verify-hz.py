#!/usr/bin/env python3
"""Check deterministic Hertz generation using existing typed protobuf DTOs."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

from tools import check, protoc


ROOT = Path(__file__).resolve().parents[1]
PROBE_MODULE = 'github.com/hmlax/muskdeer/hzprobe'


def verify() -> None:
    check()
    go_environment = json.loads(subprocess.check_output([
        sys.executable, str(ROOT / 'scripts/go.py'), 'env', '-json',
        'GOROOT', 'GOTOOLCHAIN', 'GOCACHE', 'GOMODCACHE', 'GOPATH', 'GOBIN', 'GOWORK',
    ], text=True))
    environment = os.environ.copy()
    environment.update(go_environment)
    compiler = protoc()
    environment['PATH'] = str(compiler.parent) + os.pathsep + str(ROOT / '.cache/bin') + os.pathsep + environment['PATH']
    go_binary = str(Path(go_environment['GOROOT']) / 'bin/go')
    with tempfile.TemporaryDirectory(prefix='muskdeer-hz-check-') as temporary:
        generated = []
        for generation in ('first', 'second'):
            stage = Path(temporary) / generation
            stage.mkdir()
            subprocess.run([
                str(ROOT / '.cache/bin/hz'), 'new', '--module', PROBE_MODULE,
                '--idl', str(ROOT / 'api/probe/v1/probe.proto'),
                '-I', str(ROOT / 'third_party'), '-I', str(compiler.parent.parent / 'include'),
                '--use', 'github.com/hmlax/muskdeer',
                '--trim_gopackage', 'github.com/hmlax/muskdeer/',
                '--out_dir', str(stage), '--sort_router',
            ], cwd=stage, env=environment, check=True)
            files = {path.relative_to(stage).as_posix(): path.read_bytes() for path in stage.rglob('*.go')}
            if not files or any(name.endswith('.pb.go') for name in files):
                raise ValueError('hz must reuse the existing DTOs without generating duplicate protobuf types')
            generated.append(files)
        if generated[0] != generated[1]:
            raise ValueError('hz produced different Go source for identical input')
        stage = Path(temporary) / 'first'
        (stage / 'go.mod').write_text(
            f'module {PROBE_MODULE}\n\ngo 1.27.3\n\n'
            f'require github.com/hmlax/muskdeer v0.0.0\n'
            f'replace github.com/hmlax/muskdeer => {json.dumps(str(ROOT))}\n',
            encoding='utf-8',
        )
        subprocess.run([go_binary, 'mod', 'tidy'], cwd=stage, env=environment, check=True)
        subprocess.run([go_binary, 'build', './...'], cwd=stage, env=environment, check=True)
        print('Hertz generation is deterministic; shared protobuf DTOs and generated routes compile')


if __name__ == '__main__':
    try:
        verify()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Hertz generator verification failed: {error}', file=sys.stderr)
        sys.exit(1)
