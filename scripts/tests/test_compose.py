import json
from pathlib import Path
import stat
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import compose


class ComposeEnvironmentTest(unittest.TestCase):
    def test_default_environment_follows_prepared_settings_after_repeated_prepare(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            runtime = root / '.cache/compose'
            with mock.patch.object(compose, 'ROOT', root), mock.patch.object(compose, 'DIRECTORY', runtime):
                compose.write(runtime / 'compose.env', "MUSKDEER_BUILD_CONTEXT='first-source'\n")
                self.assertTrue(compose.install_default_environment())
                self.assertEqual((root / '.env').read_text(), "MUSKDEER_BUILD_CONTEXT='first-source'\n")
                self.assertFalse((root / '.env').readlink().is_absolute())
                compose.write(runtime / 'compose.env', "MUSKDEER_BUILD_CONTEXT='second-source'\n")
                self.assertTrue(compose.install_default_environment())
                self.assertEqual((root / '.env').read_text(), "MUSKDEER_BUILD_CONTEXT='second-source'\n")

    def test_existing_project_environment_is_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            owned = root / '.env'
            owned.write_text('EXISTING_SETTING=keep-me\n')
            with mock.patch.object(compose, 'ROOT', root), mock.patch.object(compose, 'DIRECTORY', root / '.cache/compose'):
                self.assertFalse(compose.install_default_environment())
                self.assertEqual(owned.read_text(), 'EXISTING_SETTING=keep-me\n')
                self.assertFalse(owned.is_symlink())

    def test_existing_external_environment_symlink_is_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            owned = root / '.env'
            owned.symlink_to('user-settings.env')
            with mock.patch.object(compose, 'ROOT', root), mock.patch.object(compose, 'DIRECTORY', root / '.cache/compose'):
                self.assertFalse(compose.install_default_environment())
                self.assertEqual(owned.readlink(), Path('user-settings.env'))


class ComposeResourcesTest(unittest.TestCase):
    def test_existing_database_without_matching_ownership_is_never_adopted(self):
        state = {'database_password': 'local-secret', 'ownership': 'workspace-token'}
        snapshot = ('infra-postgres', {'Config': {'Env': ['POSTGRES_USER=infra-owner']}})
        with mock.patch.object(compose, 'run', side_effect=['muskdeer_local|other-owner', 'other-owner', 'muskdeer_local']) as run:
            with self.assertRaisesRegex(ValueError, 'ownership'):
                compose.prepare_database(snapshot, state, False)
        self.assertEqual(run.call_count, 3)
        self.assertTrue(all(call.kwargs['input'].startswith('SELECT ') for call in run.call_args_list))

    def test_repeated_database_preparation_does_not_reset_credentials(self):
        state = {'database_password': 'local-secret', 'ownership': 'workspace-token'}
        marker = 'muskdeer-compose:workspace-token'
        snapshot = ('infra-postgres', {'Config': {'Env': ['POSTGRES_USER=infra-owner']}})
        with mock.patch.object(compose, 'run', side_effect=['muskdeer_local|' + marker, marker, 'muskdeer_local']) as run:
            compose.prepare_database(snapshot, state, False)
        self.assertEqual(run.call_count, 3)
        self.assertTrue(all(call.kwargs['input'].startswith('SELECT ') for call in run.call_args_list))

    def test_bootstrap_replay_prepares_core_without_any_administrator(self):
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch.object(compose, 'DIRECTORY', Path(directory)):
                state = {'ownership': 'local-token'}
                compose.write(Path(directory) / 'credentials.json', json.dumps(state))
                resources = {'account_id': compose.ACCOUNT, 'instrument_id': '1', 'binance_feed_id': '2', 'okx_feed_id': '3'}
                with mock.patch.object(compose.subprocess, 'run') as run, mock.patch.object(compose, 'run', return_value=json.dumps(resources)), mock.patch.object(compose, 'prepare'):
                    compose.bootstrap()
                    compose.bootstrap()
                self.assertEqual(run.call_count, 2)
                self.assertIn('/migrate', run.call_args.args[0])
                self.assertNotIn('/adminuser', run.call_args.args[0])
                self.assertEqual(json.loads((Path(directory) / 'resources.json').read_text()), resources)
                self.assertEqual(stat.S_IMODE((Path(directory) / 'credentials.json').stat().st_mode), 0o600)

    def test_configs_enable_four_distinct_core_processes_without_management(self):
        snapshots = {service: ('infra-' + service, {}) for service in ('postgres', 'redis', 'nacos', 'nats')}
        resources = {'binance_feed_id': '2', 'okx_feed_id': '3'}
        configs = {role: compose.config(role, snapshots, {'username': 'local-nacos'}, {'username': 'muskdeer-local'}, resources) for role in compose.CORES}
        self.assertEqual(len({value['nacos']['registration']['advertise_ip'] for value in configs.values()}), 4)
        self.assertEqual(len({value['grpc']['address'] for value in configs.values()}), 4)
        for role, value in configs.items():
            self.assertEqual(value['name'], role)
            self.assertNotIn('admin_http', value)
            self.assertNotIn('http', value)
            self.assertNotIn('business', value)
        self.assertIn('grpc_clients', configs['strategy'])
        self.assertIn('jetstream', configs['portfolio'])
