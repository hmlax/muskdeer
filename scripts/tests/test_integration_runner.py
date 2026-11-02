"""Verify startup failures retain diagnostics and scoped cleanup."""

import importlib.util
import io
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location('integration_runner', Path(__file__).resolve().parents[1] / 'integration.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class IntegrationFailureTest(unittest.TestCase):
    def test_migration_batches_cover_every_runnable_entry_once(self):
        inventory = '\n'.join([
            'TestSecond', 'TestFirst', 'FuzzDecimal', 'ExampleBalance',
            'BenchmarkNotRunByDefault', 'ok\tgithub.com/hmlax/muskdeer/internal/migrate\t0.1s',
        ])
        batches = runner.migration_batches(inventory)
        flattened = [name for batch in batches for name in batch]
        self.assertEqual(len(batches), 2)
        self.assertEqual(sorted(flattened), ['ExampleBalance', 'FuzzDecimal', 'TestFirst', 'TestSecond'])
        self.assertEqual(len(flattened), len(set(flattened)))
        for bad in ['', 'TestSame\nTestSame', 'TestUnexpected/child', 'unexpected-output']:
            with self.subTest(inventory=bad):
                with self.assertRaises(ValueError):
                    runner.migration_batches(bad)

    def test_migration_batch_failure_preserves_exit_and_never_runs_later_batch(self):
        calls = []

        def execute(arguments, **kwargs):
            calls.append(arguments)
            self.assertEqual(kwargs['env'], {'test': 'owned-only'})
            if '-list' in arguments:
                return subprocess.CompletedProcess(arguments, 0, stdout='TestSecond\nTestFirst\n')
            self.assertIn('-race', arguments)
            self.assertIn('-count=1', arguments)
            self.assertIn('-timeout=10m', arguments)
            self.assertEqual(arguments[arguments.index('-run') + 1], '^(TestFirst)$')
            return subprocess.CompletedProcess(arguments, 17)

        with patch.object(runner.subprocess, 'run', side_effect=execute):
            self.assertEqual(runner.run_migration_tests({'test': 'owned-only'}), 17)
        self.assertEqual(len(calls), 2)

    def test_migration_inventory_failure_prevents_any_test_batch(self):
        result = subprocess.CompletedProcess([], 19, stdout='TestPartial\n', stderr='inventory failed\n')
        with patch.object(runner.subprocess, 'run', return_value=result) as execute:
            self.assertEqual(runner.run_migration_tests({}), 19)
        self.assertEqual(execute.call_count, 1)

    def test_migration_batches_execute_exact_full_names_with_original_limits(self):
        calls = []

        def execute(arguments, **kwargs):
            calls.append(arguments)
            if '-list' in arguments:
                return subprocess.CompletedProcess(arguments, 0, stdout='TestSecond\nTestFirst\n')
            self.assertEqual(kwargs['env'], {'test': 'owned-only'})
            self.assertIn('-timeout=10m', arguments)
            self.assertIn('-race', arguments)
            return subprocess.CompletedProcess(arguments, 0)

        with patch.object(runner.subprocess, 'run', side_effect=execute):
            self.assertEqual(runner.run_migration_tests({'test': 'owned-only'}), 0)
        self.assertEqual([call[call.index('-run') + 1] for call in calls[1:]], ['^(TestFirst)$', '^(TestSecond)$'])
