import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import dev


class DevelopmentOwnershipTest(unittest.TestCase):
    def test_down_ignores_forged_state_and_keeps_volumes(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = dev.Environment(Path(directory))
            environment.directory.mkdir(parents=True)
            (environment.directory / 'endpoints.json').write_text(json.dumps({'project': 'unrelated-user-project'}))
            with mock.patch.object(dev.subprocess, 'run') as run:
                environment.down()
            args = run.call_args.args[0]
            self.assertIn(environment.project, args)
            self.assertNotIn('unrelated-user-project', args)
            self.assertNotIn('--volumes', args)
            self.assertNotEqual(environment.project, dev.Environment(Path(directory) / 'other').project)

    def test_failed_start_does_not_delete_persistent_data(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = dev.Environment(Path(directory))
            with mock.patch.object(dev.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'up')) as run:
                with self.assertRaises(subprocess.CalledProcessError):
                    environment.up()
            self.assertEqual(run.call_count, 1)
            self.assertNotIn('down', run.call_args.args[0])

    def test_concurrent_operation_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = dev.Environment(Path(directory))
            with environment.lock():
                with self.assertRaises(BlockingIOError):
                    with dev.Environment(Path(directory)).lock():
                        self.fail('concurrent resource operations were allowed')
