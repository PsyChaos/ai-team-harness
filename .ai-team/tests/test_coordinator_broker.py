import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

SOURCE = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("broker", SOURCE / ".ai-team/coordinator/broker.py")
BROKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BROKER)


def item(status="READY", body="bounded task", provider="OpenAI"):
    return {"id": "PVTI_1", "Harness Status": status, "Provider": provider,
            "Model": "profile:balanced", "Agent Role": "Implementer",
            "content": {"type": "Issue", "number": 7,
                        "url": "https://github.com/acme/widget/issues/7",
                        "title": "Build safe widget", "body": body}}


def issue_scope(body="## Acceptance Criteria\n- [ ] widget is safe\n\n## Validation\n- pytest -q"):
    return {
        "number": 7,
        "url": "https://github.com/acme/widget/issues/7",
        "title": "Build safe widget",
        "body": body,
        "state": "OPEN",
        "dependencies": [{"number": 4, "url": "https://github.com/acme/widget/issues/4",
                          "title": "Prepare safety layer", "state": "CLOSED"}],
    }


def graphql_item(number=7, status="READY", provider="OpenAI", blockers=None):
    blockers = [] if blockers is None else blockers
    fields = {
        "Harness Status": status, "Provider": provider, "Model": "profile:balanced",
        "Agent Role": "Implementer", "Risk": "LOW",
    }
    nodes = [{"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": value,
              "field": {"name": name}} for name, value in fields.items()]
    return {
        "id": f"PVTI_{number}",
        "isArchived": False,
        "content": {"__typename": "Issue", "id": f"I_{number}", "number": number,
                    "url": f"https://github.com/acme/widget/issues/{number}",
                    "title": f"Task {number}", "body": f"Body {number}", "state": "OPEN",
                    "repository": {"nameWithOwner": "acme/widget"}},
        "fieldValues": {"nodes": nodes, "pageInfo": {"hasNextPage": False}},
        "_blockers": blockers,
    }


def blocker_node(number, state="OPEN"):
    return {"number": number, "url": f"https://github.com/acme/widget/issues/{number}",
            "title": f"Blocker {number}", "state": state,
            "repository": {"nameWithOwner": "acme/widget"}}


