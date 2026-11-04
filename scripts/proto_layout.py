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
        package = re.search(r'option\s+go_package\s*=\s*"([^";]+)(?:;[^"]+)?"', text)
        expected = MODULE + source.parent.relative_to(root).as_posix()
        if package is None or package.group(1) != expected:
            failures.append(f'{relative}: go_package must match its owner directory')
        if relative.parts[0] == 'common' and re.search(r'^service\s+', text, re.MULTILINE):
            if relative.parts[:2] != ('common', 'probe'):
                failures.append(f'{relative}: business services require their own owner directory')
    if failures:
        raise ValueError('\n'.join(failures))
