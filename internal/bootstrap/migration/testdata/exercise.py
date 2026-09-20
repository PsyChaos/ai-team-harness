#!/usr/bin/env python3
"""Exercise the preservation runbook and failed-attempt rollback offline."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
spec = importlib.util.spec_from_file_location("bootstrap_tests", ROOT / ".ai-team/tests/test_bootstrap.py")
tests = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tests)
case = tests.BootstrapTests()
case.setUp()
try:
    original = Path(__file__).with_name("graph.json").read_bytes()
    graph = json.loads(original)
    body = graph["issues"][0]["body"].encode()
    env = dict(case.env, HARNESS_BOOTSTRAP_HMAC_KEY="ab" * 32, HARNESS_REPO="test/repo")
    command = [sys.argv[1], "--repo", "test/repo"]
    migrated = subprocess.run(command, input=body, env=env, capture_output=True, check=True).stdout
    assert migrated == body, "tracking body/signature changed"
    graph["issues"][0]["body"] = migrated.decode()
    assert graph == json.loads(original), "durable state lost"
    backup = case.root / "graph.before.json"
    backup.write_bytes(original)
    case.state_path.write_text(json.dumps(graph))

    def check_python():
        # Real Python dispatch validates signature, node digests, publisher,
        # parent edges and dependencies through the persistent GitHub adapter.
        for number in (3, 4):
            subprocess.run(["python3", str(case.root / ".ai-team/bootstrap/bootstrap.py"),
                            "--check-dispatch", str(number)], env=env,
                           capture_output=True, check=True)
        durable = {key: case.state()[key] for key in graph}
        assert durable == graph

    check_python()
    # A verification failure emits no candidate and never touches durable state.
    failed = subprocess.run(command, input=body, env=dict(env, HARNESS_BOOTSTRAP_HMAC_KEY="cd" * 32),
                            capture_output=True)
    assert failed.returncode != 0 and failed.stdout == b""
    check_python()
    # Simulate failure after staging the accepted candidate, before enabling a
    # new reader. Roll back the local export from the untouched backup.
    try:
        case.state_path.write_text(json.dumps(graph))
        raise RuntimeError("injected cutover probe failure")
    except RuntimeError:
        case.state_path.write_bytes(backup.read_bytes())
    assert case.state_path.read_bytes() == original, "rollback not byte-exact"
    check_python()
    print("PASS: preserved full graph/signature; rejected wrong key; byte-exact rollback; Python dispatch verified")
finally:
    case.doCleanups()
