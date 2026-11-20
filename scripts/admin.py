#!/usr/bin/env python3
"""Build and regenerate the integrated administration applications."""

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[1]
GENERATED_ROOTS = ('admin', 'operator', 'deer/pb/operator/v1')
TOOLS = ROOT / 'scripts/admin/tools'


def environment():
    version = (ROOT / '.go-version').read_text().strip()
    return dict(os.environ, GOTOOLCHAIN='go' + version, GOWORK='off',
                GOMODCACHE=str(ROOT / '.cache/gomod'), GOCACHE=str(ROOT / '.cache/gobuild'),
                GOPATH=str(ROOT / '.cache/gopath'), GOBIN=str(ROOT / '.cache/bin'),
                PATH=str(ROOT / '.cache/bin') + os.pathsep + os.environ.get('PATH', ''))


def run(*arguments):
    subprocess.run([str(argument) for argument in arguments], cwd=ROOT,
                   env=environment(), check=True)


def go(*arguments):
    run(sys.executable, ROOT / 'scripts/go.py', *arguments)


def generated_files(root):
    files = set()
    for directory in GENERATED_ROOTS:
        for path in (root / directory).rglob('*.go'):
            if any(line.startswith('// Code generated ') and 'DO NOT EDIT.' in line
                   for line in path.read_text().splitlines()[:12]):
                files.add(path.relative_to(root))
    files.update(path.relative_to(root) for path in
                 (root / 'adminfrontend/src/api/generated').rglob('*.ts'))
    files.add(Path('deer/management/apidocs/openapi.yaml'))
    return files


def generate():
    run(sys.executable, TOOLS / 'codegen.py', 'api')
    run(sys.executable, TOOLS / 'gen-models.py')
    run(sys.executable, TOOLS / 'muskdeer-mappings.py')
    run(sys.executable, TOOLS / 'codegen.py', 'hz')
    run(sys.executable, TOOLS / 'gatewaygen.py')
    run(sys.executable, TOOLS / 'codegen.py', 'openapi')
    go('run', './scripts/admin/tools/openapigen', 'deer/management/apidocs/openapi.yaml')
    run(sys.executable, TOOLS / 'codegen.py', 'ts')
    go('tool', 'wire', 'gen', './operator/cmd/server')
    goroot = subprocess.check_output([sys.executable, str(ROOT / 'scripts/go.py'),
                                     'env', 'GOROOT'], cwd=ROOT, env=environment(), text=True).strip()
    files = sorted(str(ROOT / path) for path in generated_files(ROOT) if path.suffix == '.go')
    run(Path(goroot) / 'bin/gofmt', '-w', *files)


def check():
    # Regeneration never changes the checkout, including on generator failure.
    paths = subprocess.check_output(['git', 'ls-files', '--cached', '--others',
                                     '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0')
    with tempfile.TemporaryDirectory(prefix='muskdeer-admin-generate-') as temporary:
        stage = Path(temporary)
        for relative in paths:
            if not relative or not (ROOT / relative).is_file():
                continue
            destination = stage / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, destination)
        (stage / '.cache').symlink_to(ROOT / '.cache', target_is_directory=True)
        run(sys.executable, stage / 'scripts/admin.py', 'generate')
        changed = []
        for relative in sorted(generated_files(ROOT) | generated_files(stage)):
            expected, actual = ROOT / relative, stage / relative
            if not expected.exists() or not actual.exists() or expected.read_bytes() != actual.read_bytes():
                changed.append(str(relative))
        if changed:
            raise ValueError('Administration generated files differ: ' + ', '.join(changed))
    print('Administration Proto, GORM, Hertz, Wire, OpenAPI and TypeScript generation verified')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('generate', 'check', 'tools', 'wire', 'wire-check'))
    action = parser.parse_args().action
    if action == 'tools':
        for tool in json.loads((ROOT / 'tools/versions.json').read_text())['admin_plugins']:
            go('install', tool['package'] + '@' + tool['version'])
    elif action in ('wire', 'wire-check'):
        go('tool', 'wire', 'diff' if action == 'wire-check' else 'gen', './operator/cmd/server')
    elif action == 'check':
        check()
    else:
        generate()


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print('Administration tooling failed: ' + str(error), file=sys.stderr)
        sys.exit(1)
