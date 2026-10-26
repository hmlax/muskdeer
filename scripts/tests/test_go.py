"""Exercise the compiler gate before allowing build/test commands to run."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class GoToolchainTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix='muskdeer-go-')
        self.addCleanup(self.directory.cleanup)
        self.temporary = Path(self.directory.name)
        self.receipt = self.temporary / 'invocation.json'
        binary = self.temporary / 'go'
        binary.write_text(
            f'#!{sys.executable}\n'
            'import json, os, pathlib, sys\n'
            'if sys.argv[1:] == ["env", "GOVERSION"]:\n'
            '    print(os.environ["TEST_COMPILER_VERSION"])\n'
            '    sys.exit(int(os.environ.get("TEST_PROBE_STATUS", "0")))\n'
            'pathlib.Path(os.environ["TEST_RECEIPT"]).write_text(json.dumps({\n'
            '    "arguments": sys.argv[1:], "toolchain": os.environ["GOTOOLCHAIN"],\n'
            '    "workspace": os.environ["GOWORK"], "cwd": os.getcwd(),\n'
            '    "cache": os.environ["GOCACHE"]}))\n'
            'sys.exit(int(os.environ.get("TEST_COMMAND_STATUS", "0")))\n',
            encoding='utf-8',
        )
        binary.chmod(0o700)
        self.environment = os.environ.copy()
        self.environment.update({
            'PATH': str(self.temporary) + os.pathsep + self.environment['PATH'],
            'TEST_RECEIPT': str(self.receipt),
            'TEST_COMPILER_VERSION': 'go1.27.3',
            'GOTOOLCHAIN': 'go1.26.1+auto',
        })

    def invoke(self):
        return subprocess.run(
            [sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-run', 'TestOne', './...'],
            cwd=self.temporary,
            env=self.environment,
            capture_output=True,
            text=True,
            check=False,
        )

    def test_pins_compiler_and_preserves_arguments(self):
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        receipt = json.loads(self.receipt.read_text())
        self.assertEqual(receipt['arguments'], ['test', '-run', 'TestOne', './...'])
        self.assertEqual(receipt['toolchain'], 'go1.27.3')
        self.assertEqual(receipt['workspace'], 'off')
        self.assertEqual(receipt['cwd'], str(ROOT))
        self.assertEqual(receipt['cache'], str(ROOT / '.cache/gobuild'))

    def test_wrong_compiler_cannot_execute_build(self):
        self.environment['TEST_COMPILER_VERSION'] = 'go1.26.1'
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('expected go1.27.3', result.stderr)
        self.assertFalse(self.receipt.exists())

    def test_failed_compiler_probe_cannot_execute_build(self):
        self.environment['TEST_PROBE_STATUS'] = '1'
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.receipt.exists())

    def test_command_failure_is_not_reported_as_success(self):
        self.environment['TEST_COMMAND_STATUS'] = '23'
        self.assertEqual(self.invoke().returncode, 23)


if __name__ == '__main__':
    unittest.main()
