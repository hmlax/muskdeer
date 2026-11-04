"""Validate service-first ownership and generated Go package locations."""

from pathlib import Path
import re

SERVICES = ('market', 'strategy', 'execution', 'portfolio', 'cron')
OWNERS = ('common', *SERVICES)
MODULE = 'github.com/hmlax/muskdeer/'


def validate_layout(root: Path):
    proto_root = root / 'deer/proto'
    failures = []
    for source in sorted(proto_root.rglob('*.proto')):
        relative = source.relative_to(proto_root)
        if relative.parts[0] not in OWNERS:
            failures.append(f'{relative}: first directory must be a service or common')
            continue
        text = source.read_text()
        if 'model' in relative.parts and re.search(r'^message\s+Himalaya\w*\s*\{', text, re.MULTILINE):
            failures.append(f'{relative}: provider-specific models belong to business-neutral definitions')
        if 'model' not in relative.parts and re.search(r'^message\s+Himalaya\w*\s*\{', text, re.MULTILINE):
            failures.append(f'{relative}: provider-specific business messages are retired')
        if re.search(r'^package\s+muskdeer\.himalaya(?:\.|\s*;)', text, re.MULTILINE):
            failures.append(f'{relative}: provider-specific RPC namespace is retired')
        if 'venue' in relative.parts and re.search(r'^service\s+', text, re.MULTILINE):
            namespace = f'muskdeer.{relative.parts[0]}.venue.v1'
            if not re.search(r'^package\s+' + re.escape(namespace) + r'\s*;', text, re.MULTILINE):
                failures.append(f'{relative}: venue RPC namespace must follow its service owner')
        is_config = 'config' in relative.parts or re.search(r'^package\s+muskdeer\.config\.v1\s*;', text, re.MULTILINE)
        if is_config and relative.as_posix() != 'common/config/v1/config.proto':
            failures.append(f'{relative}: configuration definitions must share common/config/v1/config.proto')
        package = re.search(r'option\s+go_package\s*=\s*"([^";]+)(?:;[^"]+)?"', text)
        expected = MODULE + 'deer/pb/' + relative.parent.as_posix()
        if package is None or package.group(1) != expected:
            failures.append(f'{relative}: go_package must match its owner directory under deer/pb')
        if relative.parts[0] == 'common' and re.search(r'^service\s+', text, re.MULTILINE):
            if relative.parts[:2] != ('common', 'probe'):
                failures.append(f'{relative}: business services require their own owner directory')
    for source in sorted(proto_root.rglob('*.go')):
        failures.append(f'{source.relative_to(root)}: Go files belong under deer/pb, not deer/proto')
    for source in sorted((root / 'deer/pb').rglob('*.proto')):
        failures.append(f'{source.relative_to(root)}: Proto sources belong under deer/proto')
    if failures:
        raise ValueError('\n'.join(failures))
