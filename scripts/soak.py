#!/usr/bin/env python3
"""Run a physical-clock strategy scenario using only owned test resources."""

import argparse
import json
import os
import signal
import subprocess
import sys
import uuid

from integration import ROOT, capture_diagnostics, test_environment


def stop_owned_process(process: subprocess.Popen) -> None:
    """Terminate and join the entire new session, including native test actors."""
    for sig in (signal.SIGTERM, signal.SIGKILL):
        try:
            os.killpg(process.pid, sig)
        except ProcessLookupError:
            pass
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            continue
        # The group leader may exit before its native actors. Always send the
        # final signal to the same owned group; no host-wide process matching.
        if sig == signal.SIGKILL:
            return
    process.wait(timeout=10)


def execute_scenario(environment: dict[str, str], log, smoke: bool) -> int:
    name = 'TestStrategySoakDriverSmoke' if smoke else 'TestStrategyThirtyMinuteRealTimeSoak'
    process = subprocess.Popen([
        sys.executable, str(ROOT / 'scripts/go.py'), 'test', '-tags=integration,soak',
        '-race', '-count=1', '-v', '-timeout=40m', '-run', '^' + name + '$', './internal/migrate',
    ], cwd=ROOT, env=environment, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        code = process.wait(timeout=120 if smoke else 2400)
    except BaseException:
        stop_owned_process(process)
        raise
    if code:
        stop_owned_process(process)
    return code


def accepted_result(result: dict, smoke: bool) -> bool:
    target = 12000 if smoke else 1800000
    minimum = 2 if smoke else 600
    return (
        result.get('record_kind') == 'result' and result.get('passed') is True
        and type(result.get('target_ms')) is int and result['target_ms'] == target
        and type(result.get('elapsed_ms')) is int and result['elapsed_ms'] >= target
        and type(result.get('fill_count')) is int and result['fill_count'] >= minimum
    )


def run(smoke: bool) -> int:
    identifier = uuid.uuid4().hex
    directory = ROOT / '.cache' / 'soak' / identifier
    directory.mkdir(mode=0o700, parents=True)
    project = 'muskdeer-soak-' + identifier[:10]
    compose = ['docker', 'compose', '-p', project, '-f', str(ROOT / 'compose.test.yml')]
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    dirty = bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT, text=True))
    toolchain = subprocess.check_output([sys.executable, str(ROOT / 'scripts/go.py'), 'version'], cwd=ROOT, text=True).strip()
    metadata = {
        'revision': revision, 'worktree_dirty': dirty, 'toolchain': toolchain,
        'kind': 'smoke' if smoke else 'thirty_minute', 'accepted': False,
    }
    print('Soak artifacts: ' + str(directory), flush=True)
    try:
        subprocess.run([*compose, 'up', '-d', '--wait', '--wait-timeout', '120'], check=True)
        environment = test_environment(compose)
        environment['MUSKDEER_SOAK_ARTIFACTS'] = str(directory)
        with (directory / 'run.log').open('x', encoding='utf-8') as log:
            code = execute_scenario(environment, log, smoke)
        result_path = directory / 'result.json'
        result = json.loads(result_path.read_text()) if result_path.exists() else {}
        success = code == 0 and accepted_result(result, smoke)
        metadata['scenario_passed'] = success
        metadata['accepted'] = success and not smoke
        if not success:
            capture_diagnostics(compose)
            return code or 1
        return 0
    except BaseException:
        capture_diagnostics(compose)
        raise
    finally:
        try:
            subprocess.run([*compose, 'down', '--volumes'], check=True)
            metadata['infrastructure_cleaned'] = True
        except BaseException:
            metadata['accepted'] = False
            metadata['infrastructure_cleaned'] = False
            raise
        finally:
            (directory / 'metadata.json').write_text(json.dumps(metadata, indent=2) + '\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--smoke', action='store_true', help='12-second driver check; never A19 acceptance')
    options = parser.parse_args()
    try:
        sys.exit(run(options.smoke))
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f'Soak failed: {error}', file=sys.stderr)
        sys.exit(1)
