"""Reproduce issue 12's migration-only, offline manual validation.

Run from the repository root. Sources are copied to temporary read-only files;
the command writes its own separate journal. No GitHub or broker process runs.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

capture = Path(__file__).resolve().parent
names = ("snapshot.json", "github-items.json")
original = {name: (capture / name).read_bytes() for name in names}
hashes = {name: hashlib.sha256(data).hexdigest() for name, data in original.items()}
snapshot = json.loads(original[names[0]])
items = json.loads(original[names[1]])
with tempfile.TemporaryDirectory(prefix="issue-12-replay-") as temporary:
    directory = Path(temporary)
    for name, data in original.items():
        path = directory / name
        path.write_bytes(data)
        path.chmod(0o444)
    result = subprocess.run(
        ["go", "run", "./cmd/pybridge", "-snapshot", str(directory / names[0]),
         "-github-items", str(directory / names[1])],
        env={**os.environ, "GOPROXY": "off"},
        check=True, capture_output=True, text=True,
    )
    observed = json.loads(result.stdout)
    journal_path = Path(result.stderr.strip().split("temporary migration observer journal: ")[-1])
    persisted = json.loads(journal_path.read_text())
    assert all(persisted[key] == value for key, value in observed.items())
    assert not persisted["pending"]
    for name in names:
        assert (directory / name).read_bytes() == original[name]
        assert (capture / name).read_bytes() == original[name]

assert observed["version"] == 1
assert observed["cursor"] == 29
assert len(observed["events"]) == 29
assert [event["id"] for event in observed["events"]] == list(range(1, 30))
broker, *tasks = observed["events"]
assert broker["type"] == "pybridge.broker.observed"
assert broker["data"] == {
    "repo": snapshot["repo"], "created_at": snapshot["created_at"],
    "expires_at": snapshot["expires_at"], "proposed_actions": len(snapshot["actions"]),
}
for item, event in zip(items, tasks, strict=True):
    content = item["content"]
    fields = {field["field"]["name"]: field for field in item["fieldValues"]["nodes"]}
    assert event["type"] == "pybridge.task.observed"
    assert event["data"] == {
        "repo": content["repository"]["nameWithOwner"],
        "issue": content["number"], "state": content["state"],
        "status": fields.get("Harness Status", {}).get("name", ""),
        "provider": fields.get("Provider", {}).get("name", ""),
        "role": fields.get("Agent Role", {}).get("name", ""),
        "retry_count": (format(fields["Retry Count"]["number"], "g")
                        if "Retry Count" in fields else ""),
        "dependencies_complete": content["blockedBy"]["complete"],
        "dependencies": [
            {"repo": dep["repository"]["nameWithOwner"],
             "issue": dep["number"], "state": dep["state"]}
            for dep in content["blockedBy"]["nodes"]
        ],
    }
print(json.dumps({
    "result": "PASS", "source_sha256_before": hashes,
    "source_sha256_after": {
        name: hashlib.sha256((capture / name).read_bytes()).hexdigest() for name in names
    },
    "journal_version": observed["version"], "journal_cursor": observed["cursor"],
    "broker_events": 1, "task_events": len(tasks),
    "all_projected_fields_match": True, "persisted_journal_matches_stdout": True,
    "temporary_input_bytes_unchanged": True,
    "journal_path": str(journal_path),
}, indent=2))
