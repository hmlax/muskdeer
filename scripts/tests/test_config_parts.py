import hashlib
import sys
from pathlib import Path
import unittest

sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from config_parts import split,combine,loads


class ConfigPartsTest(unittest.TestCase):
    def test_bound_split_preserves_connections_and_separates_fields(self):
        cfg={'name':'strategy','version':'test','instance_id':'one','grpc':{'address':'127.0.0.1:1234'},'postgres':{'port':5432},'venue_connection':{'target':'127.0.0.1:8080','token_env':'TEST_GATEWAY_TOKEN'}}
        data,service=split(cfg)
        self.assertEqual(combine(data,service),cfg)
        self.assertNotIn('grpc',loads(data))
        self.assertNotIn('postgres',loads(service))
        self.assertEqual(loads(service)['data_sha256'],hashlib.sha256(data.encode()).hexdigest())
        with self.assertRaises(ValueError):combine(data+'# mixed\n',service)

    def test_yaml_rejects_duplicate_keys_aliases_and_unquoted_decimals(self):
        for body in ['name: a\nname: b\n','x: &a [1]\ny: *a\n','quantity: 0.123456789012345678\n','port: 0xFF\n']:
            with self.assertRaises(ValueError):loads(body)

    def test_business_parameters_cannot_be_published_to_nacos(self):
        for business in ({}, {'enabled': False}, {'venue': {'depth': 20}}, {'strategy': {'account_ids': ['test-account']}}):
            with self.subTest(business=business), self.assertRaisesRegex(ValueError, 'unknown runtime configuration fields'):
                split({'name': 'strategy', 'version': 'test', 'business': business})
