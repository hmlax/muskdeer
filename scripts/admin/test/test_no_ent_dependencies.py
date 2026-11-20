"""Keep the operator's generated and handwritten persistence paths GORM-only."""
from pathlib import Path
import re
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[3]


class PersistenceDependencyTests(unittest.TestCase):
    def test_no_ent_modules_or_go_imports(self):
        forbidden = re.compile(r'(?:entgo\.io/ent|github\.com/tx7do/go-crud/entgo)(?:[\s"/]|$)')
        self.assertIsNone(forbidden.search((ROOT / 'go.mod').read_text()))
        paths = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z', '--', '*.go'], cwd=ROOT).decode().split('\0')
        for name in filter(None, paths):
            path = ROOT / name
            if 'artifacts' in path.parts:
                continue
            self.assertIsNone(forbidden.search(path.read_text()), str(path.relative_to(ROOT)))

    def test_retired_implementations_and_generators_are_absent(self):
        for path in ['operator/internal/data/ent', 'operator/internal/data/enttest',
                     'operator/internal/data/gorm', 'operator/tools/schemaexport',
                     'operator/tools/fieldexport']:
            self.assertFalse((ROOT / path).exists(), path)
        for path in ['Makefile', 'admin/Makefile', 'operator/Makefile']:
            self.assertNotRegex((ROOT / path).read_text(), r'(?m)^ent:')


if __name__ == '__main__':
    unittest.main()
