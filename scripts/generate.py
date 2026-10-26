#!/usr/bin/env python3
"""Generate Proto Go code in isolation; --check never overwrites working files."""

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from tools import check, protoc


ROOT = Path(__file__).resolve().parents[1]


def generate(verify: bool) -> None:
    check()
    inputs = sorted((ROOT / 'deer/proto').rglob('*.proto')) + sorted((ROOT / 'api').rglob('*.proto'))
    if not inputs:
        raise ValueError('no project Proto definitions found')
    with tempfile.TemporaryDirectory(prefix='muskdeer-generate-') as temporary:
        stage = Path(temporary)
        for base in ('deer/proto', 'api', 'third_party'):
            for original in (ROOT / base).rglob('*.proto'):
                destination = stage / original.relative_to(ROOT)
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(original, destination)
        environment = os.environ.copy()
        environment['PATH'] = str(ROOT / '.cache/bin') + os.pathsep + environment['PATH']
        compiler = protoc()
        arguments = [str(compiler), '-I.', '-Ithird_party', '-I' + str(compiler.parent.parent / 'include')]
        files = [path.relative_to(ROOT).as_posix() for path in inputs] + ['hertz/api.proto']
        mapping = 'Mhertz/api.proto=github.com/hmlax/muskdeer/deer/proto/hertz;api'
        subprocess.run([
            *arguments, '--go_out=.', '--go_opt=module=github.com/hmlax/muskdeer', '--go_opt=' + mapping,
            *files,
        ], cwd=stage, env=environment, check=True)
        rpc_files = [path.relative_to(ROOT).as_posix() for path in inputs if path.is_relative_to(ROOT / 'api')]
        if rpc_files:
            subprocess.run([
                *arguments, '--go-grpc_out=.', '--go-grpc_opt=module=github.com/hmlax/muskdeer',
                '--go-grpc_opt=' + mapping, *rpc_files,
            ], cwd=stage, env=environment, check=True)
        tag_files = [path.relative_to(ROOT).as_posix() for path in inputs]
        subprocess.run([
            *arguments, '--gotag_out=module=github.com/hmlax/muskdeer:.', *tag_files,
        ], cwd=stage, env=environment, check=True)
        generated = sorted(stage.rglob('*.pb.go'))
        if not generated:
            raise ValueError('generation produced no Go files')
        expected = {path.relative_to(stage) for path in generated}
        existing = {
            path.relative_to(ROOT)
            for base in ('deer/proto', 'api')
            for path in (ROOT / base).rglob('*.pb.go')
        }
        stale = sorted(existing - expected)
        if stale:
            raise ValueError(f'stale generated files require review: {stale}')
        differences = []
        for original in generated:
            relative = original.relative_to(stage)
            destination = ROOT / relative
            if verify:
                if not destination.exists() or destination.read_bytes() != original.read_bytes():
                    differences.append(relative.as_posix())
                continue
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(original, destination)
        if differences:
            raise ValueError('generated files differ: ' + ', '.join(differences))
        print(f'{"Checked" if verify else "Generated"} {len(generated)} Proto Go file(s)')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    arguments = parser.parse_args()
    try:
        generate(arguments.check)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Generation failed: {error}', file=sys.stderr)
        sys.exit(1)
