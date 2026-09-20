#!/usr/bin/env python3
"""Record durable state from the real Python bootstrap with offline adapters."""
import importlib.util
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
spec = importlib.util.spec_from_file_location("bootstrap_tests", ROOT / ".ai-team/tests/test_bootstrap.py")
tests = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tests)
case = tests.BootstrapTests()
case.setUp()
try:
    store = case.root / "fixture-secrets/secrets.env"
    store.parent.mkdir(mode=0o700)
    store.write_text("HARNESS_BOOTSTRAP_HMAC_KEY=" + "ab" * 32 + "\n"
                     + "HARNESS_BROKER_HMAC_KEY=" + "ab" * 32 + "\n")
    store.chmod(0o600)
    with case.runtime.open("a") as runtime:
        runtime.write(f"HARNESS_SECRETS_FILE={store}\n")
    case.launch("--brief", str(case.brief))
    state = case.state()
    # Only durable GitHub records; omit calls, environment names and temp paths.
    graph = {key: state[key] for key in ("issues", "projects", "fields", "items")}
    Path(__file__).with_name("graph.json").write_text(json.dumps(graph, indent=2, sort_keys=True) + "\n")
finally:
    case.doCleanups()
