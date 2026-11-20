"""Generate operator RPC models and hz-owned admin HTTP adapters and assets."""

import os
import re
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[3]
HTTP_IDL = ROOT / 'admin/idl/proto/admin/v1'
TEMPLATES = {
    'api': 'buf.operator.gen.yaml',
    'openapi': 'buf.admin.openapi.gen.yaml',
    'ts': 'buf.react.admin.typescript.gen.yaml',
}


def normalize_typescript_source(content):
    """Adapt emitted query values to Hertz's scalar/repeated JSON binding."""
    content = re.sub(r'^/\* eslint-disable[^\n]*\*/\n', '', content, flags=re.MULTILINE)
    content = re.sub(r'^// @ts-nocheck[^\n]*\n', '', content, flags=re.MULTILINE)
    content = re.sub(r'(export type \w+) = \{\s*\};', r'\1 = Record<string, never>;', content)
    # Proto JSON omits unset fields, including empty repeated fields. Keep read
    # types unchanged while allowing callers to omit them in partial updates.
    content = re.sub(r'^(  \w+): ([^\n;]+) \| undefined;', r'\1?: \2;', content, flags=re.MULTILINE)

    def query_block(match):
        # The upstream leaf walker descends into repeated messages as if they
        # were single objects. Hertz instead binds each repeated message from
        # one JSON query value and a singular message from one JSON object.
        fields = dict.fromkeys(re.findall(r'request\.(\w+)', match.group(1)))
        entries = ''.join(f"        ['{name}', request.{name}],\n" for name in fields)
        return 'const queryParams = encodeAdminQuery([\n' + entries + '      ]);\n      let uri = path;'

    content, count = re.subn(
        r'const queryParams: string\[\] = \[\];(.*?)      let uri = path;',
        query_block, content, flags=re.DOTALL,
    )
    if count:
        helper = '''
function encodeAdminQuery(fields: ReadonlyArray<readonly [string, unknown]>): string[] {
  return fields.flatMap(([name, value]) => {
    if (value === undefined || value === null) return [];
    const items: unknown[] = Array.isArray(value) ? value : [value];
    return items.map((item) => {
      const encoded = typeof item === 'object' ? JSON.stringify(item) : String(item);
      return `${encodeURIComponent(name)}=${encodeURIComponent(encoded)}`;
    });
  });
}

'''
        content = content.replace('\ninterface TransportMeta', helper + 'interface TransportMeta', 1)
    def unused_request(match):
        body = match.group(2)
        argument = 'request' if re.search(r'\brequest\b', body) else ''
        return f'    {match.group(1)}({argument}) {{{body}    }},'
    content = re.sub(r'^    (\w+)\((?:_request|request)\) \{(.*?)^    \},', unused_request, content,
                     flags=re.MULTILINE | re.DOTALL)
    return content


def normalize_typescript_output():
    output = ROOT / 'adminfrontend/src/api/generated'
    for path in output.rglob('*.ts'):
        path.write_text(normalize_typescript_source(path.read_text()))


def project_http(directory, assets_only=False):
    raw = directory / 'source.bin'
    projected = directory / 'http.bin'
    subprocess.run(['buf', 'build', '--as-file-descriptor-set', '-o', str(raw)], cwd=ROOT, check=True)
    command = [sys.executable, str(ROOT / 'scripts/go.py'), 'run', './scripts/admin/tools/httpgen', '-descriptor', str(raw), '-http-descriptor', str(projected)]
    if assets_only:
        command.append('-assets-only')
    subprocess.run(command, cwd=ROOT, check=True)
    return projected


def update_hz(directory):
    subprocess.run(['buf', 'generate', 'admin/idl/proto', '--template', 'buf.admin.gen.yaml'], cwd=ROOT, check=True)
    subprocess.run(['buf', 'generate', 'deer/third_party/admin', '--path', 'deer/third_party/admin/hertz/api.proto', '--template', 'buf.admin.gen.yaml'], cwd=ROOT, check=True)
    project_http(directory)
    dependencies = directory / 'proto'
    subprocess.run(['buf', 'export', '.', '-o', str(dependencies)], cwd=ROOT, check=True)
    stage = directory / 'admin'
    stage.mkdir()
    shutil.copyfile(ROOT / 'admin/.hz', stage / '.hz')
    for source in sorted(HTTP_IDL.glob('*.proto')):
        if '\nservice ' not in source.read_text():
            continue
        command = [
            os.environ.get('HZ', 'hz'), 'update', '--idl', str(source),
            '--module', 'github.com/hmlax/muskdeer', '--out_dir', str(stage),
            '--customize_package', str(ROOT / 'admin/hz.yaml'),
            '--use', 'github.com/hmlax/muskdeer/admin/biz/model', '--no_recurse',
            '-I', str(ROOT / 'admin/idl/proto'), '-I', str(dependencies),
        ]
        result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True)
        if result.returncode:
            sys.stderr.write(result.stdout + result.stderr)
            raise subprocess.CalledProcessError(result.returncode, command)
        print('hz update:', source.relative_to(ROOT), flush=True)
    generated = sorted((stage / 'biz/router').glob('*_generated.go'))
    if not generated:
        raise RuntimeError('hz update did not generate any route registrations')
    destination = ROOT / 'admin/biz/router'
    destination.mkdir(parents=True, exist_ok=True)
    names = {path.name for path in generated}
    for old in destination.glob('*_generated.go'):
        if old.name not in names:
            old.unlink()
    for source in generated:
        shutil.copyfile(source, destination / source.name)

def main():
    if len(sys.argv) != 2 or sys.argv[1] not in {*TEMPLATES, 'hz'}:
        raise SystemExit('usage: codegen.py api|hz|openapi|ts')
    target = sys.argv[1]
    if target == 'ts' and shutil.which('protoc-gen-typescript-http') is None:
        # buf's clean option removes the output before invoking the plugin.
        # Check first so a missing local tool cannot delete committed clients.
        raise SystemExit('Missing protoc-gen-typescript-http; run make plugin before make ts.')
    if target == 'api':
        sources = sorted((ROOT / 'deer/proto/operator/v1').glob('*.proto'))
        command = ['buf', 'generate', '--template', TEMPLATES[target]]
        for source in sources:
            if not source.name.startswith('model_'):
                command.extend(['--path', str(source.relative_to(ROOT))])
        subprocess.run(command, cwd=ROOT, check=True)
        return
    with tempfile.TemporaryDirectory(prefix='go-admin-hz-') as work:
        directory = Path(work)
        if target == 'hz':
            update_hz(directory)
            return
        projected = project_http(directory, assets_only=True)
        command = ['buf', 'generate', str(projected), '--template', TEMPLATES[target]]
        names = {source.name for source in HTTP_IDL.glob('*.proto')}
        if target == 'openapi':
            names.add('openapi.proto')
        for name in sorted(names):
            command.extend(['--path', 'admin/v1/' + name])
        subprocess.run(command, cwd=ROOT, check=True)
        if target == 'ts':
            normalize_typescript_output()


if __name__ == '__main__':
    main()
