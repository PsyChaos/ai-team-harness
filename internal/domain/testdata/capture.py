#!/usr/bin/env python3
"""Record current Python outputs using the existing offline integration adapters."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
from unittest import mock

ROOT = Path(__file__).resolve().parents[3]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def capture(output):
    output.mkdir(parents=True, exist_ok=True)

    def save(name, value):
        (output / (name + '.json')).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')

    tests = load('bootstrap_tests', '.ai-team/tests/test_bootstrap.py')
    case = tests.BootstrapTests()
    case.setUp()
    try:
        # Public test key, never an installation key. Makes signatures reproducible.
        store = case.root / 'fixture-secrets' / 'secrets.env'
        store.parent.mkdir(mode=0o700)
        store.write_text('HARNESS_BOOTSTRAP_HMAC_KEY=' + 'ab' * 32 + '\n'
                         + 'HARNESS_BROKER_HMAC_KEY=' + 'ab' * 32 + '\n')
        store.chmod(0o600)
        with case.runtime.open('a') as runtime:
            runtime.write(f'HARNESS_SECRETS_FILE={store}\n')
        case.launch('--brief', str(case.brief))
        with mock.patch.dict(os.environ, case.dispatch_env(), clear=True):
            save('bootstrap', tests.MODULE.read_envelope(case.state()['issues'][0]['body']))
    finally:
        case.doCleanups()

    tests = load('broker_tests', '.ai-team/tests/test_coordinator_broker.py')
    broker = tests.BROKER
    with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {
        'HARNESS_REPO': 'acme/widget', 'HARNESS_ENABLE_CODEX': '1',
        'HARNESS_BROKER_HMAC_KEY': 'ab' * 32,
    }, clear=True):
        item = broker.normalize_project_item(tests.graphql_item())
        item['content']['blockedBy'] = broker.normalize_blockers({
            'id': 'I_7', 'blockedBy': {'nodes': [tests.blocker_node(4, 'CLOSED')],
                                     'pageInfo': {'hasNextPage': False}},
        }, 'I_7')
        save('project-item', item)
        items = [item]
        for status in ('IN_PROGRESS', 'IMPLEMENTED', 'REVIEWING', 'CHANGES_REQUESTED',
                       'VERIFIED', 'MERGE_READY', 'MERGING', 'BLOCKED'):
            entry = tests.item(status)
            entry['content']['blockedBy'] = {'nodes': [], 'complete': True}
            items.append(entry)
        with mock.patch.object(broker, 'project_items', return_value=items), \
             mock.patch.object(broker, 'json_run', side_effect=[
                 {'defaultBranchRef': {'name': 'main'}}, {'object': {'sha': 'a' * 40}}]), \
             mock.patch.object(broker, 'managed_paths', return_value=(Path(tmp),) * 4), \
             mock.patch.object(broker, 'clone_metadata_path', return_value=Path(tmp) / 'clone.json'), \
             mock.patch.object(broker, 'review_metadata_path', return_value=Path(tmp) / 'review.json'), \
             mock.patch.object(broker, 'trusted_issue_scope', return_value=tests.issue_scope()), \
             mock.patch.object(broker, 'pr_for_branch', return_value={'number': 12, 'headRefOid': 'b' * 40}), \
             mock.patch.object(broker.secrets, 'token_hex', return_value='01' * 16):
            snapshot = broker.make_snapshot(now=1700000000)
            broker.validate_snapshot(snapshot, now=1700000000)
            save('snapshot', snapshot)

    for kind in ('task', 'retry', 'action'):
        result = subprocess.run([
            'python3', str(ROOT / '.ai-team/decision/decision_engine.py'), kind,
            '--state-file', str(ROOT / f'.ai-team/examples/decision-{kind}.json'),
        ], env={'PATH': os.environ['PATH'], 'HARNESS_DECISION_ENGINE': 'rules'},
            text=True, capture_output=True, check=True)
        save('routing-' + kind, json.loads(result.stdout))

    decision = load('decision', '.ai-team/decision/decision_engine.py')
    state = json.loads((ROOT / '.ai-team/examples/decision-retry.json').read_text())
    state['available_providers'] = ['codex']
    response = {'model': 'fixture-judgment', 'answers': {'strategy': {
        'choice': 'replan', 'confidence': 0.95, 'probabilities': {'replan': 0.95},
    }}}
    with mock.patch.dict(os.environ, {}, clear=True), \
         mock.patch.object(decision, 'typesafe_call', return_value=response):
        save('routing-judgment', decision.jev_retry(state))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=Path(__file__).resolve().parent)
    capture(parser.parse_args().output)
