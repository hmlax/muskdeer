"""A missing generator must leave the checked-in frontend client intact."""
import hashlib
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class CodegenToolTests(unittest.TestCase):
    def test_missing_typescript_plugin_preserves_generated_client(self):
        client = ROOT / 'adminfrontend/src/api/generated/admin/v1/index.ts'
        before = hashlib.sha256(client.read_bytes()).hexdigest()
        with tempfile.TemporaryDirectory() as empty_path:
            environment = dict(os.environ, PATH=empty_path)
            result = subprocess.run(
                [sys.executable, str(ROOT / 'scripts/admin/tools/codegen.py'), 'ts'],
                cwd=ROOT, env=environment, capture_output=True, text=True, check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('run make plugin', result.stderr)
        self.assertEqual(hashlib.sha256(client.read_bytes()).hexdigest(), before)

    def test_typescript_normalization_keeps_contracts_checked(self):
        spec = importlib.util.spec_from_file_location('codegen', ROOT / 'scripts/admin/tools/codegen.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        generated = """/* eslint-disable */
// @ts-nocheck
export type Empty = {};
export type Item = {
  children: Item[] | undefined;
};
interface TransportMeta {}
const service = {
    Ping(_request) {
      return transport.unary('/ping', 'GET');
    },
    List(request) {
      const queryParams: string[] = [];
      if (request.filterExpr.conditions.field) queryParams.push('bad');
      if (request.sorting.field) queryParams.push('bad');
      let uri = path;
      return transport.unary(uri, 'GET');
    },
};
"""
        output = module.normalize_typescript_source(generated)
        self.assertNotIn('ts-nocheck', output)
        self.assertNotIn('eslint-disable', output)
        self.assertIn('Empty = Record<string, never>;', output)
        self.assertIn('children?: Item[];', output)
        self.assertIn('Ping()', output)
        self.assertIn("['filterExpr', request.filterExpr]", output)
        self.assertIn("['sorting', request.sorting]", output)
        self.assertNotIn('conditions.field', output)
        self.assertNotIn('sorting.field', output)
        self.assertEqual(output.count('function encodeAdminQuery'), 1)
        self.assertEqual(module.normalize_typescript_source(output), output)


if __name__ == '__main__':
    unittest.main()
