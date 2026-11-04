"""Exercise ownership, wire namespace compatibility and Go package routing."""

from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from proto_layout import validate_layout


class ProtoLayoutTest(unittest.TestCase):
    def make_source(self, root, relative, package=None, service=''):
        source = root / 'deer/proto' / relative
        source.parent.mkdir(parents=True, exist_ok=True)
        package = package or 'github.com/hmlax/muskdeer/' + source.parent.relative_to(root).as_posix()
        source.write_text('syntax = "proto3";\npackage legacy.namespace;\n'
                          f'option go_package = "{package};v1";\n{service}')

    def test_service_and_common_owners_keep_existing_wire_namespace(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for owner in ('common', 'market', 'strategy', 'execution', 'portfolio', 'cron'):
                self.make_source(root, owner + '/model/v1/entity.proto')
            self.make_source(root, 'common/probe/v1/probe.proto', service='service Probe {}')
            validate_layout(root)

    def test_old_type_directories_are_rejected(self):
        for directory in ('model', 'config', 'rpc', 'hertz', 'himalaya'):
            with self.subTest(directory=directory), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                self.make_source(root, directory + '/v1/entity.proto')
                with self.assertRaisesRegex(ValueError, 'first directory'):
                    validate_layout(root)

    def test_go_package_must_follow_the_owning_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.make_source(root, 'execution/v1/orders.proto', package='github.com/hmlax/muskdeer/deer/proto/common/v1')
            with self.assertRaisesRegex(ValueError, 'go_package'):
                validate_layout(root)

    def test_business_rpc_cannot_be_hidden_in_common(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.make_source(root, 'common/v1/orders.proto', service='service Execution {}')
            with self.assertRaisesRegex(ValueError, 'business services'):
                validate_layout(root)

    def test_configuration_uses_one_shared_proto(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.make_source(root, 'common/config/v1/config.proto')
            validate_layout(root)
            self.make_source(root, 'market/config/v1/market.proto')
            with self.assertRaisesRegex(ValueError, 'configuration definitions must share'):
                validate_layout(root)

    def test_configuration_namespace_cannot_escape_into_another_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.make_source(root, 'common/v1/another.proto')
            path = root / 'deer/proto/common/v1/another.proto'
            path.write_text(path.read_text().replace('package legacy.namespace;', 'package muskdeer.config.v1;'))
            with self.assertRaisesRegex(ValueError, 'configuration definitions must share'):
                validate_layout(root)
