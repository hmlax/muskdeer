"""Check the downloaded administration toolchain's trust boundary."""

import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location('web_tools', Path(__file__).resolve().parents[1] / 'web.py')
WEB = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WEB)


class WebToolchainTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix='muskdeer-web-')
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.archive = self.root / 'node.tar.xz'
        self.stage = self.root / 'stage'
        self.stage.mkdir()

    def archive_members(self, members):
        with tarfile.open(self.archive, 'w:xz') as output:
            for member in members:
                output.addfile(member, io.BytesIO(b'x') if member.isfile() else None)

    def test_valid_internal_npm_symlink_is_preserved(self):
        binary = tarfile.TarInfo('node/lib/npm.js')
        binary.size = 1
        link = tarfile.TarInfo('node/bin/npm')
        link.type, link.linkname = tarfile.SYMTYPE, '../lib/npm.js'
        self.archive_members([binary, link])
        WEB.unpack_node(self.archive, self.stage, 'node')
        self.assertEqual((self.stage / 'node/bin/npm').read_bytes(), b'x')

    def test_traversal_and_unexpected_roots_are_rejected_before_writes(self):
        for name in ['node/../outside', '/node/bin/node', 'another/bin/node']:
            with self.subTest(name=name):
                member = tarfile.TarInfo(name)
                member.size = 1
                self.archive_members([member])
                with self.assertRaises(ValueError):
                    WEB.unpack_node(self.archive, self.stage, 'node')
                self.assertEqual(list(self.stage.iterdir()), [])

    def test_escaping_links_and_special_files_are_rejected(self):
        for kind, target in [(tarfile.SYMTYPE, '../../outside'), (tarfile.LNKTYPE, 'outside'), (tarfile.FIFOTYPE, '')]:
            with self.subTest(kind=kind):
                member = tarfile.TarInfo('node/bin/node')
                member.type, member.linkname = kind, target
                self.archive_members([member])
                with self.assertRaises(ValueError):
                    WEB.unpack_node(self.archive, self.stage, 'node')
                self.assertEqual(list(self.stage.iterdir()), [])

    def test_checksum_failure_never_installs_or_executes_download(self):
        tools = self.root / 'tools'
        tools.mkdir()
        (tools / 'web-versions.json').write_text(json.dumps({
            'node': '24.22.1', 'npm': '11.19.1', 'archives': {'linux-x64': '0' * 64},
        }))
        with patch.object(WEB, 'ROOT', self.root), \
                patch.object(WEB.platform, 'system', return_value='Linux'), \
                patch.object(WEB.platform, 'machine', return_value='x86_64'), \
                patch.object(WEB.urllib.request, 'urlopen', return_value=io.BytesIO(b'untrusted archive')), \
                patch.object(WEB.subprocess, 'check_output') as execute:
            with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                WEB.node_toolchain()
            execute.assert_not_called()
        self.assertFalse((self.root / '.cache/web-tools/node-v24.22.1-linux-x64').exists())


if __name__ == '__main__':
    unittest.main()
