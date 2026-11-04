"""Protect the shared/service dependency boundary with representative violations."""

import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location('service_layout', Path(__file__).resolve().parents[1] / 'check-layout.py')
LAYOUT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(LAYOUT)
M = LAYOUT.MODULE


class ServiceLayoutTest(unittest.TestCase):
    def test_shared_contracts_and_protocol_clients_are_allowed(self):
        self.assertEqual(LAYOUT.check_packages([
            {'ImportPath': M + 'deer/trading/repository', 'Imports': [M + 'deer/trading/outboxcontract']},
            {'ImportPath': M + 'strategy/cmd', 'Deps': [M + 'strategy/internal/app', M + 'strategy/business',
                                                       M + 'execution/delivery/executiongrpc']},
            {'ImportPath': M + 'cron/cmd', 'Deps': [M + 'cron/internal/app']},
        ]), [])

    def test_shared_code_cannot_import_service_implementation(self):
        self.assertTrue(LAYOUT.check_packages([
            {'ImportPath': M + 'deer/trading/repository', 'Imports': [M + 'execution/outbox']},
        ]))

    def test_process_cannot_assemble_another_business_or_a_fixture(self):
        for dependency in (M + 'execution/business', M + 'strategy/testbusiness'):
            with self.subTest(dependency=dependency):
                self.assertTrue(LAYOUT.check_packages([
                    {'ImportPath': M + 'strategy/cmd', 'Deps': [M + 'strategy/internal/app',
                                                              M + 'strategy/business', dependency]},
                ]))

    def test_process_requires_its_own_application_graph(self):
        self.assertTrue(LAYOUT.check_packages([{'ImportPath': M + 'market/cmd', 'Deps': []}]))
