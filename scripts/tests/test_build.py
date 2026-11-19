"""Verify linker argument handling without executing metadata as shell code."""

import importlib.util
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location('metadata_build', Path(__file__).resolve().parents[1] / 'build.py')
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)


class BuildMetadataTest(unittest.TestCase):
    def test_build_values_are_linker_arguments_and_hostname_is_runtime_only(self):
        command = BUILD.command('market', './market/cmd', 'market-bin',
                                {'VERSION': 'v1.2.3', 'BUILD': 'abc@2026-11-19T00:00:00Z'})
        self.assertIn('scripts/go.py', command[1])
        self.assertEqual(command[2:4], ['build', '-trimpath'])
        flags = command[command.index('-ldflags') + 1]
        self.assertEqual(flags, '-X main.Name=market -X main.Version=v1.2.3 -X main.Build=abc@2026-11-19T00:00:00Z')
        self.assertNotIn('main.ID', flags)
        self.assertEqual(command[-3:], ['-o', 'market-bin', './market/cmd'])

    def test_explicit_name_can_override_binary_name(self):
        values = BUILD.metadata('market', {'NAME': 'custom-market', 'VERSION': 'v2', 'BUILD': 'commit'})
        self.assertEqual(values['Name'], 'custom-market')

    def test_source_archive_without_git_keeps_development_defaults(self):
        with patch.object(BUILD.subprocess, 'run', side_effect=FileNotFoundError):
            values = BUILD.metadata('market', {})
        self.assertEqual(values['Version'], 'dev')
        self.assertTrue(values['Build'].startswith('unknown@'))

    def test_unquoted_linker_metadata_cannot_contain_shell_syntax(self):
        for value in ('a b', '$(touch /tmp/metadata-injection)', '`id`', 'v1;exit', 'a\nb', "a'b"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                BUILD.metadata('market', {'VERSION': value, 'BUILD': 'test'})

    def test_build_failure_is_returned_and_shell_is_not_used(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(BUILD.os.environ, {'NAME': '', 'VERSION': 'v1', 'BUILD': 'test'}):
                with patch.object(BUILD.subprocess, 'run', return_value=SimpleNamespace(returncode=23)) as run:
                    result = BUILD.main(['market', './market/cmd', str(Path(directory) / 'market')])
                    self.assertEqual(result, 23)
                    self.assertNotIn('shell', run.call_args.kwargs)
