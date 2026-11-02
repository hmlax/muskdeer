import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import infra


class ExistingInfraTest(unittest.TestCase):
    def test_inventory_only_inspects_existing_container(self):
        snapshot = {'State': {'Running': True}}
        result = subprocess.CompletedProcess([], 0, stdout=json.dumps([snapshot]))
        with patch.object(infra.subprocess, 'run', return_value=result) as execute:
            name, value = infra.inventory('postgres')
        self.assertEqual(name, 'infra-postgres')
        self.assertEqual(value, snapshot)
        self.assertEqual(execute.call_args.args[0], ['docker', 'inspect', 'infra-postgres'])

    def test_published_wildcard_binds_are_used_through_loopback(self):
        value = {'NetworkSettings': {'Ports': {'5432/tcp': [{'HostIp': '0.0.0.0', 'HostPort': '5432'}]}}}
        self.assertEqual(infra.endpoint('postgres', value), '127.0.0.1:5432')
        value['NetworkSettings']['Ports']['5432/tcp'] = []
        with self.assertRaises(ValueError):
            infra.endpoint('postgres', value)

    def test_failure_cleans_only_its_created_database_and_releases_lock(self):
        session = infra.Infra(('postgres', 'nacos'))
        session.database = 'muskdeer_test_12345678'
        with patch.object(session, 'sql') as sql:
            session.__exit__(None, None, None)
        self.assertEqual(sql.call_args.args[0], 'DROP DATABASE "muskdeer_test_12345678" WITH (FORCE);')
        self.assertIsNone(session.lock)

    def test_no_script_deploys_or_tears_down_infrastructure(self):
        for path in (infra.ROOT / 'scripts').glob('*.py'):
            text = path.read_text()
            self.assertNotIn("'compose.test.yml'", text, path.name)
            self.assertNotIn("'compose.dev.yml'", text, path.name)
            self.assertNotIn("'down', '--volumes'", text, path.name)

    def test_shared_redis_is_not_paused_by_integration(self):
        source = (infra.ROOT / 'internal/infrastructureprobe/redis_test.go').read_text()
        self.assertNotIn('.ClientPause(', source)
        self.assertNotIn('.ClientUnpause(', source)
