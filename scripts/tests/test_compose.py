import json
from pathlib import Path
import stat
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import compose


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
            self.assertTrue(value['business']['enabled'])
            self.assertFalse(value['business'].get('strategy', {}).get('management_enabled', False))
        self.assertEqual(configs['strategy']['business']['strategy']['account_ids'], [compose.ACCOUNT])
        self.assertEqual(configs['portfolio']['business']['portfolio_consumer'], 'portfolio_local')
        self.assertEqual(len(configs['market']['business']['market']['feeds']), 2)
