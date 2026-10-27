"""Verify startup failures retain diagnostics and scoped cleanup."""

import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location('integration_runner', Path(__file__).resolve().parents[1] / 'integration.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class IntegrationFailureTest(unittest.TestCase):
    def test_assigned_ports_reach_only_the_owned_fixture(self):
        bindings = {
            'postgres': '127.0.0.1:60101',
            'redis': '127.0.0.1:60102',
            'nats': '127.0.0.1:60103',
            'nacos': '127.0.0.1:60104',
        }
        calls = []
        environments = []

        def execute(arguments, **kwargs):
            calls.append(arguments)
            if 'port' in arguments:
                return subprocess.CompletedProcess(arguments, 0, stdout=bindings[arguments[-2]] + '\n')
            if len(arguments) > 1 and arguments[1].endswith('/scripts/go.py'):
                environments.append(kwargs['env'])
            return subprocess.CompletedProcess(arguments, 0)

        with patch.object(runner.subprocess, 'run', side_effect=execute):
            with patch.object(runner, 'initialize_naming') as initialize:
                self.assertEqual(runner.run(), 0)
        initialize.assert_called_once_with(bindings['nacos'])
        self.assertEqual(len(environments), 2)
        for environment in environments:
            self.assertIn('port=60101 ', environment['MUSKDEER_TEST_POSTGRES_DSN'])
            self.assertEqual(environment['MUSKDEER_TEST_POSTGRES_PORT'], '60101')
            self.assertEqual(environment['MUSKDEER_TEST_REDIS_ADDRESS'], bindings['redis'])
            self.assertEqual(environment['MUSKDEER_TEST_NATS_ADDRESS'], bindings['nats'])
            self.assertEqual(environment['MUSKDEER_TEST_NACOS_ADDRESS'], bindings['nacos'])
        prefix = calls[0][:6]
        for arguments in calls:
            if 'port' in arguments:
                self.assertEqual(arguments[:6], prefix)
        self.assertEqual(calls[-1], [*prefix, 'down', '--volumes'])

    def test_published_address_rejects_unscoped_or_invalid_bindings(self):
        compose = ['docker', 'compose', '-p', 'muskdeer-unit-only', '-f', 'unit-only.yml']
        for binding in ['0.0.0.0:60101', 'localhost:60101', '127.0.0.1:0', '127.0.0.1:70000', '127.0.0.1:1\n127.0.0.1:2']:
            with self.subTest(binding=binding):
                result = subprocess.CompletedProcess([], 0, stdout=binding)
                with patch.object(runner.subprocess, 'run', return_value=result):
                    with self.assertRaises(ValueError):
                        runner.published_address(compose, 'postgres', 5432)

    def test_startup_error_survives_diagnostic_failure_and_cleans_own_project(self):
        failure = subprocess.CalledProcessError(17, ['isolated', 'startup'])
        calls = []

        def execute(arguments, **_kwargs):
            calls.append(arguments)
            if 'up' in arguments:
                raise failure
            return subprocess.CompletedProcess(arguments, 1 if 'logs' in arguments else 0)

        with patch.object(runner.subprocess, 'run', side_effect=execute):
            with self.assertRaises(subprocess.CalledProcessError) as caught:
                runner.run()
        self.assertIs(caught.exception, failure)
        self.assertEqual(len(calls), 3)
        prefix = calls[0][:6]
        self.assertEqual(prefix[:3], ['docker', 'compose', '-p'])
        self.assertTrue(prefix[3].startswith('muskdeer-p01-'))
        self.assertEqual(calls[1], [*prefix, 'logs', '--no-color', '--tail', '80'])
        self.assertEqual(calls[2], [*prefix, 'down', '--volumes'])


if __name__ == '__main__':
    unittest.main()
