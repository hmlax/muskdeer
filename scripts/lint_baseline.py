"""Match only unchanged administration style findings from the imported source."""

from collections import Counter
import hashlib
from pathlib import Path
import re


STYLE_RULES = frozenset(('exported', 'var-naming', 'unused-parameter', 'package-comments',
                         'context-as-argument', 'indent-error-flow', 'redefines-builtin-id',
                         'var-declaration'))
IMPORTED_ROOTS = ('admin/', 'operator/', 'deer/management/', 'scripts/admin/tools/')


def anchor(line):
    # A body edit must not require renaming an existing protocol method. Findings
    # on new statements still have their own anchor and cannot consume this one.
    line = line.strip()
    if line.startswith(('func ', 'type ')):
        line = line.split('{', 1)[0]
    return hashlib.sha256(re.sub(r'\s+', '', line).encode()).hexdigest()


def issue_key(issue, root):
    path = Path(issue['Pos']['Filename'])
    if path.is_absolute():
        try:
            path = path.relative_to(root)
        except ValueError:
            return None
    relative = path.as_posix()
    rule = issue['Text'].split(':', 1)[0]
    if (issue['FromLinter'] != 'revive' or rule not in STYLE_RULES or
            '..' in path.parts or not relative.startswith(IMPORTED_ROOTS)):
        return None
    lines = issue.get('SourceLines', [])
    if not lines:
        return None
    return relative, issue['Text'], anchor(lines[0])


def filter_issues(issues, entries, root):
    remaining = Counter({(entry['path'], entry['text'], entry['anchor']): entry['count']
                         for entry in entries})
    failures = []
    matched = 0
    for issue in issues:
        key = issue_key(issue, root)
        if key is not None and remaining[key] > 0:
            remaining[key] -= 1
            matched += 1
        else:
            failures.append(issue)
    return failures, matched
