"""Verify startup failures retain diagnostics and scoped cleanup."""

import importlib.util
import io
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location('integration_runner', Path(__file__).resolve().parents[1] / 'integration.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class IntegrationFailureTest(unittest.TestCase):
    def test_naming_admission_recovers_only_bounded_startup_transport_failure(self):
        response = io.BytesIO(b'{"hosts":[]}')
        with patch.object(runner.urllib.request, 'urlopen', side_effect=[TimeoutError(), response]) as lookup:
            with patch.object(runner.time, 'sleep'):
                runner.verify_naming_ready('http://127.0.0.1:60104/nacos', 'test-only-token')
        self.assertEqual(lookup.call_count, 2)
        for call in lookup.call_args_list:
            self.assertLessEqual(call.kwargs['timeout'], 1)

    def test_naming_admission_keeps_denials_bad_payloads_and_timeout_failed(self):
        for payload in [b'{}', b'{"hosts":[{"unexpected":"instance"}]}', b'[]']:
            with self.subTest(payload=payload):
                with patch.object(runner.urllib.request, 'urlopen', return_value=io.BytesIO(payload)):
                    with self.assertRaises(ValueError):
                        runner.verify_naming_ready('http://127.0.0.1:60104/nacos', 'test-only-token')
        with patch.object(runner.urllib.request, 'urlopen', side_effect=TimeoutError()) as lookup:
            with patch.object(runner.time, 'sleep'):
                with self.assertRaisesRegex(ValueError, 'did not become ready'):
                    runner.verify_naming_ready('http://127.0.0.1:60104/nacos', 'test-only-token')
        self.assertEqual(lookup.call_count, 5)
        denied = runner.urllib.error.HTTPError('http://127.0.0.1:60104', 403, 'private-token-detail', {}, None)
        with patch.object(runner.urllib.request, 'urlopen', side_effect=denied) as lookup:
            with self.assertRaisesRegex(ValueError, '^isolated Nacos naming readiness was denied$'):
                runner.verify_naming_ready('http://127.0.0.1:60104/nacos', 'test-only-token')
        self.assertEqual(lookup.call_count, 1)

    def test_bootstrap_verifies_authenticated_naming_not_only_console(self):
        requests = []

        def open_request(request, **kwargs):
            requests.append(request)
            if request.full_url.endswith('/v1/auth/login'):
                body = {'accessToken': 'test-only-token'}
            elif '/v2/console/namespace' in request.full_url:
                body = {'code': 0, 'data': True}
            elif request.full_url.endswith('/v1/console/server/state'):
                body = {'version': '2.5.2'}
            elif '/v1/ns/instance/list?' in request.full_url:
                self.assertLessEqual(kwargs['timeout'], 1)
                self.assertEqual(request.get_header('Authorization'), 'Bearer test-only-token')
                values = runner.urllib.parse.parse_qs(runner.urllib.parse.urlparse(request.full_url).query)
                self.assertEqual(values['namespaceId'], ['integration-naming'])
                self.assertEqual(values['serviceName'], ['deer-readiness.grpc'])
                self.assertEqual(values['healthyOnly'], ['true'])
                body = {'hosts': []}
            else:
                body = {}
            return io.BytesIO(json.dumps(body).encode())

        with patch.object(runner.urllib.request, 'urlopen', side_effect=open_request):
            runner.initialize_naming('127.0.0.1:60104')
        self.assertTrue(any('/v1/ns/instance/list?' in request.full_url for request in requests))

    def test_assigned_ports_reach_only_the_owned_fixture(self):
        bindings = {
            'postgres': '127.0.0.1:60101',
            'redis': '127.0.0.1:60102',
            'nats': '127.0.0.1:60103',
            'nacos': '127.0.0.1:60104',
        }
        calls = []
        environments = []

        def execute(arguments, **kwargs):
            calls.append(arguments)
            if 'port' in arguments:
                return subprocess.CompletedProcess(arguments, 0, stdout=bindings[arguments[-2]] + '\n')
            if len(arguments) > 1 and arguments[1].endswith('/scripts/go.py'):
                environments.append(kwargs['env'])
            return subprocess.CompletedProcess(arguments, 0)

        with patch.object(runner.subprocess, 'run', side_effect=execute):
            with patch.object(runner, 'initialize_naming') as initialize:
                self.assertEqual(runner.run(), 0)
        initialize.assert_called_once_with(bindings['nacos'])
        self.assertEqual(len(environments), 2)
        for environment in environments:
            self.assertIn('port=60101 ', environment['MUSKDEER_TEST_POSTGRES_DSN'])
            self.assertEqual(environment['MUSKDEER_TEST_POSTGRES_PORT'], '60101')
            self.assertEqual(environment['MUSKDEER_TEST_REDIS_ADDRESS'], bindings['redis'])
            self.assertEqual(environment['MUSKDEER_TEST_NATS_ADDRESS'], bindings['nats'])
            self.assertEqual(environment['MUSKDEER_TEST_NACOS_ADDRESS'], bindings['nacos'])
        prefix = calls[0][:6]
        for arguments in calls:
            if 'port' in arguments:
                self.assertEqual(arguments[:6], prefix)
        self.assertEqual(calls[-1], [*prefix, 'down', '--volumes'])

    def test_published_address_rejects_unscoped_or_invalid_bindings(self):
        compose = ['docker', 'compose', '-p', 'muskdeer-unit-only', '-f', 'unit-only.yml']
        for binding in ['0.0.0.0:60101', 'localhost:60101', '127.0.0.1:0', '127.0.0.1:70000', '127.0.0.1:1\n127.0.0.1:2']:
            with self.subTest(binding=binding):
                result = subprocess.CompletedProcess([], 0, stdout=binding)
                with patch.object(runner.subprocess, 'run', return_value=result):
                    with self.assertRaises(ValueError):
                        runner.published_address(compose, 'postgres', 5432)

    def test_startup_error_survives_diagnostic_failure_and_cleans_own_project(self):
        failure = subprocess.CalledProcessError(17, ['isolated', 'startup'])
        calls = []

        def execute(arguments, **_kwargs):
            calls.append(arguments)
            if 'up' in arguments:
                raise failure
            return subprocess.CompletedProcess(arguments, 1 if 'logs' in arguments else 0)

        with patch.object(runner.subprocess, 'run', side_effect=execute):
            with self.assertRaises(subprocess.CalledProcessError) as caught:
                runner.run()
        self.assertIs(caught.exception, failure)
        self.assertEqual(len(calls), 4)
        prefix = calls[0][:6]
        self.assertEqual(prefix[:3], ['docker', 'compose', '-p'])
        self.assertTrue(prefix[3].startswith('muskdeer-p01-'))
        self.assertEqual(calls[1], [*prefix, 'logs', '--no-color', '--tail', '80'])
        self.assertEqual(calls[2][:10], [*prefix, 'exec', '-T', 'nacos', 'tail'])
        self.assertEqual(calls[-1], [*prefix, 'down', '--volumes'])

    def test_test_failure_keeps_exit_code_and_collects_scoped_evidence(self):
        for failed_package in ['./internal/persistenceprobe', './internal/migrate']:
            with self.subTest(package=failed_package):
                calls = []

                def execute(arguments, **_kwargs):
                    calls.append(arguments)
                    return subprocess.CompletedProcess(arguments, 17 if failed_package in arguments else 0)

                with patch.object(runner.subprocess, 'run', side_effect=execute):
                    with patch.object(runner, 'published_address', return_value='127.0.0.1:60101'):
                        with patch.object(runner, 'initialize_naming'):
                            self.assertEqual(runner.run(), 17)
                prefix = calls[0][:6]
                self.assertIn([*prefix, 'logs', '--no-color', '--tail', '80'], calls)
                self.assertEqual(calls[-1], [*prefix, 'down', '--volumes'])
                if failed_package == './internal/persistenceprobe':
                    self.assertFalse(any('./internal/migrate' in arguments for arguments in calls))


if __name__ == '__main__':
    unittest.main()
