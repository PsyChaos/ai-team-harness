#!/usr/bin/env python3
"""Capture actual Python broker output offline and compare it with native Go."""
import argparse
import difflib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
from unittest import mock

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def canonical(value):
    return json.dumps(value, indent=2, sort_keys=True) + '\n'


def capture():
    spec = importlib.util.spec_from_file_location('broker', ROOT / '.ai-team/coordinator/broker.py')
    broker = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(broker)
    recording = json.loads((HERE / 'responses.json').read_text())
    calls = iter(recording['calls'])

    def replay(argv, **kwargs):
        call = next(calls)
        if call['operation'] == 'owner':
            assert argv == ['gh', 'api', 'users/acme'], argv
        else:
            query = next(v for v in argv if v.startswith('query='))
            assert call['operation'] in query, query
            for key, value in call['variables'].items():
                if isinstance(value, list):
                    assert [v[6:] for v in argv if v.startswith('ids[]=')] == value
                else:
                    assert f'{key}={value}' in argv, argv
        return call['response']

    with mock.patch.dict(os.environ, {
        'HARNESS_REPO': 'acme/widget', 'HARNESS_PROJECT_OWNER': 'acme',
        'HARNESS_PROJECT_NUMBER': '1',
    }, clear=True), mock.patch.object(broker, 'json_run', side_effect=replay), \
            mock.patch.object(broker.subprocess, 'run', side_effect=AssertionError('live command forbidden')):
        items = broker.project_items()
        assert next(calls, None) is None, 'unused recorded calls'
        return [{'item': item, 'status': broker.field_value(item, 'Harness Status'),
                 'retry_count': broker.field_value(item, 'Retry Count'),
                 'provider': broker.field_value(item, 'Provider'),
                 'role': broker.field_value(item, 'Agent Role'),
                 # These are unavailable, not inferred: the broker has no reader for either.
                 'labels': None, 'lease': None} for item in items]


def compare(label, expected, actual):
    if expected != actual:
        print(''.join(difflib.unified_diff(canonical(expected).splitlines(True),
                                         canonical(actual).splitlines(True),
                                         fromfile='captured-python', tofile=label)))
        raise SystemExit(1)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--capture', action='store_true', help='refresh the committed Python output')
    args = parser.parse_args()
    python = capture()
    golden = HERE / 'python-state.json'
    if args.capture:
        golden.write_text(canonical(python))
        print('Captured Python broker state for', len(python), 'items')
        return
    compare('committed-python', python, json.loads(golden.read_text()))
    with tempfile.TemporaryDirectory() as tmp:
        output = Path(tmp) / 'go-state.json'
        subprocess.run(['go', 'test', './internal/ghgateway/...', '-run', '^TestParity$', '-count=1'],
                       cwd=ROOT, env={**os.environ, 'GHGATEWAY_PARITY_OUTPUT': str(output)}, check=True)
        compare('native-go', python, json.loads(output.read_text()))
    print(f'PASS: {len(python)} items; all fields match current Python, committed capture, and Go')


if __name__ == '__main__':
    main()