class BrokerSecurityTests(unittest.TestCase):
    def setUp(self):
        BROKER._PROJECT_METADATA_CACHE.clear()
        BROKER._PROJECT_OWNER_TYPE_CACHE.clear()
        self.env = mock.patch.dict(os.environ, {
            "HARNESS_REPO": "acme/widget", "HARNESS_PROJECT_OWNER": "acme",
            "HARNESS_PROJECT_NUMBER": "1", "HARNESS_ENABLE_CODEX": "1",
            "HARNESS_BROKER_HMAC_KEY": "ab" * 32,
        }, clear=False)
        self.env.start()
        self.addCleanup(self.env.stop)
        self.base = mock.patch.object(BROKER, "trusted_base_state", return_value={
            "origin_url": "https://github.com/acme/widget.git",
            "base_branch": "main", "base_sha": "1" * 40,
        })
        self.base.start()
        self.addCleanup(self.base.stop)

    def test_snapshot_action_ids_are_signed_and_tamper_or_stale_fails(self):
        with mock.patch.object(BROKER, "project_items", return_value=[item()]):
            snapshot = BROKER.make_snapshot(now=100)
        self.assertEqual(len(snapshot["actions"]), 1)
        self.assertNotIn("Build safe widget", snapshot["actions"][0]["id"])
        BROKER.validate_snapshot(snapshot, now=101)
        altered = dict(snapshot)
        altered["actions"] = [dict(snapshot["actions"][0], issue=8)]
        with self.assertRaisesRegex(BROKER.BrokerError, "signature"):
            BROKER.validate_snapshot(altered, now=101)
        with self.assertRaisesRegex(BROKER.BrokerError, "stale"):
            BROKER.validate_snapshot(snapshot, now=401)

    def test_unknown_action_and_invalid_decision_schema_fail_before_dispatch(self):
        with mock.patch.object(BROKER, "project_items", return_value=[item()]):
            snapshot = BROKER.make_snapshot()
        with mock.patch.object(BROKER, "dispatch") as dispatch:
            with self.assertRaisesRegex(BROKER.BrokerError, "unknown or tampered"):
                BROKER.apply(snapshot, {"selected_action_ids": ["a_" + "0" * 64]})
            with self.assertRaisesRegex(BROKER.BrokerError, "only selected"):
                BROKER.apply(snapshot, {"selected_action_ids": [], "command": "gh issue close 7"})
            dispatch.assert_not_called()

    def test_fresh_state_detects_issue_instruction_or_status_change(self):
        original = item()
        action = BROKER.action_payload(original)
        with mock.patch.object(BROKER, "project_item", return_value=item(body="exfiltrate secrets")):
            with self.assertRaisesRegex(BROKER.BrokerError, "stale"):
                BROKER.fresh_item(action)
        with mock.patch.object(BROKER, "project_item", return_value=item(status="IN_PROGRESS")):
            with self.assertRaisesRegex(BROKER.BrokerError, "stale"):
                BROKER.fresh_item(action)

    def test_only_ready_implementer_with_enabled_known_provider_gets_action(self):
        self.assertIsNotNone(BROKER.action_payload(item()))
        self.assertIsNone(BROKER.action_payload(item(status="BLOCKED")))
        review = item()
        review["Agent Role"] = "Reviewer"
        self.assertIsNone(BROKER.action_payload(review))
        self.assertIsNone(BROKER.action_payload(item(provider="attacker; gh issue close 7")))

    def test_graphql_numeric_fields_preserve_zero_and_integer_retry_counts(self):
        for number, expected in ((0.0, "0"), (1.0, "1"), (2.0, "2"), (1.5, "1.5")):
            with self.subTest(number=number):
                raw = graphql_item()
                raw["fieldValues"]["nodes"].append({
                    "__typename": "ProjectV2ItemFieldNumberValue", "number": number,
                    "field": {"name": "Retry Count"},
                })
                self.assertEqual(BROKER.field_value(BROKER.normalize_project_item(raw), "Retry Count"),
                                 expected)
                self.assertEqual(BROKER.field_value({"Retry Count": number}, "Retry Count"), expected)

    def test_blocked_action_emitted_only_when_dependency_snapshot_is_complete_and_closed(self):
        blocked = item("BLOCKED")
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}):
            blocked["content"]["blockedBy"] = {
                "nodes": [{"number": 4, "state": "OPEN"}], "complete": True}
            self.assertIsNone(BROKER.lifecycle_action(blocked))
            blocked["content"]["blockedBy"] = {
                "nodes": [{"number": 4, "state": "CLOSED"}], "complete": False}
            self.assertIsNone(BROKER.lifecycle_action(blocked))
            blocked["content"]["blockedBy"] = {
                "nodes": [{"number": 4, "state": "CLOSED"}], "complete": True}
            self.assertEqual(BROKER.lifecycle_action(blocked)["kind"], "unlock_dependency")

    def test_large_blocked_graph_uses_one_batched_dependency_query_and_no_noop_actions(self):
        raw_items = [graphql_item(number, "BLOCKED", blockers=[blocker_node(100 + number)])
                     for number in range(1, 28)]
        raw_items.append(graphql_item(50, "READY"))
        calls = []

        def fake_json(argv, **_kwargs):
            calls.append(argv)
            joined = " ".join(argv)
            if argv[:3] == ["gh", "api", "users/acme"]:
                return {"type": "Organization"}
            if "HarnessProjectItems" in joined:
                return {"data": {"organization": {"projectV2": {"items": {
                    "nodes": raw_items, "pageInfo": {"hasNextPage": False, "endCursor": None}}}},
                        "user": None}}
            if "HarnessProjectBlockers" in joined:
                return {"data": {"nodes": [{"id": value["content"]["id"],
                                    "blockedBy": {"nodes": value["_blockers"],
                                                  "pageInfo": {"hasNextPage": False}}}
                                   for value in raw_items[:-1]]}}
            raise AssertionError(argv)

        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(BROKER, "json_run", side_effect=fake_json):
            snapshot = BROKER.make_snapshot(now=100)
        self.assertEqual([action["issue"] for action in snapshot["actions"]], [50])
        self.assertEqual(len(calls), 3)
        self.assertTrue(any("HarnessProjectBlockers" in " ".join(call) for call in calls))
        self.assertFalse(any(call[:3] == ["gh", "project", "item-list"] for call in calls))

    def test_action_freshness_queries_only_exact_project_item(self):
        action = BROKER.action_payload(item())
        raw = graphql_item()
        raw["id"] = "PVTI_1"
        raw["content"]["title"] = "Build safe widget"
        raw["content"]["body"] = "bounded task"
        raw["fieldValues"]["nodes"] = [
            value for value in raw["fieldValues"]["nodes"] if value["field"]["name"] != "Risk"]
        calls = []

        def fake_json(argv, **_kwargs):
            calls.append(argv)
            return {"data": {"node": raw}}

        with mock.patch.object(BROKER, "json_run", side_effect=fake_json):
            fresh = BROKER.fresh_item(action)
        self.assertEqual(fresh["id"], "PVTI_1")
        self.assertEqual(len(calls), 1)
        self.assertIn("query HarnessProjectItem($id", " ".join(calls[0]))
        self.assertFalse(any("item-list" in value for value in calls[0]))

    def test_archived_or_unknown_archive_state_rejects_snapshot_actions(self):
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}):
            for status in ("READY", "IN_PROGRESS"):
                raw = graphql_item(status=status)
                normalized = BROKER.normalize_project_item(raw)
                with mock.patch.object(BROKER, "project_items", return_value=[normalized]):
                    action = BROKER.make_snapshot()["actions"][0]
                for archived in (True, None, "false"):
                    with self.subTest(status=status, archived=archived):
                        raw["isArchived"] = archived
                        with mock.patch.object(BROKER, "json_run", return_value={"data": {"node": raw}}):
                            check = BROKER.fresh_item if status == "READY" else BROKER.fresh_lifecycle_item
                            with self.assertRaisesRegex(BROKER.BrokerError, "archived|archive state"):
                                check(action)

    def test_project_field_metadata_is_cached_within_cycle(self):
        calls = []
        fields = [
            {"id": "STATUS", "name": "Harness Status",
             "options": [{"id": "READY", "name": "READY"}]},
            {"id": "RETRY", "name": "Retry Count"},
        ]

        def fake_json(argv, **_kwargs):
            calls.append(argv)
            if "field-list" in argv:
                return {"fields": fields}
            return {"id": "PROJECT"}

        with mock.patch.object(BROKER, "json_run", side_effect=fake_json), \
             mock.patch.object(BROKER, "run") as run:
            BROKER.set_project_field("PVTI_7", "Harness Status", "READY")
            BROKER.set_project_field("PVTI_7", "Retry Count", "1")
        self.assertEqual(len(calls), 2)
        self.assertEqual(run.call_count, 2)

    def test_rate_limit_failure_is_safely_categorized_with_reset_hint(self):
        failure = subprocess.CompletedProcess(
            ["gh"], 1, "", "GraphQL: API rate limit exceeded token=secret reset at 2030-01-02T03:04:05Z")
        with mock.patch.object(BROKER.subprocess, "run", return_value=failure):
            with self.assertRaisesRegex(BROKER.BrokerError, "rate limit exhausted.*reset") as raised:
                BROKER.run(["gh", "api", "graphql"])
        self.assertNotIn("secret", str(raised.exception))

    def test_secret_environment_is_scrubbed_from_all_unprivileged_children(self):
        os.environ.update({"HARNESS_BOOTSTRAP_HMAC_KEY": "secret", "TYPESAFE_API_KEY": "secret",
                           "GH_TOKEN": "secret", "GITHUB_TOKEN": "secret"})
        env = BROKER.safe_env()
        for name in BROKER.SECRET_NAMES:
            self.assertNotIn(name, env)

    def test_legacy_dispatch_verifier_does_not_require_bootstrap_key(self):
        os.environ.pop("HARNESS_BOOTSTRAP_HMAC_KEY", None)
        child = BROKER.bootstrap_verifier_env()
        self.assertNotIn("HARNESS_BOOTSTRAP_HMAC_KEY", child)
        os.environ["HARNESS_BOOTSTRAP_HMAC_KEY"] = "cd" * 32
        self.assertEqual(BROKER.bootstrap_verifier_env()["HARNESS_BOOTSTRAP_HMAC_KEY"], "cd" * 32)

    def test_complete_legacy_ready_dispatch_passes_no_bootstrap_key(self):
        os.environ.pop("HARNESS_BOOTSTRAP_HMAC_KEY", None)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / ".ai-team/bootstrap").mkdir(parents=True)
            (root / ".ai-team/bin").mkdir(parents=True)
            os.environ["HARNESS_ROOT"] = str(root)
            action = BROKER.action_payload(item())
            issue = {"number": 7, "url": "https://github.com/acme/widget/issues/7",
                     "title": "Build safe widget", "body": "bounded", "state": "OPEN"}

            calls = []
            def fake_run(argv, **kwargs):
                calls.append((argv, kwargs))
                if argv[:3] == ["git", "rev-parse", "--show-toplevel"]:
                    return str(root)
                if argv[:3] == ["git", "symbolic-ref", "--quiet"]:
                    return "origin/main"
                if argv[:4] == ["git", "remote", "get-url", "origin"]:
                    return "https://github.com/acme/widget.git"
                if argv[-2:] == ["rev-parse", "HEAD"]:
                    return "1" * 40
                if "--show-object-format" in argv:
                    return "sha1"
                return ""

            with mock.patch.object(BROKER, "run", side_effect=fake_run), \
                 mock.patch.object(BROKER, "fresh_item", return_value=item()), \
                 mock.patch.object(BROKER, "ensure_dependencies", return_value=issue), \
                 mock.patch.object(BROKER, "active_implementers", return_value=0), \
                 mock.patch.object(BROKER.shutil, "which", return_value="/usr/bin/codex"), \
                 mock.patch.object(BROKER, "set_status"), \
                 mock.patch.object(BROKER, "require_project_status"):
                BROKER.dispatch(action)

            verifier = next(entry for entry in calls if "--check-dispatch" in entry[0])
            self.assertNotIn("HARNESS_BOOTSTRAP_HMAC_KEY", verifier[1]["env"])

    def test_origin_url_is_exactly_bound_to_configured_repo(self):
        self.assertEqual(BROKER.validate_origin_url("git@github.com:acme/widget.git"),
                         "git@github.com:acme/widget.git")
        with self.assertRaisesRegex(BROKER.BrokerError, "does not match"):
            BROKER.validate_origin_url("git@github.com:attacker/widget.git")

    def test_managed_paths_reject_escape_and_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            parent = Path(tmp) / "managed"
            parent.mkdir()
            self.assertEqual(BROKER.safe_child(parent / "ok", parent), parent / "ok")
            with self.assertRaisesRegex(BROKER.BrokerError, "escapes"):
                BROKER.safe_child(parent / ".." / "outside", parent)
            (parent / "link").symlink_to(Path(tmp))
            with self.assertRaisesRegex(BROKER.BrokerError, "refusing symlinked"):
                BROKER.safe_child(parent / "link" / "child", parent)

    def test_json_input_and_output_refuse_symlinks(self):
        with tempfile.TemporaryDirectory() as tmp:
            target = Path(tmp) / "target.json"
            target.write_text("{}")
            link = Path(tmp) / "link.json"
            link.symlink_to(target)
            with self.assertRaisesRegex(BROKER.BrokerError, "unsafe JSON"):
                BROKER.read_json_file(link)
            with self.assertRaisesRegex(BROKER.BrokerError, "symlink output"):
                BROKER.atomic_json(link, {})

    def test_status_transition_allowlist_rejects_unknown_state(self):
        with self.assertRaisesRegex(BROKER.BrokerError, "invalid broker status"):
            BROKER.set_status("item", "ATTACKER_CONTROLLED")

    def test_dependency_gate_rejects_open_blocker(self):
        response = {"number": 7, "url": "https://github.com/acme/widget/issues/7",
                    "title": "Task", "body": "body", "state": "OPEN", "blockedBy": {"nodes": [
                        {"number": 6, "url": "https://github.com/acme/widget/issues/6",
                         "title": "Blocker", "state": "OPEN"}]}}
        with mock.patch.object(BROKER, "json_run", return_value=response):
            with self.assertRaisesRegex(BROKER.BrokerError, "incomplete dependencies"):
                BROKER.ensure_dependencies(7)

    def test_no_shell_true_exists_in_broker_runner(self):
        source = (SOURCE / ".ai-team/coordinator/broker.py").read_text()
        self.assertNotIn("shell=True", source)

    def test_implementation_parser_is_strict_and_never_executes_validation_text(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = Path(tmp) / "result.md"
            canary = Path(tmp) / "executed"
            result.write_text(
                "# Implementation Result\n\n## Outcome\nSUCCESS\n\n## Summary\nok\n\n"
                "## Commit\n" + "a" * 40 + "\n\n## Files Changed\n- x\n\n"
                "## Acceptance Criteria Mapping\n- [x] done\n\n## Validation\n"
                f"$(touch {canary}) was printed, never executed\n\n## Risks / Limitations\nnone\n"
            )
            parsed = BROKER.parse_implementation_result(result)
            self.assertEqual(parsed["commit"], "a" * 40)
            self.assertFalse(canary.exists())
            result.write_text(result.read_text().replace("- [x] done", "- [ ] done"))
            with self.assertRaisesRegex(BROKER.BrokerError, "acceptance"):
                BROKER.parse_implementation_result(result)

    def test_review_approve_with_high_finding_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = Path(tmp) / "review.md"
            result.write_text(
                "<!-- ai-harness-review:v1 -->\n\n# Review Result\n\n## Decision\nAPPROVE\n\n"
                "## Acceptance Criteria\nok\n\n## Findings\n### [HIGH] unsafe\nproblem\n\n"
                "## Validation Assessment\nbad\n\n## Residual Risks\nhigh\n\n"
                "## Reviewer Independence\nProvider: codex\nModel/session: x\n"
            )
            with self.assertRaisesRegex(BROKER.BrokerError, "blocking"):
                BROKER.parse_review_result(result)

    def test_signed_review_evidence_rejects_spoof_and_stale_head(self):
        payload = {"version": 1, "issue": 7, "pr": 9, "head_sha": "a" * 40,
                   "role": "reviewer", "provider": "claude", "decision": "APPROVE",
                   "blocking": False, "result_digest": "b" * 64}
        body = BROKER.signed_evidence(BROKER.REVIEW_EVIDENCE_MARKER, payload)
        self.assertEqual(BROKER.parse_signed_evidence(body, BROKER.REVIEW_EVIDENCE_MARKER), payload)
        self.assertIsNone(BROKER.parse_signed_evidence(body.replace('"issue": 7', '"issue": 8'),
                                                       BROKER.REVIEW_EVIDENCE_MARKER))
        pr = {"number": 9, "headRefOid": "c" * 40, "comments": [{"body": body}]}
        self.assertEqual(BROKER.verified_review_roles(pr, {"issue": 7}), set())

    def test_merge_gate_fails_closed_for_empty_pending_and_failed_ci(self):
        action = {"issue": 7, "pr": 9, "head_sha": "a" * 40, "branch": "ai/issue-7-safe", "risk": "LOW"}
        base = {"number": 9, "state": "OPEN", "isDraft": False,
                "headRefName": action["branch"], "baseRefName": "main",
                "headRefOid": action["head_sha"], "mergeable": "MERGEABLE",
                "body": "Closes #7", "comments": []}
        with mock.patch.object(BROKER, "exact_pr", side_effect=lambda pr, *_a, **_k: pr), \
             mock.patch.object(BROKER, "verified_review_roles", return_value={"reviewer"}):
            with mock.patch.object(BROKER, "pr_details", return_value=dict(base, statusCheckRollup=[])):
                with self.assertRaisesRegex(BROKER.BrokerError, "empty"):
                    BROKER.merge_gates(action)
            with mock.patch.object(BROKER, "pr_details", return_value=dict(
                    base, statusCheckRollup=[{"status": "IN_PROGRESS", "conclusion": ""}])):
                with self.assertRaisesRegex(BROKER.BrokerError, "pending"):
                    BROKER.merge_gates(action)
            with mock.patch.object(BROKER, "pr_details", return_value=dict(
                    base, statusCheckRollup=[{"status": "COMPLETED", "conclusion": "FAILURE"}])):
                with self.assertRaisesRegex(BROKER.BrokerError, "did not pass"):
                    BROKER.merge_gates(action)

    def test_high_risk_merge_requires_security_review(self):
        action = {"issue": 7, "pr": 9, "head_sha": "a" * 40,
                  "branch": "ai/issue-7-safe", "risk": "HIGH"}
        pr = {"number": 9, "state": "OPEN", "isDraft": False,
              "headRefName": action["branch"], "baseRefName": "main",
              "headRefOid": action["head_sha"], "mergeable": "MERGEABLE",
              "body": "Closes #7", "comments": [],
              "statusCheckRollup": [{"status": "COMPLETED", "conclusion": "SUCCESS"}]}
        with mock.patch.object(BROKER, "pr_details", return_value=pr), \
             mock.patch.object(BROKER, "exact_pr", return_value=pr), \
             mock.patch.object(BROKER, "verified_review_roles", return_value={"reviewer"}):
            with self.assertRaisesRegex(BROKER.BrokerError, "signed review"):
                BROKER.merge_gates(action)

    def test_merge_uses_match_head_and_never_admin(self):
        action = {"issue": 7, "item_id": "PVTI_1", "pr": 9, "head_sha": "a" * 40,
                  "branch": "ai/issue-7-safe", "kind": "merge_pr"}
        pr = {"number": 9, "headRefOid": action["head_sha"]}
        calls = []
        def completed(argv, **_kwargs):
            calls.append(argv)
            return subprocess.CompletedProcess(argv, 0, "", "")
        with mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("MERGE_READY")), \
             mock.patch.object(BROKER, "merge_gates", return_value=pr), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_status"), \
             mock.patch.object(BROKER, "run_completed", side_effect=completed):
            BROKER.merge_pr(action)
        merge = calls[0]
        self.assertIn("--match-head-commit", merge)
        self.assertIn(action["head_sha"], merge)
        self.assertNotIn("--admin", merge)

    def test_validate_implementation_accepts_isolated_clone_and_rejects_dirty_tree(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "root"
            upstream = Path(tmp) / "upstream"
            upstream.mkdir()
            subprocess.run(["git", "init", "-b", "main"], cwd=upstream, check=True, capture_output=True)
            subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=upstream, check=True)
            subprocess.run(["git", "config", "user.name", "Test"], cwd=upstream, check=True)
            (upstream / "base.txt").write_text("base\n")
            subprocess.run(["git", "add", "base.txt"], cwd=upstream, check=True)
            subprocess.run(["git", "commit", "-m", "base"], cwd=upstream, check=True, capture_output=True)
            base_sha = subprocess.run(["git", "rev-parse", "HEAD"], cwd=upstream, check=True,
                                      capture_output=True, text=True).stdout.strip()
            (root / ".worktrees").mkdir(parents=True)
            clone = root / ".worktrees/issue-7"
            subprocess.run(["git", "clone", "--no-local", str(upstream), str(clone)], check=True, capture_output=True)
            subprocess.run(["git", "checkout", "-b", "ai/issue-7-safe"], cwd=clone, check=True, capture_output=True)
            subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=clone, check=True)
            subprocess.run(["git", "config", "user.name", "Test"], cwd=clone, check=True)
            (clone / "change.txt").write_text("safe\n")
            subprocess.run(["git", "add", "change.txt"], cwd=clone, check=True)
            subprocess.run(["git", "commit", "-m", "change"], cwd=clone, check=True, capture_output=True)
            commit = subprocess.run(["git", "rev-parse", "HEAD"], cwd=clone, check=True,
                                    capture_output=True, text=True).stdout.strip()
            os.environ["HARNESS_ROOT"] = str(root)
            (root / ".ai-team/runtime/reviews").mkdir(parents=True)
            BROKER.atomic_json(BROKER.clone_metadata_path(7), {
                "version": 1, "issue": 7, "branch": "ai/issue-7-safe",
                "origin_url": "https://github.com/acme/widget.git",
                "default_branch": "main", "object_format": "sha1", "base_sha": base_sha,
            })
            canary = Path(tmp) / "git-metadata-executed"
            attack = Path(tmp) / "fsmonitor-attack.sh"
            attack.write_text(f"#!/bin/sh\ntouch {canary}\n")
            attack.chmod(0o700)
            subprocess.run(["git", "config", "core.fsmonitor", str(attack)], cwd=clone, check=True)
            subprocess.run(["git", "config", "diff.external", str(attack)], cwd=clone, check=True)
            subprocess.run(["git", "config", "remote.origin.url", "ext::sh -c 'touch /tmp/remote-helper-canary'"],
                           cwd=clone, check=True)
            hook = clone / ".git/hooks/pre-push"
            hook.write_text(f"#!/bin/sh\ntouch {canary}\n")
            hook.chmod(0o700)
            subprocess.run(["git", "update-ref", "refs/remotes/origin/main", commit], cwd=clone, check=True)
            subprocess.run(["git", "symbolic-ref", "refs/remotes/origin/HEAD",
                            "refs/remotes/origin/main"], cwd=clone, check=True)
            self.assertEqual(BROKER.validate_implementation(7, "ai/issue-7-safe", commit), ["change.txt"])
            self.assertFalse(canary.exists())
            self.assertFalse(hook.exists())
            sanitized = (clone / ".git/config").read_text()
            self.assertNotIn("ext::", sanitized)
            self.assertNotIn(str(attack), sanitized)
            (clone / "dirty.txt").write_text("dirty\n")
            with self.assertRaisesRegex(BROKER.BrokerError, "dirty"):
                BROKER.validate_implementation(7, "ai/issue-7-safe", commit)

            # Git follows a regular commondir file even when every metadata
            # entry passes symlink/hardlink checks. Never mutate its target.
            subprocess.run(["git", "update-ref", "refs/remotes/origin/canary", base_sha],
                           cwd=upstream, check=True)
            upstream_git = upstream / ".git"
            before = {p.relative_to(upstream_git): p.read_bytes()
                      for p in upstream_git.rglob("*") if p.is_file()}
            (clone / ".git/commondir").write_text(str(upstream_git) + "\n")
            with self.assertRaisesRegex(BROKER.BrokerError, "commondir"):
                BROKER.sanitize_clone_metadata(7, "ai/issue-7-safe")
            after = {p.relative_to(upstream_git): p.read_bytes()
                     for p in upstream_git.rglob("*") if p.is_file()}
            self.assertEqual(after, before)

    def test_forged_origin_head_cannot_hide_forbidden_change_from_bound_base(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "root"
            upstream = Path(tmp) / "upstream"
            upstream.mkdir()
            subprocess.run(["git", "init", "-b", "main"], cwd=upstream, check=True, capture_output=True)
            subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=upstream, check=True)
            subprocess.run(["git", "config", "user.name", "Test"], cwd=upstream, check=True)
            (upstream / "base.txt").write_text("base\n")
            subprocess.run(["git", "add", "base.txt"], cwd=upstream, check=True)
            subprocess.run(["git", "commit", "-m", "base"], cwd=upstream, check=True, capture_output=True)
            base_sha = subprocess.run(["git", "rev-parse", "HEAD"], cwd=upstream, check=True,
                                      capture_output=True, text=True).stdout.strip()
            (root / ".worktrees").mkdir(parents=True)
            clone = root / ".worktrees/issue-7"
            subprocess.run(["git", "clone", "--no-local", str(upstream), str(clone)], check=True, capture_output=True)
            subprocess.run(["git", "checkout", "-b", "ai/issue-7-safe"], cwd=clone, check=True, capture_output=True)
            subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=clone, check=True)
            subprocess.run(["git", "config", "user.name", "Test"], cwd=clone, check=True)
            hidden = clone / ".ai-team/runtime/hidden.txt"
            hidden.parent.mkdir(parents=True)
            hidden.write_text("forbidden\n")
            subprocess.run(["git", "add", str(hidden.relative_to(clone))], cwd=clone, check=True)
            subprocess.run(["git", "commit", "-m", "hidden forbidden change"], cwd=clone,
                           check=True, capture_output=True)
            forged_base = subprocess.run(["git", "rev-parse", "HEAD"], cwd=clone, check=True,
                                         capture_output=True, text=True).stdout.strip()
            (clone / "legit.txt").write_text("legit\n")
            subprocess.run(["git", "add", "legit.txt"], cwd=clone, check=True)
            subprocess.run(["git", "commit", "-m", "legit visible change"], cwd=clone,
                           check=True, capture_output=True)
            head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=clone, check=True,
                                  capture_output=True, text=True).stdout.strip()
            subprocess.run(["git", "update-ref", "refs/remotes/origin/main", forged_base],
                           cwd=clone, check=True)
            subprocess.run(["git", "symbolic-ref", "refs/remotes/origin/HEAD",
                            "refs/remotes/origin/main"], cwd=clone, check=True)
            subprocess.run(["git", "replace", base_sha, forged_base], cwd=clone, check=True)
            os.environ["HARNESS_ROOT"] = str(root)
            (root / ".ai-team/runtime/reviews").mkdir(parents=True)
            BROKER.atomic_json(BROKER.clone_metadata_path(7), {
                "version": 1, "issue": 7, "branch": "ai/issue-7-safe",
                "origin_url": "https://github.com/acme/widget.git",
                "default_branch": "main", "object_format": "sha1", "base_sha": base_sha,
            })
            with self.assertRaisesRegex(BROKER.BrokerError, "forbidden path"):
                BROKER.validate_implementation(7, "ai/issue-7-safe", head)
            restored = subprocess.run(["git", "rev-parse", "refs/remotes/origin/main"], cwd=clone,
                                      check=True, capture_output=True, text=True).stdout.strip()
            self.assertEqual(restored, base_sha)

    def test_publish_diverged_remote_never_force_pushes_and_waits_for_human(self):
        action = {"issue": 7, "item_id": "PVTI_1", "branch": "ai/issue-7-safe",
                  "implementation_digest": "d" * 64}
        implementation = {"commit": "a" * 40, "digest": "d" * 64}
        calls = []
        def fake_git(_issue, argv, **_kwargs):
            calls.append(argv)
            if "ls-remote" in argv:
                return "b" * 40 + "\trefs/heads/ai/issue-7-safe"
            return ""
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("IMPLEMENTED")), \
             mock.patch.object(BROKER, "parse_implementation_result", return_value=implementation), \
             mock.patch.object(BROKER, "validate_implementation", return_value=["safe.txt"]), \
             mock.patch.object(BROKER, "sanitize_clone_metadata", return_value={"origin_url": "https://github.com/acme/widget.git"}), \
             mock.patch.object(BROKER, "safe_clone_git", side_effect=fake_git), \
             mock.patch.object(BROKER, "pr_for_branch", return_value=None), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_status") as status:
            BROKER.publish_pr(action)
        status.assert_called_once_with("PVTI_1", "WAITING_HUMAN")
        self.assertFalse(any("--force" in arg for call in calls for arg in call))

    def test_publish_recovers_lost_pr_create_response_without_duplicate(self):
        action = {"issue": 7, "item_id": "PVTI_1", "branch": "ai/issue-7-safe",
                  "implementation_digest": "d" * 64}
        implementation = {"commit": "a" * 40, "digest": "d" * 64}
        pr = {"number": 9, "state": "OPEN", "isDraft": False,
              "headRefName": action["branch"], "baseRefName": "main",
              "headRefOid": implementation["commit"]}
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("IMPLEMENTED")), \
             mock.patch.object(BROKER, "parse_implementation_result", return_value=implementation), \
             mock.patch.object(BROKER, "validate_implementation", return_value=["safe.txt"]), \
             mock.patch.object(BROKER, "sanitize_clone_metadata", return_value={"origin_url": "https://github.com/acme/widget.git"}), \
             mock.patch.object(BROKER, "safe_clone_git", return_value=""), \
             mock.patch.object(BROKER, "default_branch", return_value="main"), \
             mock.patch.object(BROKER, "pr_for_branch", side_effect=[None, pr]), \
             mock.patch.object(BROKER, "run_completed", return_value=subprocess.CompletedProcess([], 1, "", "lost")), \
             mock.patch.object(BROKER, "exact_pr", return_value=pr), \
             mock.patch.object(BROKER, "require_project_status"):
            BROKER.publish_pr(action)

    def test_reconcile_inactive_valid_worker_transitions_to_implemented(self):
        action = {"issue": 7, "item_id": "PVTI_1", "status": "IN_PROGRESS",
                  "provider": "codex", "branch": "ai/issue-7-safe",
                  "result_digest": "d" * 64, "job_digest": "e" * 64}
        job = {"PROVIDER": "codex", "WORKDIR": "/tmp/root/.worktrees/issue-7",
               "RESULT": "/tmp/root/.ai-team/runtime/results/issue-7-implementer.md",
               "UNIT": "ai-harness-impl-7"}
        result = {"commit": "a" * 40, "digest": "d" * 64}
        with mock.patch.dict(os.environ, {"HARNESS_ROOT": "/tmp/root"}), \
             mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("IN_PROGRESS")), \
             mock.patch.object(BROKER, "parse_job", return_value=job), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "parse_implementation_result", return_value=result), \
             mock.patch.object(BROKER, "file_digest", return_value="e" * 64), \
             mock.patch.object(BROKER, "validate_implementation", return_value=["safe.txt"]), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_project_field"), \
             mock.patch.object(BROKER, "set_status") as status, \
             mock.patch.object(Path, "exists", return_value=True):
            BROKER.reconcile_implementation(action)
        status.assert_called_once_with("PVTI_1", "IMPLEMENTED")

    def test_failed_codex_worker_reconciles_to_bounded_retry(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            jobs = root / ".ai-team/runtime/jobs"
            results = root / ".ai-team/runtime/results"
            jobs.mkdir(parents=True)
            results.mkdir(parents=True)
            job_path = jobs / "issue-7-implementer.env"
            result_path = results / "issue-7-implementer.md"
            job_path.write_text(
                "ROLE=implementer\nPROVIDER=codex\nMODEL_PROFILE=balanced\nISSUE=7\n"
                "UNIT=ai-harness-impl-7\n"
                f"WORKDIR={root}/.worktrees/issue-7\nPACK={root}/.ai-team/runtime/taskpacks/issue-7-implementer.md\n"
                f"RESULT={result_path}\nSTARTED_AT=2026-09-19T00:00:00Z\nPROVIDER_HOME=\n"
            )
            result_path.write_text(
                "WARNING: could not create PATH aliases: Read-only file system\n"
                "Error: failed to initialize in-process app-server client: Read-only file system\n"
                "# Harness Process Failure\n\nProvider process exit code: 1\n"
            )
            action = {"issue": 7, "item_id": "PVTI_1", "status": "IN_PROGRESS",
                      "provider": "codex", "branch": "ai/issue-7-safe",
                      "result_digest": BROKER.file_digest(result_path),
                      "job_digest": BROKER.file_digest(job_path)}
            with mock.patch.dict(os.environ, {"HARNESS_ROOT": str(root)}), \
                 mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("IN_PROGRESS")), \
                 mock.patch.object(BROKER, "unit_active", return_value=False), \
                 mock.patch.object(BROKER, "require_project_status"), \
                 mock.patch.object(BROKER, "set_project_field") as evidence, \
                 mock.patch.object(BROKER, "set_status") as status:
                BROKER.reconcile_implementation(action)
            status.assert_called_once_with("PVTI_1", "RETRY_PENDING")
            self.assertEqual(evidence.call_args.args[1], "Evidence")

    def test_finalize_records_and_closes_before_done_then_cleans_clone(self):
        action = {"issue": 7, "item_id": "PVTI_1", "pr": 9,
                  "head_sha": "a" * 40, "branch": "ai/issue-7-safe"}
        pr = {"number": 9, "state": "MERGED", "headRefOid": "a" * 40,
              "mergedAt": "2026-09-19T00:00:00Z", "mergeCommit": {"oid": "b" * 40}}
        events = []
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            clone = root / ".worktrees/issue-7"
            clone.mkdir(parents=True)
            os.environ["HARNESS_ROOT"] = str(root)
            def fake_run(argv, **_kwargs):
                if argv[:3] == ["gh", "issue", "close"]:
                    events.append("close")
                return ""
            def fake_field(_item_id, name, _value):
                events.append(name)
            with mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("MERGING")), \
                 mock.patch.object(BROKER, "pr_details", return_value=pr), \
                 mock.patch.object(BROKER, "json_run", return_value={"state": "OPEN", "comments": []}), \
                 mock.patch.object(BROKER, "post_issue_comment", side_effect=lambda *_: events.append("comment")), \
                 mock.patch.object(BROKER, "run", side_effect=fake_run), \
                 mock.patch.object(BROKER, "require_project_status"), \
                 mock.patch.object(BROKER, "set_project_field", side_effect=fake_field), \
                 mock.patch.object(BROKER, "set_status", side_effect=lambda *_: events.append("DONE")), \
                 mock.patch.object(BROKER.shutil, "rmtree", side_effect=lambda *_: events.append("cleanup")):
                BROKER.finalize_merge(action)
        self.assertLess(events.index("comment"), events.index("DONE"))
        self.assertLess(events.index("close"), events.index("DONE"))
        self.assertLess(events.index("Evidence"), events.index("DONE"))
        self.assertLess(events.index("DONE"), events.index("cleanup"))

    def test_unlock_requires_all_native_blockers_closed_and_bootstrap_gate(self):
        action = {"issue": 7, "item_id": "PVTI_1", "status": "BLOCKED"}
        issue = {"number": 7, "state": "OPEN", "blockedBy": {"nodes": [
            {"number": 5, "state": "CLOSED"}, {"number": 6, "state": "CLOSED"}]}}
        calls = []
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("BLOCKED")), \
             mock.patch.object(BROKER, "ensure_dependencies", return_value=issue), \
             mock.patch.object(BROKER, "run", side_effect=lambda argv, **_kwargs: calls.append(argv) or ""), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_status") as status:
            BROKER.unlock_dependency(action)
        self.assertTrue(any("--check-dispatch" in call for call in calls))
        status.assert_called_once_with("PVTI_1", "READY")

    def test_blocked_with_open_dependency_is_noop_not_cycle_failure(self):
        action = {"issue": 7, "item_id": "PVTI_1", "status": "BLOCKED"}
        with mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("BLOCKED")), \
             mock.patch.object(BROKER, "ensure_dependencies",
                               side_effect=BROKER.BrokerError("issue 7 has incomplete dependencies")), \
             mock.patch.object(BROKER, "run") as run, \
             mock.patch.object(BROKER, "set_status") as status:
            BROKER.unlock_dependency(action)
        run.assert_not_called()
        status.assert_not_called()

    def test_initial_process_failure_can_enter_bounded_retry_without_review(self):
        scope = issue_scope()
        action = {"issue": 7, "item_id": "PVTI_1", "status": "RETRY_PENDING",
                  "provider": "codex", "profile": "balanced", "branch": "ai/issue-7-safe",
                  "issue_fingerprint": BROKER.issue_scope_fingerprint(scope)}
        retry_item = item("RETRY_PENDING")
        retry_item["Retry Count"] = "0"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            results = root / ".ai-team/runtime/results"
            results.mkdir(parents=True)
            (results / "issue-7-implementer.md").write_text(
                "# Harness Process Failure\n\nProvider process exit code: 1\n")
            action["prior_result_digest"] = BROKER.file_digest(results / "issue-7-implementer.md")
            with mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2"}), \
             mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=retry_item), \
             mock.patch.object(BROKER, "trusted_issue_scope", return_value=scope), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "retry_launch_guard"), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_project_field") as field, \
             mock.patch.object(BROKER, "set_status") as status, \
             mock.patch.object(BROKER, "run", return_value=""):
                BROKER.retry_implementation(action)
            pack = (root / ".ai-team/runtime/taskpacks/issue-7-implementer.md").read_text()
        field.assert_called_once_with("PVTI_1", "Retry Count", "1")
        status.assert_called_once_with("PVTI_1", "IN_PROGRESS")
        self.assertIn("Title: Build safe widget", pack)
        self.assertIn("widget is safe", pack)
        self.assertIn("pytest -q", pack)
        self.assertIn("Prepare safety layer", pack)
        self.assertIn("Provider process exit code: 1", pack)
        self.assertIn("Repository: acme/widget", pack)
        self.assertIn("Branch: ai/issue-7-safe", pack)

    def test_existing_old_head_pr_keeps_publish_action_for_safe_fast_forward(self):
        implemented = item("IMPLEMENTED")
        old = {"number": 9, "headRefOid": "a" * 40}
        with tempfile.TemporaryDirectory() as tmp:
            os.environ["HARNESS_ROOT"] = tmp
            BROKER.atomic_json(BROKER.clone_metadata_path(7), {
                "version": 1, "issue": 7, "branch": "ai/issue-7-build-safe-widget",
                "origin_url": "https://github.com/acme/widget.git", "default_branch": "main",
                "object_format": "sha1", "base_sha": "1" * 40,
            })
            with mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(BROKER, "pr_for_branch", return_value=old), \
             mock.patch.object(BROKER, "file_digest", return_value="d" * 64), \
             mock.patch.object(BROKER, "parse_implementation_result",
                               return_value={"commit": "b" * 40, "digest": "d" * 64}):
                action = BROKER.lifecycle_action(implemented)
        self.assertEqual(action["kind"], "publish_pr")
        self.assertEqual(action["pr"], 9)
        self.assertEqual(action["head_sha"], "a" * 40)

    def test_retry_publish_fast_forwards_bound_old_pr_without_force(self):
        action = {"issue": 7, "item_id": "PVTI_1", "branch": "ai/issue-7-safe",
                  "implementation_digest": "d" * 64, "pr": 9, "head_sha": "a" * 40}
        implementation = {"commit": "b" * 40, "digest": "d" * 64}
        old = {"number": 9, "state": "OPEN", "isDraft": False,
               "headRefName": action["branch"], "baseRefName": "main", "headRefOid": "a" * 40}
        new = dict(old, headRefOid="b" * 40)
        calls = []
        def safe_git(_issue, argv, **_kwargs):
            calls.append(argv)
            if "ls-remote" in argv:
                return "a" * 40 + "\trefs/heads/ai/issue-7-safe"
            return ""
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("IMPLEMENTED")), \
             mock.patch.object(BROKER, "parse_implementation_result", return_value=implementation), \
             mock.patch.object(BROKER, "validate_implementation", return_value=["safe.txt"]), \
             mock.patch.object(BROKER, "sanitize_clone_metadata", return_value={"origin_url": "https://github.com/acme/widget.git"}), \
             mock.patch.object(BROKER, "safe_clone_git", side_effect=safe_git), \
             mock.patch.object(BROKER, "safe_clone_git_completed",
                               return_value=subprocess.CompletedProcess([], 0, "", "")), \
             mock.patch.object(BROKER, "pr_for_branch", side_effect=[old, new]), \
             mock.patch.object(BROKER, "exact_pr", side_effect=lambda pr, *_a, **_k: pr), \
             mock.patch.object(BROKER, "require_project_status"):
            BROKER.publish_pr(action)
        push = next(call for call in calls if "push" in call)
        self.assertNotIn("--force", push)
        self.assertIn("HEAD:refs/heads/ai/issue-7-safe", push)

    def test_conclusive_open_pr_after_merge_failure_restores_merge_ready(self):
        action = {"issue": 7, "item_id": "PVTI_1", "pr": 9,
                  "head_sha": "a" * 40, "branch": "ai/issue-7-safe"}
        pr = {"number": 9, "state": "OPEN", "headRefOid": "a" * 40}
        with mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=item("MERGE_READY")), \
             mock.patch.object(BROKER, "merge_gates", return_value=pr), \
             mock.patch.object(BROKER, "run_completed",
                               return_value=subprocess.CompletedProcess([], 1, "", "transient")), \
             mock.patch.object(BROKER, "pr_details", return_value=pr), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_status") as status:
            BROKER.merge_pr(action)
        self.assertEqual(status.call_args_list,
                         [mock.call("PVTI_1", "MERGING"), mock.call("PVTI_1", "MERGE_READY")])

    def test_retry_pack_includes_rejecting_security_review_when_standard_approves(self):
        def review(decision, findings):
            return ("<!-- ai-harness-review:v1 -->\n\n# Review Result\n\n"
                    f"## Decision\n{decision}\n\n## Acceptance Criteria\nchecked\n\n"
                    f"## Findings\n{findings}\n\n## Validation Assessment\nchecked\n\n"
                    "## Residual Risks\nnone\n\n## Reviewer Independence\nProvider: independent\nModel/session: x\n")
        scope = issue_scope("## Acceptance Criteria\n- [ ] preserve original scope\n\n## Validation\n- make test")
        action = {"issue": 7, "item_id": "PVTI_1", "status": "CHANGES_REQUESTED",
                  "provider": "codex", "profile": "balanced", "risk": "HIGH",
                  "branch": "ai/issue-7-safe", "pr": 9, "head_sha": "a" * 40,
                  "issue_fingerprint": BROKER.issue_scope_fingerprint(scope)}
        retry_item = item("CHANGES_REQUESTED")
        retry_item["Retry Count"] = "0"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            os.environ["HARNESS_ROOT"] = str(root)
            results = root / ".ai-team/runtime/results"
            reviews = root / ".ai-team/runtime/reviews"
            results.mkdir(parents=True)
            standard = results / "issue-7-reviewer.md"
            security = results / "issue-7-security-reviewer.md"
            standard.write_text(review("APPROVE", "None."))
            security.write_text(review("CHANGES_REQUESTED", "### [HIGH] Fix auth boundary\nRequired change: close it."))
            for role, provider in (("reviewer", "claude"), ("security-reviewer", "gemini")):
                BROKER.atomic_json(reviews / f"issue-7-{role}.json", {
                    "version": 1, "issue": 7, "pr": 9, "role": role, "provider": provider,
                    "branch": action["branch"], "head_sha": action["head_sha"],
                })
            action.update({
                "review_digest": BROKER.file_digest(standard),
                "security_digest": BROKER.file_digest(security),
                "review_meta_digest": BROKER.file_digest(reviews / "issue-7-reviewer.json"),
                "security_meta_digest": BROKER.file_digest(reviews / "issue-7-security-reviewer.json"),
            })
            with mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=retry_item), \
                 mock.patch.object(BROKER, "trusted_issue_scope", return_value=scope), \
                 mock.patch.object(BROKER, "unit_active", return_value=False), \
                 mock.patch.object(BROKER, "retry_launch_guard"), \
                 mock.patch.object(BROKER, "require_project_status"), \
                 mock.patch.object(BROKER, "set_project_field"), \
                 mock.patch.object(BROKER, "set_status"), \
                 mock.patch.object(BROKER, "run", return_value=""):
                BROKER.retry_implementation(action)
            pack = (root / ".ai-team/runtime/taskpacks/issue-7-implementer.md").read_text()
        self.assertIn("Role: security-reviewer", pack)
        self.assertIn("Provider: gemini", pack)
        self.assertIn("Fix auth boundary", pack)
        self.assertNotIn("Role: reviewer\n", pack)
        self.assertIn("preserve original scope", pack)
        self.assertIn("make test", pack)
        self.assertIn("Prepare safety layer", pack)

    def test_retry_fails_closed_if_issue_scope_changes_after_snapshot(self):
        original = issue_scope()
        changed = issue_scope(body="mutated scope")
        action = {"issue": 7, "item_id": "PVTI_1", "status": "RETRY_PENDING",
                  "provider": "codex", "profile": "balanced", "branch": "ai/issue-7-safe",
                  "issue_fingerprint": BROKER.issue_scope_fingerprint(original),
                  "prior_result_digest": ""}
        retry_item = item("RETRY_PENDING")
        retry_item["Retry Count"] = "0"
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2"}), \
             mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=retry_item), \
             mock.patch.object(BROKER, "trusted_issue_scope", return_value=changed), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "run") as run:
            with self.assertRaisesRegex(BROKER.BrokerError, "issue scope changed"):
                BROKER.retry_implementation(action)
        run.assert_not_called()

    def test_retry_action_binds_fresh_issue_scope_and_prior_result(self):
        scope = issue_scope()
        retry_item = item("RETRY_PENDING")
        retry_item["Retry Count"] = "1"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result = root / ".ai-team/runtime/results/issue-7-implementer.md"
            result.parent.mkdir(parents=True)
            result.write_text("bounded failure")
            expected_digest = BROKER.file_digest(result)
            with mock.patch.dict(os.environ, {"HARNESS_ROOT": str(root)}), \
                 mock.patch.object(BROKER, "trusted_issue_scope", return_value=scope):
                action = BROKER.lifecycle_action(retry_item)
        self.assertEqual(action["issue_fingerprint"], BROKER.issue_scope_fingerprint(scope))
        self.assertEqual(action["prior_result_digest"], expected_digest)

    def test_retry_evidence_is_bounded_without_losing_tail(self):
        source = "A" * BROKER.MAX_RETRY_EVIDENCE + "TAIL_FAILURE"
        bounded = BROKER.bounded_retry_evidence(source)
        self.assertLess(len(bounded), len(source))
        self.assertIn("broker truncated", bounded)
        self.assertTrue(bounded.endswith("TAIL_FAILURE"))

    def test_legacy_scope_less_retry_pack_is_credited_once(self):
        scope = issue_scope()
        action = {"issue": 7, "item_id": "PVTI_1", "status": "RETRY_PENDING",
                  "provider": "codex", "profile": "balanced", "branch": "ai/issue-7-safe",
                  "issue_fingerprint": BROKER.issue_scope_fingerprint(scope),
                  "prior_result_digest": ""}
        retry_item = item("RETRY_PENDING")
        retry_item["Retry Count"] = "2"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            packs = root / ".ai-team/runtime/taskpacks"
            packs.mkdir(parents=True)
            (packs / "issue-7-implementer.md").write_text(
                "# Deterministic Retry Pack\n\nIssue: 7\nPrior failure only\n")
            with mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2"}), \
                 mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=retry_item), \
                 mock.patch.object(BROKER, "trusted_issue_scope", return_value=scope), \
                 mock.patch.object(BROKER, "unit_active", return_value=False), \
                 mock.patch.object(BROKER, "retry_launch_guard"), \
                 mock.patch.object(BROKER, "require_project_status"), \
                 mock.patch.object(BROKER, "set_project_field") as field, \
                 mock.patch.object(BROKER, "set_status") as status, \
                 mock.patch.object(BROKER, "run", return_value=""):
                BROKER.retry_implementation(action)
        field.assert_called_once_with("PVTI_1", "Retry Count", "2")
        status.assert_called_once_with("PVTI_1", "IN_PROGRESS")

    def test_retry_scope_mutation_at_final_launch_guard_restores_state_and_never_spawns(self):
        original = issue_scope()
        changed = issue_scope(body="changed after project mutations")
        branch = "ai/issue-7-build-safe-widget"
        action = {"issue": 7, "item_id": "PVTI_1", "status": "RETRY_PENDING",
                  "provider": "codex", "profile": "balanced", "branch": branch,
                  "risk": "LOW", "issue_fingerprint": BROKER.issue_scope_fingerprint(original)}
        retry_item = item("RETRY_PENDING")
        retry_item["Retry Count"] = "0"
        launch_item = item("IN_PROGRESS")
        launch_item["Retry Count"] = "1"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            packs = root / ".ai-team/runtime/taskpacks"
            jobs = root / ".ai-team/runtime/jobs"
            results = root / ".ai-team/runtime/results"
            for directory in (packs, jobs, results):
                directory.mkdir(parents=True)
            pack = packs / "issue-7-implementer.md"
            job = jobs / "issue-7-implementer.env"
            result = results / "issue-7-implementer.md"
            pack.write_text("# Deterministic Task Pack\noriginal\n")
            job.write_text("old job\n")
            result.write_text("old failure\n")
            action["prior_result_digest"] = BROKER.file_digest(result)
            with mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2"}), \
                 mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=retry_item), \
                 mock.patch.object(BROKER, "trusted_issue_scope",
                                   side_effect=[original, original, changed]), \
                 mock.patch.object(BROKER, "project_item", return_value=launch_item), \
                 mock.patch.object(BROKER, "unit_active", return_value=False), \
                 mock.patch.object(BROKER, "require_project_status"), \
                 mock.patch.object(BROKER, "set_project_field") as field, \
                 mock.patch.object(BROKER, "set_status") as status, \
                 mock.patch.object(BROKER, "run") as run:
                with self.assertRaisesRegex(BROKER.BrokerError, "launch boundary"):
                    BROKER.retry_implementation(action)
            self.assertEqual(pack.read_text(), "# Deterministic Task Pack\noriginal\n")
            self.assertEqual(job.read_text(), "old job\n")
            self.assertEqual(result.read_text(), "old failure\n")
        run.assert_not_called()
        self.assertEqual(field.call_args_list,
                         [mock.call("PVTI_1", "Retry Count", "1"),
                          mock.call("PVTI_1", "Retry Count", "0")])
        self.assertEqual(status.call_args_list,
                         [mock.call("PVTI_1", "IN_PROGRESS"),
                          mock.call("PVTI_1", "RETRY_PENDING")])

    def test_retry_guard_never_clobbers_concurrently_changed_project_state(self):
        scope = issue_scope()
        branch = "ai/issue-7-build-safe-widget"
        action = {"issue": 7, "item_id": "PVTI_1", "status": "RETRY_PENDING",
                  "provider": "codex", "profile": "balanced", "branch": branch,
                  "risk": "LOW", "issue_fingerprint": BROKER.issue_scope_fingerprint(scope),
                  "prior_result_digest": ""}
        retry_item = item("RETRY_PENDING")
        retry_item["Retry Count"] = "0"
        changed_item = item("IN_PROGRESS", provider="Claude")
        changed_item["Retry Count"] = "99"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            pack = root / ".ai-team/runtime/taskpacks/issue-7-implementer.md"
            pack.parent.mkdir(parents=True)
            pack.write_text("# Deterministic Task Pack\noriginal\n")
            with mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2"}), \
                 mock.patch.object(BROKER, "fresh_lifecycle_item", return_value=retry_item), \
                 mock.patch.object(BROKER, "trusted_issue_scope", side_effect=[scope, scope]), \
                 mock.patch.object(BROKER, "project_item", return_value=changed_item), \
                 mock.patch.object(BROKER, "unit_active", return_value=False), \
                 mock.patch.object(BROKER, "require_project_status"), \
                 mock.patch.object(BROKER, "set_project_field") as field, \
                 mock.patch.object(BROKER, "set_status") as status, \
                 mock.patch.object(BROKER, "run") as run:
                with self.assertRaisesRegex(BROKER.BrokerError, "without clobbering"):
                    BROKER.retry_implementation(action)
            self.assertEqual(pack.read_text(), "# Deterministic Task Pack\noriginal\n")
        run.assert_not_called()
        self.assertEqual(field.call_args_list, [mock.call("PVTI_1", "Retry Count", "1")])
        self.assertEqual(status.call_args_list, [mock.call("PVTI_1", "IN_PROGRESS")])


if __name__ == "__main__":
    unittest.main()
