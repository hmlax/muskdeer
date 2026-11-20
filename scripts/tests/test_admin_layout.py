"""Protect the single-module administration and core dependency boundary."""

import importlib.util
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
SPEC = importlib.util.spec_from_file_location('service_layout', ROOT / 'scripts/check-layout.py')
LAYOUT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(LAYOUT)


class AdministrationLayoutTests(unittest.TestCase):
    def test_core_process_cannot_link_administration(self):
        module = LAYOUT.MODULE
        dependencies = [module + 'strategy/internal/app', module + 'strategy/business']
        for forbidden in ('admin/biz/handler', 'operator/internal/data',
                          'deer/management/runtimeconfig', 'deer/pb/operator/v1'):
            with self.subTest(package=forbidden):
                issues = LAYOUT.check_packages([{
                    'ImportPath': module + 'strategy/cmd',
                    'Deps': [*dependencies, module + forbidden],
                }])
                self.assertTrue(any('administration' in issue for issue in issues), issues)

    def test_shared_runtime_cannot_import_management_service_implementation(self):
        for service in ('admin', 'operator'):
            issues = LAYOUT.check_packages([{
                'ImportPath': LAYOUT.MODULE + 'deer/management/security',
                'Imports': [LAYOUT.MODULE + service + '/internal/data'],
            }])
            self.assertTrue(any('imports service implementation' in issue for issue in issues), issues)

    def test_console_client_reuses_the_core_contract(self):
        client = (ROOT / 'operator/internal/data/muskdeer_client.go').read_text()
        self.assertIn('github.com/hmlax/muskdeer/deer/pb/strategy/console/v1', client)
        self.assertFalse((ROOT / 'deer/proto/muskdeer').exists())
        self.assertFalse((ROOT / 'deer/pb/muskdeer').exists())
        for directory in ('admin', 'operator', 'deer/management'):
            self.assertFalse((ROOT / directory / 'go.mod').exists())


if __name__ == '__main__':
    unittest.main()
