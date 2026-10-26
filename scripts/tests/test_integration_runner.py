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
