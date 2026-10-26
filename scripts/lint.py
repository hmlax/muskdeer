#!/usr/bin/env python3
"""Run the version-locked Go linter using the pinned project toolchain."""

import json
import os
from pathlib import Path
import subprocess
import sys

from go import run


ROOT = Path(__file__).resolve().parents[1]


if __name__ == '__main__':
    try:
        versions = json.loads((ROOT / 'tools/versions.json').read_text())
        os.environ['GOLANGCI_LINT_CACHE'] = str(ROOT / '.cache/golangci-lint')
        sys.exit(run([
            'run', 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@' + versions['golangci_lint'],
            'run', './...',
        ]))
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f'Lint failed: {error}', file=sys.stderr)
        sys.exit(1)
