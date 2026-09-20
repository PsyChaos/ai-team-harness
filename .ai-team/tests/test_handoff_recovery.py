import importlib.util
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location(
    "recovery_broker", Path(__file__).resolve().parents[1] / "coordinator/broker.py")
BROKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BROKER)


class HandoffRecoveryTests(unittest.TestCase):
    def test_observation_export_preserves_real_states_and_omits_unneeded_text(self):
        content = {"type": "Issue", "number": 7, "state": "OPEN", "repository": {"nameWithOwner": "acme/widget"},
                   "blockedBy": {"nodes": [], "complete": True}, "body": "large private description"}
        status = {"field": {"name": "Harness Status"}, "name": "WAITING_HUMAN"}
        item = {"id": "PVTI_7", "content": content, "fieldValues": {"nodes": [status, {"field": {"name": "Evidence"}, "text": "unnecessary evidence"}]}}
        exported = BROKER.observation_items([item])[0]
        self.assertEqual(exported["content"]["blockedBy"], content["blockedBy"])
        self.assertNotIn("body", exported["content"])
        self.assertEqual(exported["fieldValues"]["nodes"], [status])

    def test_security_worker_receives_shared_required_review_contract(self):
        root = Path(SPEC.origin).parents[2]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            (path / "codex").write_text("#!/bin/sh\ncat\n")
            (path / "codex").chmod(0o700)
            pack = path / "pack.md"
            pack.write_text("Review the assigned security-sensitive change.")
            result = path / "result.md"
            env = {**os.environ, "PATH": str(path) + os.pathsep + os.environ["PATH"],
                   "HARNESS_ROOT": str(root), "HARNESS_AGENT_ROLE": "security-reviewer",
                   "HARNESS_AGENT_PROVIDER": "codex", "HARNESS_AGENT_ISSUE": "7",
                   "HARNESS_AGENT_PACK": str(pack), "HARNESS_AGENT_RESULT": str(result)}
            subprocess.run(["bash", str(root / ".ai-team/bin/run-provider-agent")], env=env,
                           capture_output=True, check=True)
            prompt = result.read_text()
            self.assertIn("reachable input/state", prompt)
            self.assertIn("<!-- ai-harness-review:v1 -->", prompt)
            self.assertIn("## Decision", prompt)
            self.assertIn("APPROVE | CHANGES_REQUESTED", prompt)

    def test_validation_context_is_signed_head_bound_and_never_approval(self):
        head = "a" * 40
        item = {"content": {"number": 7}}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {
                "HARNESS_ROOT": tmp, "HARNESS_REPO": "acme/widget", "HARNESS_BROKER_HMAC_KEY": "ab" * 32}), \
             mock.patch.object(BROKER, "project_items", return_value=[item]), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "canonical_branch", return_value="ai/issue-7-test"), \
             mock.patch.object(BROKER, "sanitize_clone_metadata"), \
             mock.patch.object(BROKER, "validate_implementation"), \
             mock.patch.object(BROKER, "safe_clone_git", return_value=head), \
             mock.patch.object(BROKER, "set_status") as status:
            with self.assertRaisesRegex(BROKER.BrokerError, "current HEAD"):
                BROKER.record_validation_context(7, "Report for " + "b" * 40)
            BROKER.record_validation_context(7, "Actual captured input " + head)
            self.assertIn("Actual captured input", BROKER.validation_context(7, head))
            self.assertEqual(BROKER.validation_context(7, "b" * 40), "")
            status.assert_not_called()
            path = BROKER.managed_paths(7)[3] / "issue-7-validation-context.md"
            path.write_text(path.read_text().replace("Actual captured input", "fake pass"))
            self.assertEqual(BROKER.validation_context(7, head), "")

    def test_resume_review_keeps_malformed_findings_and_grants_one_attempt(self):
        branch, head, digest = "ai/issue-7-test", "a" * 40, "b" * 64
        item = {"id": "PVTI_7", "Harness Status": "WAITING_HUMAN", "content": {"number": 7}}
        metadata = {"issue": 7, "pr": 9, "role": "security-reviewer", "branch": branch,
                    "head_sha": head, "implementation_result_digest": digest, "retry_count": 2}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2"}), \
             mock.patch.object(BROKER, "project_items", return_value=[item]), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "trusted_issue_scope"), \
             mock.patch.object(BROKER, "canonical_branch", return_value=branch), \
             mock.patch.object(BROKER, "pr_for_branch", return_value={"state": "OPEN", "number": 9, "headRefName": branch, "headRefOid": head}), \
             mock.patch.object(BROKER, "parse_implementation_result", return_value={"commit": head, "digest": digest}), \
             mock.patch.object(BROKER, "validate_implementation"), \
             mock.patch.object(BROKER, "read_json_file", return_value=metadata), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_project_field"), \
             mock.patch.object(BROKER, "set_status") as status:
            result = BROKER.managed_paths(7)[2] / "issue-7-security-reviewer.md"
            result.parent.mkdir(parents=True)
            result.write_text("HIGH: preserve this actual blocking finding")
            BROKER.resume_review(7, "security-reviewer", "Fixed missing structured output contract")
            self.assertIn("HIGH", result.read_text())
            self.assertEqual(metadata["retry_count"], 1)
            status.assert_called_once_with("PVTI_7", "REVIEWING")
            status.reset_mock()
            with mock.patch.object(BROKER, "parse_review_result", return_value={"decision": "CHANGES_REQUESTED"}):
                with self.assertRaisesRegex(BROKER.BrokerError, "review is valid"):
                    BROKER.resume_review(7, "security-reviewer", "Try again")
            status.assert_not_called()

    def test_only_broker_bound_integration_merge_is_accepted(self):
        base, prior, head = "a" * 40, "b" * 40, "c" * 40
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}):
            clone = Path(tmp) / ".worktrees/issue-7"
            (clone / ".git").mkdir(parents=True)
            branch = "ai/issue-7-test"
            def git(_issue, args):
                return {("rev-parse", "--show-toplevel"): str(clone),
                        ("branch", "--show-current"): branch,
                        ("status", "--porcelain", "--untracked-files=all"): "",
                        ("rev-parse", "HEAD"): head,
                        ("cat-file", "-t", base): "commit",
                        ("rev-list", "--count", f"{base}..{head}"): "2",
                        ("rev-list", "--merges", f"{base}..{head}"): head,
                        ("rev-list", "--parents", "-n", "1", head): f"{head} {prior} {base}"}[tuple(args)]
            with mock.patch.object(BROKER, "safe_clone_git", side_effect=git), \
                 mock.patch.object(BROKER, "safe_clone_git_completed", return_value=subprocess.CompletedProcess([], 0, "app.go\0", "")), \
                 mock.patch.object(BROKER, "sanitize_clone_metadata") as metadata:
                metadata.return_value = {"base_sha": base, "integration_parent_sha": prior}
                self.assertEqual(BROKER.validate_implementation(7, branch, head), ["app.go"])
                for binding in [None, "d" * 40]:
                    metadata.return_value = {"base_sha": base, "integration_parent_sha": binding}
                    with self.assertRaises(BROKER.BrokerError):
                        BROKER.validate_implementation(7, branch, head)

    def test_conflicting_pr_prepares_bound_merge_and_queues_assigned_worker(self):
        old, upstream = "a" * 40, "b" * 40
        action = {"issue": 7, "branch": "ai/issue-7-test"}
        metadata = {"base_sha": "c" * 40, "origin_url": "https://github.com/acme/widget.git", "default_branch": "main"}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2", "HARNESS_REPO": "acme/widget"}), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "parse_implementation_result", return_value={"commit": old}), \
             mock.patch.object(BROKER, "validate_implementation"), \
             mock.patch.object(BROKER, "sanitize_clone_metadata", return_value=metadata), \
             mock.patch.object(BROKER, "safe_clone_git", return_value=upstream), \
             mock.patch.object(BROKER, "safe_clone_git_completed", return_value=subprocess.CompletedProcess([], 1, "", "")) as git, \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "atomic_json") as save, \
             mock.patch.object(BROKER, "set_project_field"), \
             mock.patch.object(BROKER, "set_status") as status:
            BROKER.prepare_upstream_integration(action, {"id": "PVTI_7"}, {"headRefOid": old})
            self.assertEqual(save.call_args.args[1]["integration_parent_sha"], old)
            self.assertEqual(save.call_args.args[1]["base_sha"], upstream)
            self.assertIn("--no-commit", git.call_args.args[1])
            status.assert_called_once_with("PVTI_7", "RETRY_PENDING")

    def test_interrupted_integration_replays_transition_without_merging_again(self):
        old, upstream = "a" * 40, "b" * 40
        branch = "ai/issue-7-test"
        metadata = {"base_sha": upstream, "integration_parent_sha": old,
                    "integration_target_sha": upstream, "integration_pending": True}
        def git(_issue, args):
            return {("rev-parse", "HEAD"): old, ("branch", "--show-current"): branch,
                    ("rev-parse", "MERGE_HEAD"): upstream}[tuple(args)]
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "parse_implementation_result", return_value={"commit": old}), \
             mock.patch.object(BROKER, "validate_implementation") as validate, \
             mock.patch.object(BROKER, "sanitize_clone_metadata", return_value=metadata), \
             mock.patch.object(BROKER, "safe_clone_git", side_effect=git), \
             mock.patch.object(BROKER, "safe_clone_git_completed", return_value=subprocess.CompletedProcess([], 0, upstream, "")) as completed, \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "atomic_json"), \
             mock.patch.object(BROKER, "set_project_field"), \
             mock.patch.object(BROKER, "set_status") as status:
            BROKER.prepare_upstream_integration({"issue": 7, "branch": branch}, {"id": "PVTI_7"}, {"headRefOid": old})
            validate.assert_not_called()
            completed.assert_called_once_with(7, ["rev-parse", "--verify", "MERGE_HEAD"])
            status.assert_called_once_with("PVTI_7", "RETRY_PENDING")
            self.assertFalse(metadata["integration_pending"])

    def test_browser_registration_rejects_old_head_and_signs_current_head(self):
        head = "a" * 40
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {
                "HARNESS_ROOT": tmp, "HARNESS_REPO": "acme/widget", "HARNESS_BROKER_HMAC_KEY": "ab" * 32}), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "canonical_branch", return_value="ai/issue-7-test"), \
             mock.patch.object(BROKER, "sanitize_clone_metadata", return_value={}), \
             mock.patch.object(BROKER, "validate_implementation"), \
             mock.patch.object(BROKER, "safe_clone_git", return_value=head):
            report = Path(tmp) / "browser.md"
            report.write_text("Decision: PASS\n" + "b" * 40)
            with self.assertRaisesRegex(BROKER.BrokerError, "current HEAD"):
                BROKER.register_external_validation(7, {}, report)
            report.write_text("Decision: PASS\n" + head)
            BROKER.register_external_validation(7, {}, report)
            self.assertIn(head, BROKER.external_validation_report(7, head))
            self.assertEqual(BROKER.external_validation_report(7, "b" * 40), "")
            self.assertTrue(json.loads(BROKER.clone_metadata_path(7).read_text())["browser_validation_required"])

    def test_integration_cannot_reuse_checked_browser_acceptance_from_old_head(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(BROKER, "parse_implementation_result", return_value={"commit": "b" * 40, "pending_validation": []}), \
             mock.patch.object(BROKER, "external_validation_report", side_effect=lambda _issue, head: "old PASS" if head == "a" * 40 else ""):
            metadata = BROKER.clone_metadata_path(7)
            metadata.parent.mkdir(parents=True)
            metadata.write_text(json.dumps({"integration_parent_sha": "a" * 40}))
            with self.assertRaisesRegex(BROKER.BrokerError, "signed PASS"):
                BROKER.require_pending_validation(7, "b" * 40, [])
            metadata.write_text(json.dumps({"browser_validation_required": True}))
            with self.assertRaisesRegex(BROKER.BrokerError, "signed PASS"):
                BROKER.require_pending_validation(7, "b" * 40, [])

    def test_expired_claude_auth_refresh_disables_tools_and_excludes_harness_secrets(self):
        script = Path(SPEC.origin).parents[1] / "bin/refresh-claude-auth"
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            executable = root / "claude"
            executable.write_text(
                '#!/bin/bash\nset -eu\n'
                'test "$PWD" = /tmp\n'
                'test -z "${HARNESS_BROKER_HMAC_KEY:-}${HARNESS_BOOTSTRAP_HMAC_KEY:-}${TYPESAFE_API_KEY:-}${GH_TOKEN:-}${GITHUB_TOKEN:-}"\n'
                'printf "%s\\n" "$@" > "$AUTH_TEST_ARGS"\n')
            executable.chmod(0o700)
            args_path = root / "args"
            env = {**os.environ, "PATH": str(root) + os.pathsep + os.environ["PATH"],
                   "CLAUDE_CONFIG_DIR": tmp, "AUTH_TEST_ARGS": str(args_path),
                   "HARNESS_BROKER_HMAC_KEY": "test-only", "GH_TOKEN": "test-only"}
            credentials = root / ".credentials.json"
            credentials.write_text(json.dumps({"claudeAiOauth": {"expiresAt": 0}}))
            subprocess.run(["bash", str(script)], env=env, check=True, capture_output=True)
            args = args_path.read_text().splitlines()
            self.assertEqual(args[args.index("--tools") + 1], "")
            self.assertEqual(args[args.index("--setting-sources") + 1], "")
            self.assertIn('{"disableAllHooks":true}', args)
            self.assertIn('{"mcpServers":{}}', args)
            self.assertIn("--no-session-persistence", args)
            args_path.unlink()
            credentials.write_text(json.dumps({"claudeAiOauth": {"expiresAt": 99_999_999_999_999}}))
            subprocess.run(["bash", str(script)], env=env, check=True, capture_output=True)
            self.assertFalse(args_path.exists())

    def test_reviewer_process_failure_retries_review_only_with_a_bound(self):
        action = {"issue": 7, "branch": "ai/issue-7-test"}
        implementation = {"commit": "a" * 40, "digest": "b" * 64}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {
                "HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2"}), \
             mock.patch.object(BROKER, "fresh_lifecycle_item", return_value={"id": "PVTI_7"}), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_project_field"), \
             mock.patch.object(BROKER, "set_status") as status, \
             mock.patch.object(BROKER, "validate_implementation", return_value=["app.go"]), \
             mock.patch.object(BROKER, "spawn_review") as spawn:
            BROKER.retry_review(action, "reviewer", {"provider": "claude"}, {}, implementation, "auth expired")
            self.assertEqual(spawn.call_args.kwargs, {"retry_count": 1})
            status.assert_not_called()
            spawn.reset_mock()
            BROKER.retry_review(action, "reviewer", {"provider": "claude", "retry_count": 2}, {}, implementation, "failed")
            status.assert_called_once_with("PVTI_7", "WAITING_HUMAN")
            spawn.assert_not_called()

    def test_review_respawn_checks_snapshot_before_replacing_failed_evidence(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}):
            root = Path(tmp)
            results = root / ".ai-team/runtime/results"
            jobs = root / ".ai-team/runtime/jobs"
            results.mkdir(parents=True)
            jobs.mkdir(parents=True)
            old_result = results / "issue-7-reviewer.md"
            old_result.write_text("HIGH: unsafe worker-controlled Git hooks")
            (results / "issue-7-implementer.md").write_text("Local checks passed")
            action = {"issue": 7, "branch": "ai/issue-7-test"}
            pr = {"number": 9, "headRefOid": "a" * 40, "baseRefName": "main"}

            def fresh(_action):
                self.assertEqual(old_result.read_text(), "HIGH: unsafe worker-controlled Git hooks")
                return {"content": {"url": "https://github.com/acme/widget/issues/7"}}

            with mock.patch.object(BROKER, "unit_active", return_value=False), \
                 mock.patch.object(BROKER, "fresh_lifecycle_item", side_effect=fresh), \
                 mock.patch.object(BROKER, "issue_content", side_effect=lambda item: item["content"]), \
                 mock.patch.object(BROKER, "external_validation_report", return_value=""), \
                 mock.patch.object(BROKER, "pr_details", return_value={"statusCheckRollup": []}), \
                 mock.patch.object(BROKER, "run") as run:
                BROKER.spawn_review(action, "reviewer", "claude", pr, ["app.go"], "b" * 64, retry_count=1)
            self.assertFalse(old_result.exists())
            pack = root / ".ai-team/runtime/taskpacks/issue-7-reviewer.md"
            self.assertIn("HIGH: unsafe worker-controlled Git hooks", pack.read_text())
            metadata = json.loads((root / ".ai-team/runtime/reviews/issue-7-reviewer.json").read_text())
            self.assertEqual(metadata["retry_count"], 1)
            self.assertIn("spawn-agent", run.call_args.args[0][0])

    def test_only_explicit_external_checks_can_be_deferred(self):
        template = (
            "# Implementation Result\n\n## Outcome\n{outcome}\n\n## Summary\nReady\n\n"
            "## Commit\n" + "a" * 40 + "\n\n## Files Changed\n- code.go\n\n"
            "## Acceptance Criteria Mapping\n- [x] Local implementation tested\n{pending}\n\n"
            "## Validation\nLocal Go tests passed\n\n## Risks / Limitations\nPending external validation\n")
        cases = [
            ("VALIDATION_PENDING", "- [ ] [external:ci] Hosted CI", True),
            ("VALIDATION_PENDING", "- [ ] [external:browser] Browser evidence", True),
            ("VALIDATION_PENDING", "- [ ] Missing implementation", False),
            ("VALIDATION_PENDING", "- [ ] [external:unknown] Unknown gate", False),
            ("VALIDATION_PENDING", "", False),
            ("SUCCESS", "- [ ] [external:ci] Hosted CI", False),
            ("PARTIAL", "- [ ] [external:ci] Hosted CI", False),
        ]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "result.md"
            for outcome, pending, valid in cases:
                with self.subTest(outcome=outcome, pending=pending):
                    path.write_text(template.format(outcome=outcome, pending=pending))
                    if valid:
                        self.assertEqual(BROKER.parse_implementation_result(path)["pending_validation"], [pending])
                    else:
                        with self.assertRaises(BROKER.BrokerError):
                            BROKER.parse_implementation_result(path)

    def test_resume_grants_one_retry_and_preserves_prior_attempt_count_in_audit(self):
        item = {"id": "PVTI_7", "Harness Status": "WAITING_HUMAN", "Agent Role": "Implementer",
                "Retry Count": "2", "content": {"number": 7}}
        with mock.patch.dict(os.environ, {"HARNESS_MAX_RETRIES": "2"}), \
             mock.patch.object(BROKER, "project_items", return_value=[item]), \
             mock.patch.object(BROKER, "issue_content", return_value={"number": 7}), \
             mock.patch.object(BROKER, "unit_active", return_value=False), \
             mock.patch.object(BROKER, "trusted_issue_scope"), \
             mock.patch.object(BROKER, "require_project_status"), \
             mock.patch.object(BROKER, "set_project_field") as fields, \
             mock.patch.object(BROKER, "set_status") as status:
            BROKER.resume_implementation(7, "Go toolchain provisioned")
        self.assertIn("previous retry count 2", fields.call_args_list[0].args[2])
        self.assertEqual(fields.call_args_list[1].args, ("PVTI_7", "Retry Count", "1"))
        status.assert_called_once_with("PVTI_7", "RETRY_PENDING")

    def test_resume_refuses_active_workers_or_nonwaiting_state(self):
        for state, active in [("IN_PROGRESS", False), ("WAITING_HUMAN", True)]:
            item = {"id": "PVTI_7", "Harness Status": state, "Agent Role": "Implementer"}
            with self.subTest(state=state, active=active), \
                 mock.patch.object(BROKER, "project_items", return_value=[item]), \
                 mock.patch.object(BROKER, "issue_content", return_value={"number": 7}), \
                 mock.patch.object(BROKER, "unit_active", return_value=active), \
                 mock.patch.object(BROKER, "set_project_field") as fields:
                with self.assertRaises(BROKER.BrokerError):
                    BROKER.resume_implementation(7, "fixed")
                fields.assert_not_called()

    def test_transition_is_audited_and_comment_failure_does_not_advance(self):
        item = {"id": "PVTI_7", "Status": "In Progress", "Harness Status": "IN_PROGRESS", "Provider": "OpenAI", "Model": "profile:strong",
                "Evidence": "External CI pending"}
        bodies = []

        def capture(argv):
            bodies.append(Path(argv[-1]).read_text())

        with mock.patch.dict(os.environ, {"HARNESS_REPO": "acme/widget"}), \
             mock.patch.object(BROKER, "project_item", return_value=item), \
             mock.patch.object(BROKER, "issue_content", return_value={"number": 7}), \
             mock.patch.object(BROKER, "run", side_effect=capture), \
             mock.patch.object(BROKER, "set_project_field") as field:
            BROKER.set_status("PVTI_7", "IMPLEMENTED")
        self.assertIn("IN_PROGRESS → IMPLEMENTED", bodies[0])
        self.assertIn("External CI pending", bodies[0])
        field.assert_called_once_with("PVTI_7", "Harness Status", "IMPLEMENTED")
        with mock.patch.dict(os.environ, {"HARNESS_REPO": "acme/widget"}), \
             mock.patch.object(BROKER, "project_item", return_value=item), \
             mock.patch.object(BROKER, "issue_content", return_value={"number": 7}), \
             mock.patch.object(BROKER, "run", side_effect=BROKER.BrokerError("offline")), \
             mock.patch.object(BROKER, "set_project_field") as field:
            with self.assertRaises(BROKER.BrokerError):
                BROKER.set_status("PVTI_7", "IMPLEMENTED")
            field.assert_not_called()

    def test_operator_resume_does_not_receive_legacy_retry_credit(self):
        with tempfile.TemporaryDirectory() as tmp:
            pack = Path(tmp) / "pack.md"
            pack.write_text("# Deterministic Retry Pack\nlegacy retry")
            self.assertEqual(BROKER.legacy_retry_credit(pack, 1), 0)
            self.assertEqual(BROKER.legacy_retry_credit(pack, 1, operator_recovery=True), 1)

    def test_overlapping_timer_tick_skips_but_operator_commands_fail(self):
        with tempfile.TemporaryDirectory() as tmp:
            lock_path = Path(tmp) / ".ai-team/runtime/coordinator.lock"
            lock_path.parent.mkdir(parents=True)
            with lock_path.open("w") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX)
                env = {**os.environ, "HARNESS_ROOT": tmp}
                run = subprocess.run([sys.executable, SPEC.origin, "run"], env=env,
                                     capture_output=True, text=True, check=False)
                self.assertEqual(run.returncode, 0, run.stderr)
                self.assertIn("skipping overlapping", run.stdout)
                snapshot = subprocess.run([sys.executable, SPEC.origin, "snapshot", "--output", tmp + "/snap.json"],
                                          env=env, capture_output=True, text=True, check=False)
                self.assertEqual(snapshot.returncode, 1)
                self.assertIn("another coordinator", snapshot.stderr)

    def test_standard_status_tracks_active_complete_and_waiting_states(self):
        for harness, expected in [("IN_PROGRESS", "In Progress"), ("IMPLEMENTED", "In Progress"),
                                  ("REVIEWING", "In Progress"), ("DONE", "Done"), ("BLOCKED", "Todo")]:
            with self.subTest(harness=harness), mock.patch.object(BROKER, "set_project_field") as field:
                BROKER.sync_standard_status({"id": "PVTI_7"}, harness)
                field.assert_called_once_with("PVTI_7", "Status", expected)

    def test_deferred_browser_gate_requires_signed_pass_for_exact_head(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {
                "HARNESS_ROOT": tmp, "HARNESS_REPO": "acme/widget", "HARNESS_BROKER_HMAC_KEY": "ab" * 32}):
            root = Path(tmp)
            result_dir = root / ".ai-team/runtime/results"
            result_dir.mkdir(parents=True)
            (result_dir / "issue-7-implementer.md").write_text(
                "# Implementation Result\n\n## Outcome\nVALIDATION_PENDING\n\n## Summary\nReady\n\n"
                "## Commit\n" + "a" * 40 + "\n\n## Files Changed\n- app.js\n\n"
                "## Acceptance Criteria Mapping\n- [x] Local checks passed\n"
                "- [ ] [external:browser] Browser tests\n\n## Validation\nLocal tests passed\n\n"
                "## Risks / Limitations\nBrowser unavailable\n")
            with self.assertRaisesRegex(BROKER.BrokerError, "signed PASS"):
                BROKER.require_pending_validation(7, "a" * 40, [])
            reviews = root / ".ai-team/runtime/reviews"
            reviews.mkdir()
            path = reviews / "issue-7-external-validation.md"
            payload = {"repo": "acme/widget", "issue": 7, "head_sha": "a" * 40,
                       "kind": "browser", "outcome": "PASS", "report": "Browser checks passed"}
            for changed in [{"head_sha": "b" * 40}, {"outcome": "FAIL"}, {"kind": "ci"}, {"issue": 8}]:
                path.write_text(BROKER.signed_evidence(BROKER.EXTERNAL_VALIDATION_MARKER, dict(payload, **changed)))
                with self.assertRaisesRegex(BROKER.BrokerError, "signed PASS"):
                    BROKER.require_pending_validation(7, "a" * 40, [])
            path.write_text(BROKER.signed_evidence(BROKER.EXTERNAL_VALIDATION_MARKER, payload))
            BROKER.require_pending_validation(7, "a" * 40, [])
            path.write_text(path.read_text().replace("Browser checks passed", "tampered"))
            with self.assertRaisesRegex(BROKER.BrokerError, "signed PASS"):
                BROKER.require_pending_validation(7, "a" * 40, [])
