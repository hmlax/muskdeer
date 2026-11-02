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

    def test_bootstrap_replay_migrates_but_does_not_reprovision_user(self):
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch.object(compose, 'DIRECTORY', Path(directory)):
                state = {'provisioned': True, 'username': 'admin', 'password': 'local-secret'}
                compose.write(Path(directory) / 'credentials.json', json.dumps(state))
                with mock.patch.object(compose.subprocess, 'run') as run:
                    compose.bootstrap()
                self.assertEqual(run.call_count, 1)
                self.assertIn('/migrate', run.call_args.args[0])
                self.assertNotIn('/adminuser', run.call_args.args[0])
                self.assertEqual(stat.S_IMODE((Path(directory) / 'credentials.json').stat().st_mode), 0o600)

    def test_configs_enable_management_without_seeding_trading_workers(self):
        snapshots = {service: ('infra-' + service, {}) for service in ('postgres', 'redis', 'nacos')}
        operator = compose.config('operator', snapshots, {'username': 'local-nacos'})
        admin = compose.config('admin', snapshots, {'username': 'local-nacos'})
        self.assertEqual(operator['postgres'], admin['postgres'])
        self.assertEqual(operator['postgres']['host'], 'infra-postgres')
        self.assertNotIn('account_ids', operator['business']['strategy'])
        self.assertEqual(operator['nacos']['registration']['advertise_ip'], '172.31.240.10')
        self.assertTrue(admin['admin_http']['allow_container_listener'])
        self.assertEqual(admin['admin_http']['public_origin'], 'http://127.0.0.1:59000')
        self.assertEqual(admin['grpc_clients']['allowed_services'], ['operator.grpc'])
