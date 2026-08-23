#!/usr/bin/env python3

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from decimal import Decimal
from importlib.metadata import version
from pathlib import Path
from typing import Any, Iterable

import jsonschema_rs
from openapi_spec_validator import OpenAPIV31SpecValidator
from openapi_spec_validator.readers import read_from_filename


class ContractStandardsError(RuntimeError):
    pass


def reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ContractStandardsError(f"duplicate JSON object key: {key}")
        result[key] = value
    return result


def reject_non_finite(value: str) -> None:
    raise ContractStandardsError(f"non-finite JSON number: {value}")


def load_json(path: Path) -> Any:
    if path.is_symlink() or not path.is_file():
        raise ContractStandardsError(f"expected regular JSON file: {path}")
    try:
        return json.loads(
            path.read_text(encoding="utf-8"),
            object_pairs_hook=reject_duplicate_keys,
            parse_float=Decimal,
            parse_constant=reject_non_finite,
        )
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ContractStandardsError(f"cannot read strict JSON {path}: {error}") from error


def required_object(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ContractStandardsError(f"{label} must be an object")
    return value


def required_array(value: Any, label: str) -> list[Any]:
    if not isinstance(value, list):
        raise ContractStandardsError(f"{label} must be an array")
    return value


def required_string(value: Any, label: str) -> str:
    if not isinstance(value, str) or value == "":
        raise ContractStandardsError(f"{label} must be a non-empty string")
    return value


def required_integer(value: Any, label: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise ContractStandardsError(f"{label} must be a non-negative integer")
    return value


def exact_keys(value: dict[str, Any], expected: set[str], label: str) -> None:
    actual = set(value)
    if actual != expected:
        raise ContractStandardsError(
            f"{label} keys mismatch: expected={sorted(expected)} actual={sorted(actual)}"
        )


def regular_files(root: Path) -> list[Path]:
    if not root.is_dir() or root.is_symlink():
        raise ContractStandardsError(f"expected regular directory: {root}")
    result: list[Path] = []
    for path in root.rglob("*"):
        if path.is_symlink():
            raise ContractStandardsError(f"symlink is forbidden in standards corpus: {path}")
        if path.is_dir():
            continue
        if not path.is_file():
            raise ContractStandardsError(f"non-regular standards corpus entry: {path}")
        result.append(path)
    return sorted(result, key=lambda path: path.relative_to(root).as_posix())


def file_sha256(path: Path) -> str:
    if path.is_symlink() or not path.is_file():
        raise ContractStandardsError(f"expected regular file: {path}")
    return hashlib.sha256(path.read_bytes()).hexdigest()


def repository_path(boundary: Path, base: Path, raw_path: str, label: str) -> Path:
    relative = Path(raw_path)
    if relative.is_absolute():
        raise ContractStandardsError(f"{label} must be repository-relative: {raw_path}")
    resolved_boundary = boundary.resolve()
    resolved = (base / relative).resolve()
    if not resolved.is_relative_to(resolved_boundary):
        raise ContractStandardsError(f"{label} escapes its repository boundary: {raw_path}")
    return resolved


def corpus_manifest_sha256(root: Path) -> tuple[str, int]:
    digest = hashlib.sha256()
    files = regular_files(root)
    for path in files:
        relative = path.relative_to(root).as_posix()
        content = path.read_bytes()
        digest.update(relative.encode("utf-8"))
        digest.update(b"\0")
        digest.update(hashlib.sha256(content).hexdigest().encode("ascii"))
        digest.update(b"\0")
        digest.update(str(len(content)).encode("ascii"))
        digest.update(b"\0")
    return digest.hexdigest(), len(files)


def json_pointer(document: Any, pointer: str) -> Any:
    if pointer == "":
        return document
    if not pointer.startswith("/"):
        raise ContractStandardsError(f"invalid RFC 6901 pointer: {pointer}")
    current = document
    for raw_token in pointer[1:].split("/"):
        if re.search(r"~(?:[^01]|$)", raw_token):
            raise ContractStandardsError(f"invalid RFC 6901 escape in pointer: {pointer}")
        token = raw_token.replace("~1", "/").replace("~0", "~")
        if isinstance(current, dict):
            if token not in current:
                raise ContractStandardsError(f"JSON pointer does not exist: {pointer}")
            current = current[token]
        elif isinstance(current, list):
            if not token.isascii() or not token.isdigit() or (token.startswith("0") and token != "0"):
                raise ContractStandardsError(f"invalid JSON array pointer token: {token}")
            index = int(token)
            if index >= len(current):
                raise ContractStandardsError(f"JSON pointer does not exist: {pointer}")
            current = current[index]
        else:
            raise ContractStandardsError(f"JSON pointer traverses a scalar: {pointer}")
    return current


def validate_runtime(profile: dict[str, Any], root: Path) -> None:
    toolchain = required_object(profile.get("toolchain"), "profile toolchain")
    exact_keys(toolchain, {"bun", "python", "uv", "pyproject", "lock"}, "profile toolchain")
    required_string(toolchain.get("bun"), "profile Bun version")
    expected_python = required_string(toolchain.get("python"), "profile Python version")
    actual_python = ".".join(str(item) for item in sys.version_info[:3])
    if actual_python != expected_python:
        raise ContractStandardsError(
            f"Python runtime mismatch: expected={expected_python} actual={actual_python}"
        )
    for key in ("pyproject", "lock"):
        fact = required_object(toolchain.get(key), f"profile {key}")
        exact_keys(fact, {"path", "sha256"}, f"profile {key}")
        path = repository_path(
            root, root, required_string(fact.get("path"), f"profile {key} path"), f"profile {key}"
        )
        expected = required_string(fact.get("sha256"), f"profile {key} SHA-256")
        actual = file_sha256(path)
        if actual != expected:
            raise ContractStandardsError(
                f"{key} SHA-256 mismatch: expected={expected} actual={actual}"
            )

    packages = required_object(profile.get("packages"), "profile packages")
    expected_packages = {
        required_string(name, "profile package name"): required_string(
            package_version, f"profile package {name} version"
        )
        for name, package_version in packages.items()
    }
    actual_packages = {name: version(name) for name in expected_packages}
    if actual_packages != expected_packages:
        raise ContractStandardsError(
            f"Python package mismatch: expected={expected_packages} actual={actual_packages}"
        )


def validate_corpus(profile: dict[str, Any], root: Path) -> Path:
    suite = required_object(profile.get("jsonSchemaOfficialSuite"), "official suite profile")
    local_root = repository_path(
        root,
        root,
        required_string(suite.get("localRoot"), "official suite local root"),
        "official suite local root",
    )
    expected_manifest = required_string(
        suite.get("corpusManifestSha256"), "official suite corpus manifest SHA-256"
    )
    actual_manifest, file_count = corpus_manifest_sha256(local_root)
    expected_files = required_integer(suite.get("corpusFiles"), "official suite corpus files")
    if actual_manifest != expected_manifest or file_count != expected_files:
        raise ContractStandardsError(
            "official suite corpus mismatch: "
            f"expected_sha={expected_manifest} actual_sha={actual_manifest} "
            f"expected_files={expected_files} actual_files={file_count}"
        )
    license_path = local_root / "LICENSE"
    expected_license = required_string(suite.get("licenseSha256"), "official suite license SHA-256")
    actual_license = file_sha256(license_path)
    if actual_license != expected_license:
        raise ContractStandardsError(
            f"official suite license mismatch: expected={expected_license} actual={actual_license}"
        )
    return local_root


def build_registry(resources: Iterable[tuple[str, Any]]) -> jsonschema_rs.Registry:
    return jsonschema_rs.Registry(list(resources), draft=jsonschema_rs.Draft202012)


def run_official_suite(profile: dict[str, Any], corpus_root: Path) -> dict[str, int]:
    suite = required_object(profile.get("jsonSchemaOfficialSuite"), "official suite profile")
    test_root = corpus_root / "tests" / "draft2020-12"
    remote_root = corpus_root / "remotes"
    test_files = sorted(test_root.glob("*.json"))
    remote_files = sorted(remote_root.rglob("*.json"))
    registry = build_registry(
        (
            f"http://localhost:1234/{path.relative_to(remote_root).as_posix()}",
            load_json(path),
        )
        for path in remote_files
    )
    failures: list[str] = []
    cases = 0
    assertions = 0
    for path in test_files:
        for raw_case in required_array(load_json(path), f"official suite {path}"):
            case = required_object(raw_case, f"official suite case in {path}")
            cases += 1
            tests = required_array(case.get("tests"), f"official suite tests in {path}")
            try:
                validator = jsonschema_rs.validator_for(
                    case.get("schema"), registry=registry, validate_formats=False
                )
            except Exception as error:
                failures.append(f"{path.name} :: {case.get('description')} :: compile :: {error}")
                assertions += len(tests)
                continue
            for raw_test in tests:
                test = required_object(raw_test, f"official suite test in {path}")
                assertions += 1
                expected = test.get("valid")
                if not isinstance(expected, bool):
                    raise ContractStandardsError(f"official suite validity must be boolean: {path}")
                try:
                    actual = validator.is_valid(test.get("data"))
                except Exception as error:
                    failures.append(
                        f"{path.name} :: {case.get('description')} :: "
                        f"{test.get('description')} :: {error}"
                    )
                    continue
                if actual != expected:
                    failures.append(
                        f"{path.name} :: {case.get('description')} :: "
                        f"{test.get('description')} :: expected={expected} actual={actual}"
                    )
    expected = {
        "files": required_integer(suite.get("mandatoryFiles"), "official suite file count"),
        "cases": required_integer(suite.get("cases"), "official suite case count"),
        "assertions": required_integer(
            suite.get("assertions"), "official suite assertion count"
        ),
        "remotes": required_integer(suite.get("remoteFiles"), "official suite remote count"),
    }
    actual = {
        "files": len(test_files),
        "cases": cases,
        "assertions": assertions,
        "remotes": len(remote_files),
    }
    if actual != expected:
        raise ContractStandardsError(
            f"official suite cardinality mismatch: expected={expected} actual={actual}"
        )
    if failures:
        raise ContractStandardsError(
            f"official JSON Schema suite failed ({len(failures)}):\n" + "\n".join(failures[:20])
        )
    return actual


def current_schema_registry(root: Path) -> tuple[jsonschema_rs.Registry, dict[Path, Any]]:
    schema_paths = sorted((root / "contracts").rglob("*.schema.json"))
    schemas = {path.resolve(): load_json(path) for path in schema_paths}
    resources: list[tuple[str, Any]] = []
    identifiers: set[str] = set()
    for path, schema_value in schemas.items():
        schema = required_object(schema_value, f"schema {path}")
        identifier = required_string(schema.get("$id"), f"schema {path} $id")
        if identifier in identifiers:
            raise ContractStandardsError(f"duplicate JSON Schema $id: {identifier}")
        identifiers.add(identifier)
        resources.append((identifier, schema))
    return build_registry(resources), schemas


def run_current_schema_fixtures(profile: dict[str, Any], root: Path) -> dict[str, int]:
    current = required_object(profile.get("currentContracts"), "current contract profile")
    contract_root = (root / "contracts").resolve()
    registry, schemas = current_schema_registry(root)
    validators: dict[Path, Any] = {}
    for path, schema in schemas.items():
        try:
            validators[path] = jsonschema_rs.validator_for(
                schema, registry=registry, validate_formats=False
            )
        except Exception as error:
            raise ContractStandardsError(f"current schema compile failed: {path}: {error}") from error

    manifests = sorted((root / "contracts").rglob("fixtures/manifest.json"))
    cases = 0
    failures: list[str] = []
    for manifest_path in manifests:
        manifest = required_object(load_json(manifest_path), f"fixture manifest {manifest_path}")
        for raw_case in required_array(manifest.get("cases"), f"fixture cases {manifest_path}"):
            case = required_object(raw_case, f"fixture case {manifest_path}")
            cases += 1
            schema_path = repository_path(
                contract_root,
                manifest_path.parent,
                required_string(case.get("schema"), "fixture schema"),
                "fixture schema",
            )
            validator = validators.get(schema_path)
            if validator is None:
                raise ContractStandardsError(f"fixture refers to an unknown schema: {schema_path}")
            has_instance = "instance" in case
            has_document = "document" in case
            if has_instance == has_document:
                raise ContractStandardsError(
                    f"fixture must contain exactly one of instance/document: {manifest_path}"
                )
            source_key = "instance" if has_instance else "document"
            source_path = repository_path(
                contract_root,
                manifest_path.parent,
                required_string(case.get(source_key), f"fixture {source_key}"),
                f"fixture {source_key}",
            )
            instance = load_json(source_path)
            if has_document:
                pointer = required_string(case.get("instancePointer"), "fixture instancePointer")
                instance = json_pointer(instance, pointer)
            expected_valid = case.get("expectedSchemaValid")
            if not isinstance(expected_valid, bool):
                raise ContractStandardsError("fixture expectedSchemaValid must be boolean")
            try:
                actual_valid = validator.is_valid(instance)
            except Exception as error:
                failures.append(f"{case.get('name')} :: engine error :: {error}")
                continue
            if actual_valid != expected_valid:
                failures.append(
                    f"{case.get('name')} :: expected={expected_valid} actual={actual_valid}"
                )
    actual = {"schemas": len(schemas), "manifests": len(manifests), "cases": cases}
    expected = {
        "schemas": required_integer(current.get("schemaFiles"), "current schema count"),
        "manifests": required_integer(current.get("fixtureManifests"), "current manifest count"),
        "cases": required_integer(current.get("fixtureCases"), "current fixture count"),
    }
    if actual != expected:
        raise ContractStandardsError(
            f"current contract cardinality mismatch: expected={expected} actual={actual}"
        )
    if failures:
        raise ContractStandardsError(
            f"independent current JSON Schema validation failed ({len(failures)}):\n"
            + "\n".join(failures[:20])
        )
    return actual


def run_openapi_validation(profile: dict[str, Any], root: Path) -> dict[str, int]:
    openapi = required_object(profile.get("openapi"), "OpenAPI profile")
    documents = required_array(openapi.get("documents"), "OpenAPI documents")
    expected_version = required_string(openapi.get("documentVersion"), "OpenAPI document version")
    failures: list[str] = []
    operations = 0
    for raw_path in documents:
        path = repository_path(
            root,
            root,
            required_string(raw_path, "OpenAPI document path"),
            "OpenAPI document path",
        )
        if path.is_symlink() or not path.is_file():
            raise ContractStandardsError(f"expected regular OpenAPI document: {path}")
        spec, base_uri = read_from_filename(str(path))
        if spec.get("openapi") != expected_version:
            failures.append(
                f"{path}: expected version {expected_version}, found {spec.get('openapi')}"
            )
            continue
        operations += sum(
            1
            for path_item in required_object(spec.get("paths"), f"OpenAPI paths {path}").values()
            if isinstance(path_item, dict)
            for method in path_item
            if method in {"get", "put", "post", "delete", "options", "head", "patch", "trace"}
        )
        errors = list(OpenAPIV31SpecValidator(spec, base_uri=base_uri).iter_errors())
        failures.extend(f"{path}: {error}" for error in errors)
    actual = {"documents": len(documents), "operations": operations}
    expected = {
        "documents": required_integer(openapi.get("documentCount"), "OpenAPI document count"),
        "operations": required_integer(openapi.get("operationCount"), "OpenAPI operation count"),
    }
    if actual != expected:
        raise ContractStandardsError(
            f"OpenAPI cardinality mismatch: expected={expected} actual={actual}"
        )
    if failures:
        raise ContractStandardsError(
            f"independent OpenAPI validation failed ({len(failures)}):\n"
            + "\n".join(failures[:20])
        )
    return actual


def validate_profile(profile: dict[str, Any]) -> None:
    exact_keys(
        profile,
        {
            "formatVersion",
            "status",
            "notGateClosure",
            "toolchain",
            "packages",
            "jsonSchemaOfficialSuite",
            "currentContracts",
            "openapi",
            "implementationBoundary",
        },
        "contract standards profile",
    )
    if profile.get("formatVersion") != "cloud-agents-contract-standards-profile/v1":
        raise ContractStandardsError("unsupported contract standards profile")
    if profile.get("status") != "GENERATED_NON_GATE_EVIDENCE" or profile.get("notGateClosure") is not True:
        raise ContractStandardsError("contract standards profile must remain non-Gate evidence")
    current = required_object(profile.get("currentContracts"), "current contract profile")
    if current.get("crossEngineExactFixtureResults") is not True:
        raise ContractStandardsError("cross-engine fixture comparison must remain enabled")
    suite = required_object(profile.get("jsonSchemaOfficialSuite"), "official suite profile")
    production_ajv_audit = required_object(
        suite.get("productionAjvOfficialSuiteAudit"), "production Ajv official-suite audit"
    )
    exact_keys(
        production_ajv_audit,
        {"validator", "status"},
        "production Ajv official-suite audit",
    )
    if production_ajv_audit != {
        "validator": "Ajv 8.20.0",
        "status": "NOT_RUN_NOT_CLAIMED",
    }:
        raise ContractStandardsError(
            "production Ajv official-suite audit must remain NOT_RUN_NOT_CLAIMED"
        )
    boundary = required_object(profile.get("implementationBoundary"), "implementation boundary")
    expected_boundary = {
        "productionRuntimeDependency": "FORBIDDEN",
        "productionDatabaseWrites": "NOT_AUTHORIZED",
        "httpSurface": "NOT_IMPLEMENTED",
        "p2Surface": "NOT_IMPLEMENTED",
        "providerSideEffects": "FORBIDDEN",
        "deployment": "NOT_AUTHORIZED",
        "publication": "NOT_AUTHORIZED",
        "gateStatus": "ALL_GATES_OPEN",
        "independentReview": "PENDING",
    }
    if boundary != expected_boundary:
        raise ContractStandardsError(
            f"implementation boundary mismatch: expected={expected_boundary} actual={boundary}"
        )


def run(root: Path, profile_path: Path) -> dict[str, Any]:
    profile = required_object(load_json(profile_path), "contract standards profile")
    validate_profile(profile)
    validate_runtime(profile, root)
    corpus_root = validate_corpus(profile, root)
    official = run_official_suite(profile, corpus_root)
    current = run_current_schema_fixtures(profile, root)
    openapi = run_openapi_validation(profile, root)
    return {
        "status": "INDEPENDENT_CONTRACT_STANDARDS_VALIDATED",
        "notGateClosure": True,
        "jsonSchemaOfficialSuite": official,
        "currentJsonSchema": current,
        "openapi31": openapi,
        "validators": {
            "jsonschema-rs": version("jsonschema-rs"),
            "openapi-spec-validator": version("openapi-spec-validator"),
        },
        "independentReview": "PENDING",
        "gateStatus": "ALL_GATES_OPEN",
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument(
        "--profile",
        type=Path,
        default=Path("tools/contract-standards/profile.json"),
    )
    arguments = parser.parse_args()
    root = arguments.root.resolve()
    profile_path = arguments.profile
    if not profile_path.is_absolute():
        profile_path = root / profile_path
    try:
        result = run(root, profile_path)
    except (ContractStandardsError, jsonschema_rs.ValidationError) as error:
        print(f"contract-standards: {error}", file=sys.stderr)
        return 1
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
