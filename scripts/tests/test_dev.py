import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import dev


class DevelopmentAttachmentTest(unittest.TestCase):
    def test_detach_only_updates_workspace_state(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = dev.Environment(Path(directory))
            environment.write('endpoints.json', json.dumps({'containers': {'postgres': 'existing'}}))
            environment.down()
            state = json.loads((environment.directory / 'endpoints.json').read_text())
            self.assertFalse(state['attached'])
            self.assertEqual(state['containers'], {'postgres': 'existing'})

    def test_missing_infra_does_not_deploy_a_replacement(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = dev.Environment(Path(directory))
            with mock.patch.object(dev, 'inventory', side_effect=ValueError('missing existing infra')):
                with self.assertRaisesRegex(ValueError, 'missing existing infra'):
                    environment.up()
            self.assertFalse((environment.directory / 'endpoints.json').exists())

    def test_concurrent_attachment_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = dev.Environment(Path(directory))
            with environment.lock():
                with self.assertRaises(BlockingIOError):
                    with dev.Environment(Path(directory)).lock():
                        self.fail('concurrent attachment was allowed')
