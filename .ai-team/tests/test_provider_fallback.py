import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("fallback_broker", ROOT / ".ai-team/coordinator/broker.py")
B = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(B)


class ProviderFallbackTests(unittest.TestCase):
    def test_cli_quota_classification_requires_failed_process_and_stderr(self):
        cases = [(1, "You've hit your usage limit", "", True),
                 (1, "rate_limit_exceeded", "", True),
                 (1, "test failed", "", False),
                 (0, "You've hit your usage limit", "", False),
                 (1, "", "You've hit your usage limit", False)]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            cli = path / "codex"
            cli.write_text('#!/bin/sh\ncat >/dev/null\nprintf "%s\\n" "$FAKE_OUT"\nprintf "%s\\n" "$FAKE_ERR" >&2\nexit "$FAKE_EXIT"\n')
            cli.chmod(0o700)
            pack = path / "pack.md"
            pack.write_text("Assigned task")
            result = path / "result.md"
            for code, stderr, stdout, quota in cases:
                env = {**os.environ, "PATH": str(path) + os.pathsep + os.environ["PATH"],
                       "HARNESS_ROOT": str(ROOT), "HARNESS_AGENT_ROLE": "implementer",
                       "HARNESS_AGENT_PROVIDER": "codex", "HARNESS_AGENT_ISSUE": "7",
                       "HARNESS_AGENT_PACK": str(pack), "HARNESS_AGENT_RESULT": str(result),
                       "FAKE_EXIT": str(code), "FAKE_ERR": stderr, "FAKE_OUT": stdout}
                with self.subTest(code=code, stderr=stderr, stdout=stdout):
                    run = subprocess.run(["bash", str(ROOT / ".ai-team/bin/run-provider-agent")],
                                         env=env, capture_output=True, text=True)
                    self.assertEqual(run.returncode, code)
                    self.assertEqual(B.quota_failure(result), quota)

    def test_selection_respects_enablement_installation_cooldown_and_independence(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {
                "HARNESS_ROOT": tmp, "HARNESS_ENABLE_CODEX": "1", "HARNESS_ENABLE_CLAUDE": "1",
                "HARNESS_ENABLE_GEMINI": "0", "HARNESS_IMPLEMENTER_ORDER": "codex,claude,gemini",
                "HARNESS_REVIEWER_ORDER": "claude,codex,gemini", "HARNESS_PROVIDER_COOLDOWN_SECONDS": "30"}), \
             mock.patch.object(B.shutil, "which", side_effect=lambda name: "/bin/" + name), \
             mock.patch.object(B.time, "time", return_value=100) as clock:
            B.mark_provider_quota("codex", "a" * 64, "1" * 64)
            self.assertEqual(B.provider_candidate("implementer", "codex"), "claude")
            self.assertEqual(B.provider_candidate("reviewer", "claude"), "")
            self.assertEqual(B.provider_candidate("reviewer", "codex"), "claude")
            B.mark_provider_quota("claude", "b" * 64, "2" * 64)
            self.assertEqual(B.provider_candidate("implementer", "codex"), "")
            clock.return_value = 131
            B.mark_provider_quota("codex", "a" * 64, "1" * 64)  # Same job cannot extend cooldown.
            self.assertFalse(B.provider_blocked("codex"))
            self.assertEqual(B.provider_candidate("implementer", "codex"), "codex")
            B.mark_provider_quota("codex", "a" * 64, "3" * 64)  # Identical output from a new job rearms it.
            self.assertTrue(B.provider_blocked("codex"))
            self.assertEqual(B.provider_candidate("implementer", "codex"), "claude")

    def test_quota_cooldown_requires_bound_job_identity(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}):
            with self.assertRaisesRegex(B.BrokerError, "bound result/job"):
                B.mark_provider_quota("codex", "a" * 64, "")
            self.assertFalse(B.provider_blocked("codex"))

    def test_same_provider_review_recovery_preserves_rejection_and_replays_spawn_once(self):
        head, digest, recovery = "a" * 40, "b" * 64, "c" * 64
        branch, role = "ai/issue-7-test", "security-reviewer"
        action = {"issue": 7, "branch": branch, "provider": "codex", "status": "IMPLEMENTED",
                  "risk": "HIGH", "pr": 9, "head_sha": head, "implementation_digest": digest}
        pr = {"number": 9, "headRefOid": head, "baseRefName": "main"}
        item = {"id": "PVTI_7", "content": {"url": "https://github.com/acme/widget/issues/7"}}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_REPO": "acme/widget"}), \
             mock.patch.object(B, "fresh_lifecycle_item", return_value=item), \
             mock.patch.object(B, "pr_for_branch", return_value=pr), \
             mock.patch.object(B, "exact_pr", return_value=pr), \
             mock.patch.object(B, "parse_implementation_result", return_value={"commit": head, "digest": digest}), \
             mock.patch.object(B, "validate_implementation", return_value=["app.go"]), \
             mock.patch.object(B, "unit_active", return_value=False), \
             mock.patch.object(B, "provider_candidate", return_value="claude"), \
             mock.patch.object(B, "issue_content", return_value=item["content"]), \
             mock.patch.object(B, "pr_details", return_value={"statusCheckRollup": []}), \
             mock.patch.object(B, "external_validation_report", return_value=""), \
             mock.patch.object(B, "require_project_status"), \
             mock.patch.object(B, "set_status", side_effect=[B.BrokerError("interrupted status write"), None]), \
             mock.patch.object(B, "run") as spawn:
            root = Path(tmp)
            _, jobs, results, _ = B.managed_paths(7)
            result = results / f"issue-7-{role}.md"
            pack = root / f".ai-team/runtime/taskpacks/issue-7-{role}.md"
            job = jobs / f"issue-7-{role}.env"
            B.atomic_text(pack, "Previous security task")
            B.atomic_text(job, "ROLE=security-reviewer\nISSUE=7\nPROVIDER=claude\nSTARTED_AT=old\n")
            B.atomic_text(result, "\n# Harness Process Failure\n\nProvider process exit code: 1\nProvider failure kind: quota\n")
            B.atomic_text(results / "issue-7-implementer.md", "Local checks passed")
            sibling_result = results / "issue-7-reviewer.md"
            B.atomic_text(sibling_result, "Valid rejecting review: CHANGES_REQUESTED; HIGH security finding")
            sibling_metadata = B.review_metadata_path(7, "reviewer")
            B.atomic_json(sibling_metadata, {"head_sha": head, "provider": "claude", "decision": "CHANGES_REQUESTED"})
            sibling_before = (sibling_result.read_bytes(), sibling_metadata.read_bytes())
            B.atomic_json(B.review_metadata_path(7, role), {
                "version": 1, "issue": 7, "pr": 9, "role": role, "provider": "claude",
                "branch": branch, "head_sha": head, "implementation_result_digest": digest,
                "changed_files": ["app.go"], "retry_count": 2})
            waiting = {"issue": 7, "branch": branch, "role": role, "failed_provider": "claude",
                       "resume": "REVIEWING", "pending": True, "head_sha": head,
                       "implementation_result_digest": digest, "review_recovery_id": recovery,
                       "pack_digest": B.file_digest(pack), "job_digest": B.file_digest(job),
                       "result_digest": B.file_digest(result)}
            B.atomic_json(B.provider_wait_path(7), waiting)
            def launch(argv, **_kwargs):
                self.assertEqual(argv[1:3], ["security-reviewer", "claude"])
                B.atomic_text(job, "ROLE=security-reviewer\nISSUE=7\nPROVIDER=claude\nSTARTED_AT=new\n")
                B.atomic_text(result, "New completed security review")
            spawn.side_effect = launch
            with self.assertRaisesRegex(B.BrokerError, "interrupted status"):
                B.dispatch_review(action)
            self.assertTrue(json.loads(B.provider_wait_path(7).read_text())["pending"])
            B.dispatch_review(action)
            self.assertEqual(spawn.call_count, 1)
            self.assertEqual((sibling_result.read_bytes(), sibling_metadata.read_bytes()), sibling_before)
            metadata = json.loads(B.review_metadata_path(7, role).read_text())
            self.assertEqual(metadata["retry_count"], 2)
            self.assertEqual(metadata["provider_recovery_id"], recovery)
            self.assertFalse(json.loads(B.provider_wait_path(7).read_text())["pending"])

    def test_fallback_keeps_clone_and_routes_enabled_provider_without_retry_charge(self):
        head, branch = "a" * 40, "ai/issue-7-test"
        item = {"id": "PVTI_7", "Harness Status": "WAITING_PROVIDER"}
        action = {"issue": 7, "branch": branch, "provider": "codex", "status": "WAITING_PROVIDER"}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(B, "fresh_lifecycle_item", return_value=item), \
             mock.patch.object(B, "unit_active", return_value=False), \
             mock.patch.object(B, "provider_candidate", return_value="claude"), \
             mock.patch.object(B, "trusted_issue_scope"), \
             mock.patch.object(B, "sanitize_clone_metadata"), \
             mock.patch.object(B, "safe_clone_git", return_value=head), \
             mock.patch.object(B, "require_project_status"), \
             mock.patch.object(B, "set_project_field") as field, \
             mock.patch.object(B, "set_status") as status:
            path = B.provider_wait_path(7)
            B.atomic_json(path, {"issue": 7, "branch": branch, "role": "implementer", "failed_provider": "codex",
                                "resume": "RETRY_PENDING", "result_digest": "", "job_digest": "", "pack_digest": "", "head_sha": head, "pending": True})
            action["provider_wait_digest"] = B.file_digest(path)
            B.recover_provider(action)
            self.assertIn(mock.call("PVTI_7", "Provider", "Claude"), field.call_args_list)
            self.assertFalse(any(c.args[1] == "Retry Count" for c in field.call_args_list))
            status.assert_called_once_with("PVTI_7", "RETRY_PENDING")
            self.assertEqual(json.loads(path.read_text())["selected_provider"], "claude")

    def test_partially_prepared_review_fallback_waits_without_old_artifact_recheck(self):
        head, digest, recovery = "a" * 40, "b" * 64, "c" * 64
        branch = "ai/issue-7-test"
        action = {"issue": 7, "branch": branch, "provider": "codex", "pr": 9,
                  "head_sha": head, "implementation_digest": digest}
        pr = {"number": 9, "headRefOid": head}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(B, "fresh_lifecycle_item", return_value={"id": "PVTI_7"}), \
             mock.patch.object(B, "pr_for_branch", return_value=pr), \
             mock.patch.object(B, "exact_pr", return_value=pr), \
             mock.patch.object(B, "parse_implementation_result", return_value={"commit": head, "digest": digest}), \
             mock.patch.object(B, "validate_implementation", return_value=[]), \
             mock.patch.object(B, "unit_active", return_value=False), \
             mock.patch.object(B, "provider_candidate", return_value=""), \
             mock.patch.object(B, "spawn_review") as spawn, \
             mock.patch.object(B, "set_status") as status:
            B.atomic_json(B.provider_wait_path(7), {"issue": 7, "branch": branch, "role": "reviewer",
                "resume": "REVIEWING", "pending": True, "head_sha": head,
                "implementation_result_digest": digest, "review_recovery_id": recovery})
            B.atomic_json(B.review_metadata_path(7, "reviewer"), {"issue": 7, "branch": branch,
                "role": "reviewer", "pr": 9, "head_sha": head, "provider": "claude",
                "implementation_result_digest": digest, "provider_recovery_id": recovery})
            B.dispatch_review(action)
            spawn.assert_not_called()
            status.assert_not_called()

    def test_review_fallback_stages_before_pack_write_and_keeps_prior_findings(self):
        action = {"issue": 7, "branch": "ai/issue-7-test"}
        pr = {"number": 9, "headRefOid": "a" * 40, "baseRefName": "main"}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
             mock.patch.object(B, "fresh_lifecycle_item", return_value={"content": {}}), \
             mock.patch.object(B, "issue_content", return_value={}), \
             mock.patch.object(B, "unit_active", return_value=False), \
             mock.patch.object(B, "pr_details", return_value={"statusCheckRollup": []}), \
             mock.patch.object(B, "external_validation_report", return_value=""), \
             mock.patch.object(B, "run") as spawn:
            _, jobs, results, _ = B.managed_paths(7)
            pack = Path(tmp) / ".ai-team/runtime/taskpacks/issue-7-reviewer.md"
            job = jobs / "issue-7-reviewer.env"
            result = results / "issue-7-reviewer.md"
            B.atomic_text(pack, "Original review task")
            B.atomic_text(job, "Original failed job")
            B.atomic_text(result, "HIGH: finding before quota interruption")
            B.atomic_text(results / "issue-7-implementer.md", "Implementation evidence")
            kwargs = {"provider_recovery_id": "c" * 64, "provider_recovery_source_job": B.file_digest(job)}
            with mock.patch.object(B, "atomic_text", side_effect=B.BrokerError("interrupted pack write")):
                with self.assertRaisesRegex(B.BrokerError, "interrupted pack"):
                    B.spawn_review(action, "reviewer", "claude", pr, ["app.go"], "b" * 64, **kwargs)
            metadata = B.read_json_file(B.review_metadata_path(7, "reviewer"))
            self.assertEqual(metadata["provider_recovery_id"], "c" * 64)
            self.assertIn("HIGH", result.read_text())
            self.assertEqual(job.read_text(), "Original failed job")
            # A later crash can remove both old files, but the staged findings
            # still survive and the old result is never mistaken for completion.
            result.unlink()
            job.unlink()
            B.spawn_review(action, "reviewer", "claude", pr, ["app.go"], "b" * 64, **kwargs)
            spawn.assert_called_once()
            self.assertIn("HIGH: finding before quota interruption", pack.read_text())

    def test_quota_continuation_preserves_pack_and_bypasses_exhausted_product_budget_once(self):
        head, branch = "a" * 40, "ai/issue-7-test"
        scope = {"number": 7, "title": "Task", "url": "https://github.com/acme/widget/issues/7", "state": "OPEN", "body": "Required fix", "dependencies": []}
        action = {"issue": 7, "branch": branch, "provider": "claude", "status": "RETRY_PENDING", "profile": "balanced",
                  "issue_fingerprint": B.issue_scope_fingerprint(scope)}
        item = {"id": "PVTI_7", "Retry Count": "2", "Evidence": "Provider fallback"}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_REPO": "acme/widget", "HARNESS_MAX_RETRIES": "2"}), \
             mock.patch.object(B, "fresh_lifecycle_item", return_value=item), \
             mock.patch.object(B, "trusted_issue_scope", return_value=scope), \
             mock.patch.object(B, "unit_active", return_value=False), \
             mock.patch.object(B, "sanitize_clone_metadata"), \
             mock.patch.object(B, "safe_clone_git", return_value=head), \
             mock.patch.object(B, "require_project_status"), \
             mock.patch.object(B, "retry_launch_guard"), \
             mock.patch.object(B, "set_project_field") as field, \
             mock.patch.object(B, "set_status"), \
             mock.patch.object(B, "run") as spawn:
            _, jobs, results, _ = B.managed_paths(7)
            pack = Path(tmp) / ".ai-team/runtime/taskpacks/issue-7-implementer.md"
            B.atomic_text(pack, "Original rejecting review: repair durable evidence")
            result = results / "issue-7-implementer.md"
            B.atomic_text(result, "\n# Harness Process Failure\n\nProvider process exit code: 1\nProvider failure kind: quota\n")
            path = B.provider_wait_path(7)
            B.atomic_json(path, {"issue": 7, "branch": branch, "role": "implementer", "failed_provider": "codex", "selected_provider": "claude",
                                "resume": "RETRY_PENDING", "result_digest": B.file_digest(result), "job_digest": "", "pack_digest": B.file_digest(pack), "head_sha": head, "pending": True})
            action.update(prior_result_digest=B.file_digest(result), provider_wait_digest=B.file_digest(path))
            spawn.side_effect = lambda *_args, **_kwargs: B.atomic_text(
                jobs / "issue-7-implementer.env", "ROLE=implementer\nISSUE=7\nPROVIDER=claude\nSTARTED_AT=new\n")
            B.retry_implementation(action)
            self.assertEqual(spawn.call_args.args[0][2], "claude")
            self.assertIn("Original rejecting review: repair durable evidence", pack.read_text())
            self.assertIn(mock.call("PVTI_7", "Retry Count", "2"), field.call_args_list)
            self.assertFalse(json.loads(path.read_text())["pending"])

    def test_quota_review_waits_instead_of_exhausting_process_retry_budget(self):
        action = {"issue": 7, "branch": "ai/issue-7-test", "status": "REVIEWING"}
        with tempfile.TemporaryDirectory() as tmp, mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp, "HARNESS_MAX_RETRIES": "2"}), \
             mock.patch.object(B, "fresh_lifecycle_item", return_value={"id": "PVTI_7"}), \
             mock.patch.object(B, "require_project_status"), \
             mock.patch.object(B, "quota_failure", return_value=True), \
             mock.patch.object(B, "mark_provider_quota") as cooldown, \
             mock.patch.object(B, "queue_provider_wait") as waiting, \
             mock.patch.object(B, "set_status") as status:
            B.retry_review(action, "reviewer", {"provider": "claude", "retry_count": 2}, {}, {}, "quota")
            cooldown.assert_called_once()
            self.assertEqual(waiting.call_args.args[2:], ("reviewer", "claude", "REVIEWING"))
            status.assert_not_called()


if __name__ == "__main__":
    unittest.main()
