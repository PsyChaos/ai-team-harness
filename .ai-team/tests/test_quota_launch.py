import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("quota_launch", Path(__file__).resolve().parents[1] / "coordinator/broker.py")
B = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(B)


class QuotaLaunchTests(unittest.TestCase):
    def test_interrupted_launch_restores_evidence_and_entitlement(self):
        for new_job, new_result, restored in [(False, False, True), (True, False, True), (True, True, False)]:
            with self.subTest(new_job=new_job, new_result=new_result), tempfile.TemporaryDirectory() as tmp, \
                 mock.patch.dict(os.environ, {"HARNESS_ROOT": tmp}), \
                 mock.patch.object(B, "unit_active", return_value=False), \
                 mock.patch.object(B, "require_project_status"), \
                 mock.patch.object(B, "set_status") as status:
                action = {"issue": 7, "branch": "ai/issue-7-test", "provider": "claude", "status": "IN_PROGRESS"}
                _, jobs, results, _ = B.managed_paths(7)
                job = jobs / "issue-7-implementer.env"
                result = results / "issue-7-implementer.md"
                B.atomic_json(B.provider_wait_path(7), {"pending": True, "job_digest": "old"})
                B.atomic_json(B.quota_launch_path(7), {**action, "pack": "original correction", "job": "old job", "result": "quota"})
                if new_job:
                    B.atomic_text(job, "new job")
                if new_result:
                    B.atomic_text(result, "new completed result")
                self.assertEqual(B.recover_quota_launch(action, {"id": "item"}), restored)
                self.assertFalse(B.quota_launch_path(7).exists())
                self.assertEqual(B.read_json_file(B.provider_wait_path(7))["pending"], restored)
                if restored:
                    self.assertEqual(result.read_text(), "quota")
                    self.assertEqual(job.read_text(), "old job")
                    status.assert_called_once_with("item", "RETRY_PENDING")
                else:
                    self.assertEqual(result.read_text(), "new completed result")
                    status.assert_not_called()
