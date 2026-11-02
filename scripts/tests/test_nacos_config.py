import sys
from pathlib import Path
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from nacos_config import ConfigStore, remote_content


class NacosConfigurationTest(unittest.TestCase):
    def test_remote_configuration_retains_registration_but_never_bootstrap_credentials(self):
        config = {'name': 'strategy', 'business': {'strategy': {'account_ids': ['own-account']}},
                  'nacos': {'address': 'bootstrap-only:8848', 'username': 'bootstrap-only',
                            'password_env': 'NACOS_PASSWORD', 'registration': {'advertise_ip': '127.0.0.1'}}}
        result = remote_content(config)
        self.assertEqual(result['nacos'], {'registration': {'advertise_ip': '127.0.0.1'}})
        self.assertEqual(result['business'], config['business'])
        self.assertIn('address', config['nacos'])

    def test_preparation_does_not_overwrite_existing_nacos_business_configuration(self):
        store = object.__new__(ConfigStore)
        with mock.patch.object(store, 'get', return_value='{"name":"strategy","business":"operator-owned"}'), mock.patch.object(store, 'request') as request:
            self.assertFalse(store.publish('strategy.json', {'name': 'strategy'}))
        request.assert_not_called()
