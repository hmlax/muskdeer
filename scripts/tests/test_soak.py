"""Soak acceptance and owned-process cleanup safety."""

import importlib.util
import json
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
spec = importlib.util.spec_from_file_location('soak_runner', Path(__file__).resolve().parents[1] / 'soak.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class SoakRunnerTest(unittest.TestCase):
    def test_smoke_cannot_substitute_for_physical_thirty_minutes(self):
        result = dict(record_kind='result', passed=True, target_ms=12000, elapsed_ms=12001, fill_count=6)
        self.assertTrue(runner.accepted_result(result, True))
        self.assertFalse(runner.accepted_result(result, False))
        result.update(target_ms=1800000, elapsed_ms=1799999, fill_count=900)
        self.assertFalse(runner.accepted_result(result, False))
        result['elapsed_ms'] = 1800000
        self.assertTrue(runner.accepted_result(result, False))
        result['passed'] = False
        self.assertFalse(runner.accepted_result(result, False))

    def test_timeout_terminates_only_its_owned_group_and_joins(self):
        process = Mock(pid=32101)
        process.wait.side_effect = [subprocess.TimeoutExpired('test', 120), subprocess.TimeoutExpired('test', 10), -9]
        with patch.object(runner.subprocess, 'Popen', return_value=process) as start:
            with patch.object(runner.os, 'killpg') as kill:
                with self.assertRaises(subprocess.TimeoutExpired):
                    runner.execute_scenario({}, Mock(), True)
        self.assertTrue(start.call_args.kwargs['start_new_session'])
        self.assertEqual(kill.call_args_list[0].args, (32101, signal.SIGTERM))
        self.assertEqual(kill.call_args_list[1].args, (32101, signal.SIGKILL))
        self.assertEqual(process.wait.call_count, 3)

    def test_abnormal_exit_still_cleans_native_descendants(self):
        process = Mock(pid=32102)
        process.wait.return_value = 2
        with patch.object(runner.subprocess, 'Popen', return_value=process):
            with patch.object(runner.os, 'killpg') as kill:
                self.assertEqual(runner.execute_scenario({}, Mock(), False), 2)
        self.assertEqual(kill.call_args_list[0].args, (32102, signal.SIGTERM))
        self.assertEqual(kill.call_args_list[1].args, (32102, signal.SIGKILL))

    def test_cleanup_failure_never_leaves_accepted_metadata(self):
        def execute(environment, _log, _smoke):
            result = dict(record_kind='result', passed=True, target_ms=1800000, elapsed_ms=1800001, fill_count=900)
            (Path(environment['MUSKDEER_SOAK_ARTIFACTS']) / 'result.json').write_text(json.dumps(result))
            return 0

        def compose(arguments, **_kwargs):
            if 'down' in arguments:
                raise subprocess.CalledProcessError(17, arguments)
            return subprocess.CompletedProcess(arguments, 0)

        with tempfile.TemporaryDirectory() as directory:
            with patch.object(runner, 'ROOT', Path(directory)):
                with patch.object(runner.subprocess, 'check_output', side_effect=['unit-revision', '', 'go version go1.27.3']):
                    with patch.object(runner.subprocess, 'run', side_effect=compose):
                        with patch.object(runner, 'test_environment', return_value={}):
                            with patch.object(runner, 'execute_scenario', side_effect=execute):
                                with self.assertRaises(subprocess.CalledProcessError):
                                    runner.run(False)
            metadata_path, = Path(directory).glob('.cache/soak/*/metadata.json')
            metadata = json.loads(metadata_path.read_text())
            self.assertTrue(metadata['scenario_passed'])
            self.assertFalse(metadata['accepted'])
            self.assertFalse(metadata['infrastructure_cleaned'])


if __name__ == '__main__':
    unittest.main()
