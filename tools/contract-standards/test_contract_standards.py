from __future__ import annotations

import copy
import json
import tempfile
import unittest
from pathlib import Path

from openapi_spec_validator import OpenAPIV31SpecValidator
from openapi_spec_validator.readers import read_from_filename

from check_contract_standards import (
    ContractStandardsError,
    corpus_manifest_sha256,
    json_pointer,
    load_json,
    repository_path,
    run,
    validate_profile,
    validate_runtime,
)


ROOT = Path(__file__).resolve().parents[2]
PROFILE_PATH = ROOT / "tools" / "contract-standards" / "profile.json"


class ContractStandardsTest(unittest.TestCase):
    def test_checked_in_profile_and_contracts(self) -> None:
        result = run(ROOT, PROFILE_PATH)
        self.assertEqual(result["status"], "INDEPENDENT_CONTRACT_STANDARDS_VALIDATED")
        self.assertEqual(result["jsonSchemaOfficialSuite"]["assertions"], 1299)
        self.assertEqual(result["currentJsonSchema"]["cases"], 71)
        self.assertEqual(result["openapi31"]["operations"], 9)
        self.assertTrue(result["notGateClosure"])
        self.assertEqual(result["gateStatus"], "ALL_GATES_OPEN")

    def test_runtime_rejects_lock_digest_drift(self) -> None:
        profile = copy.deepcopy(load_json(PROFILE_PATH))
        profile["toolchain"]["lock"]["sha256"] = "0" * 64
        with self.assertRaisesRegex(ContractStandardsError, "lock SHA-256 mismatch"):
            validate_runtime(profile, ROOT)

    def test_boundary_cannot_claim_gate_closure(self) -> None:
        profile = copy.deepcopy(load_json(PROFILE_PATH))
        profile["implementationBoundary"]["gateStatus"] = "CLOSED"
        with self.assertRaisesRegex(ContractStandardsError, "implementation boundary mismatch"):
            validate_profile(profile)

    def test_production_ajv_official_suite_cannot_be_claimed_without_a_runner(self) -> None:
        profile = copy.deepcopy(load_json(PROFILE_PATH))
        profile["jsonSchemaOfficialSuite"]["productionAjvOfficialSuiteAudit"]["status"] = "PASS"
        with self.assertRaisesRegex(ContractStandardsError, "must remain NOT_RUN_NOT_CLAIMED"):
            validate_profile(profile)

    def test_source_tree_has_no_python_bytecode(self) -> None:
        bytecode = sorted(
            path.relative_to(ROOT).as_posix()
            for path in (ROOT / "tools" / "contract-standards").rglob("*.pyc")
        )
        caches = sorted(
            path.relative_to(ROOT).as_posix()
            for path in (ROOT / "tools" / "contract-standards").rglob("__pycache__")
        )
        self.assertEqual(bytecode, [])
        self.assertEqual(caches, [])

    def test_corpus_manifest_binds_path_content_and_size(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = root / "a.json"
            second = root / "nested" / "b.json"
            second.parent.mkdir()
            first.write_text("{}\n", encoding="utf-8")
            second.write_text("[]\n", encoding="utf-8")
            before = corpus_manifest_sha256(root)
            second.write_text("[0]\n", encoding="utf-8")
            after = corpus_manifest_sha256(root)
            self.assertEqual(before[1], 2)
            self.assertEqual(after[1], 2)
            self.assertNotEqual(before[0], after[0])

    def test_repository_paths_reject_escape_and_absolute_inputs(self) -> None:
        with self.assertRaisesRegex(ContractStandardsError, "escapes its repository boundary"):
            repository_path(ROOT, ROOT / "contracts", "../../outside.json", "fixture")
        with self.assertRaisesRegex(ContractStandardsError, "must be repository-relative"):
            repository_path(ROOT, ROOT, "/tmp/outside.json", "profile")

    def test_json_pointer_is_rfc6901_exact(self) -> None:
        document = {"a/b": {"~key": ["value"]}}
        self.assertEqual(json_pointer(document, "/a~1b/~0key/0"), "value")
        with self.assertRaisesRegex(ContractStandardsError, "array pointer token"):
            json_pointer(document, "/a~1b/~0key/00")
        with self.assertRaisesRegex(ContractStandardsError, "RFC 6901 escape"):
            json_pointer(document, "/a~2b")

    def test_openapi31_engine_rejects_version_drift(self) -> None:
        path = ROOT / "contracts" / "managed-agent" / "v1alpha1" / "openapi.json"
        spec, base_uri = read_from_filename(str(path))
        drifted = copy.deepcopy(spec)
        drifted["openapi"] = "3.0.3"
        self.assertNotEqual(list(OpenAPIV31SpecValidator(drifted, base_uri=base_uri).iter_errors()), [])

    def test_strict_json_rejects_duplicate_keys(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "duplicate.json"
            path.write_text('{"value":1,"value":2}\n', encoding="utf-8")
            with self.assertRaisesRegex(ContractStandardsError, "duplicate JSON object key"):
                load_json(path)


if __name__ == "__main__":
    unittest.main()
