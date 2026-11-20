#!/usr/bin/env python3
"""Run the version-locked Go linter using the pinned project toolchain."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

from go import run
from lint_baseline import filter_issues


ROOT = Path(__file__).resolve().parents[1]


if __name__ == '__main__':
    try:
        versions = json.loads((ROOT / 'tools/versions.json').read_text())
        os.environ['GOLANGCI_LINT_CACHE'] = str(ROOT / '.cache/golangci-lint')
        baseline = json.loads((ROOT / 'tools/admin-style-baseline.json').read_text())
        with tempfile.TemporaryDirectory(prefix='muskdeer-lint-') as directory:
            report = Path(directory) / 'issues.json'
            result = run([
                'run', 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@' + versions['golangci_lint'],
                'run', '--issues-exit-code=0', '--max-issues-per-linter=0', '--max-same-issues=0',
                '--output.json.path=' + str(report), '--output.text.path=' + os.devnull, './...',
            ])
            if result not in (0, 1) or not report.is_file():
                raise ValueError('linter did not produce a complete issue report')
            payload = json.loads(report.read_text())
            if 'Issues' not in payload or payload['Issues'] is not None and not isinstance(payload['Issues'], list):
                raise ValueError('linter issue report is malformed')
            issues = payload['Issues'] or []
            if result != 0 and not issues:
                raise ValueError('linter failed without a complete issue report')
            failures, matched = filter_issues(issues, baseline['entries'], ROOT)
            for issue in failures:
                position = issue['Pos']
                print(f"{position['Filename']}:{position['Line']}:{position['Column']}: "
                      f"{issue['Text']} ({issue['FromLinter']})")
            print(f'{len(failures)} new issue(s); {matched} imported framework style finding(s) '
                  'match the reviewed source baseline')
            sys.exit(bool(failures))
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Lint failed: {error}', file=sys.stderr)
        sys.exit(1)
