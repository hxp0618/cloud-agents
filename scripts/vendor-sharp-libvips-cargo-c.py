#!/usr/bin/env python3
"""Reproduce the authority-bound cargo-c vendor archive from local Cargo inputs."""

from __future__ import annotations

import argparse
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import tarfile
import tempfile
import tomllib
from typing import Any
from urllib.parse import urlsplit


SCRIPT_DIRECTORY = Path(__file__).resolve().parent
DEFAULT_REPO_ROOT = SCRIPT_DIRECTORY.parent
SOURCE_RELATIVE_PATH = Path("tools/sharp-libvips-successor/v1/source.json")
PREPARE_SCRIPT = SCRIPT_DIRECTORY / "prepare-sharp-libvips-successor.py"
VENDOR_HELPER = SCRIPT_DIRECTORY / "lib/native-cargo-vendor.py"
CRATES_IO_SOURCE = "registry+https://github.com/rust-lang/crates.io-index"
PACKAGE_COMPONENT = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+-]*$")


def _load_module(name: str, path: Path) -> Any:
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load helper: {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


_PREPARE = _load_module("prepare_sharp_libvips_successor_for_vendor", PREPARE_SCRIPT)
_VENDOR = _load_module("native_cargo_vendor_for_cargo_c", VENDOR_HELPER)


def _load_authority(repo_root: Path) -> dict[str, Any]:
    source_path = repo_root / SOURCE_RELATIVE_PATH
    _PREPARE._verified(source_path, "source.json")
    try:
        document = json.loads(source_path.read_text(encoding="utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError(f"source.json is invalid: {exc}") from exc
    if not isinstance(document, dict):
        raise ValueError("source.json must be an object")
    cargo_c = _PREPARE._validate_cargo_c(document.get("cargoC"))
    raw_cargo_c = document.get("cargoC")
    registry = raw_cargo_c.get("registry") if isinstance(raw_cargo_c, dict) else None
    if not isinstance(registry, str) or not registry.startswith("sparse+"):
        raise ValueError("cargoC.registry must be a sparse credential-free HTTPS URL")
    https_url = registry.removeprefix("sparse+")
    _PREPARE._build_input_url(https_url, "cargoC.registry")
    parsed = urlsplit(https_url)
    if parsed.query or parsed.fragment or not parsed.path.endswith("/"):
        raise ValueError("cargoC.registry must be a canonical sparse HTTPS URL")
    cargo_c["registry"] = registry
    return cargo_c


def _validate_source_archive(
    path: Path, cargo_c: dict[str, Any]
) -> tuple[bytes, list[dict[str, str]]]:
    source = cargo_c["source"]
    verified = _PREPARE._verified(path, "cargo-c source archive", source["sha256"])
    root = source["root"]
    required = {f"{root}/Cargo.toml", f"{root}/Cargo.lock"}
    _PREPARE._archive_root(
        verified, root, "cargo-c source archive", required, allow_implicit_root=True
    )
    inventory = _VENDOR.archive_inventory(verified, "cargo-c source archive", _PREPARE)
    config_prefix = f"{root}/.cargo/"
    for member in inventory:
        if member.startswith(config_prefix) and PurePosixPath(member).name.startswith("config"):
            raise ValueError("cargo-c source archive must not contain .cargo/config files")

    manifest = _VENDOR.member_toml(
        verified, f"{root}/Cargo.toml", "cargo-c Cargo.toml", _PREPARE
    )
    package = manifest.get("package")
    if not isinstance(package, dict) or package.get("name") != "cargo-c":
        raise ValueError("cargo-c Cargo.toml package name must be cargo-c")
    if package.get("version") != cargo_c["version"]:
        raise ValueError("cargo-c Cargo.toml package version does not match authority")

    lock_bytes = _PREPARE._archive_member_bytes(
        verified, f"{root}/Cargo.lock", "cargo-c Cargo.lock"
    )
    try:
        lock = tomllib.loads(lock_bytes.decode("utf-8"))
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as exc:
        raise ValueError(f"cargo-c Cargo.lock is invalid: {exc}") from exc
    packages = lock.get("package")
    if not isinstance(packages, list) or not packages:
        raise ValueError("cargo-c Cargo.lock must contain packages")
    root_packages = [
        package_record
        for package_record in packages
        if isinstance(package_record, dict) and package_record.get("source") is None
    ]
    if len(root_packages) != 1:
        raise ValueError("cargo-c Cargo.lock must contain exactly one local root package")
    root_package = root_packages[0]
    if root_package.get("name") != "cargo-c" or root_package.get("version") != cargo_c["version"]:
        raise ValueError("cargo-c Cargo.lock root package does not match authority")

    registry_packages: list[dict[str, str]] = []
    identities: set[tuple[str, str]] = set()
    for index, package_record in enumerate(packages):
        if package_record is root_package:
            continue
        if not isinstance(package_record, dict):
            raise ValueError(f"cargo-c Cargo.lock package {index} must be an object")
        name, version = package_record.get("name"), package_record.get("version")
        if (
            not isinstance(name, str)
            or not PACKAGE_COMPONENT.fullmatch(name)
            or not isinstance(version, str)
            or not PACKAGE_COMPONENT.fullmatch(version)
        ):
            raise ValueError(f"cargo-c Cargo.lock package {index} has an unsafe identity")
        if package_record.get("source") != CRATES_IO_SOURCE:
            raise ValueError(f"cargo-c Cargo.lock package {name} {version} is not crates.io")
        checksum = _PREPARE._canonical_sha256(
            package_record.get("checksum"), f"cargo-c Cargo.lock package {name} {version}"
        )
        identity = (name, version)
        if identity in identities:
            raise ValueError(f"duplicate cargo-c Cargo.lock package: {name} {version}")
        identities.add(identity)
        registry_packages.append({"name": name, "version": version, "checksum": checksum})
    if not registry_packages:
        raise ValueError("cargo-c Cargo.lock has no registry packages")
    return lock_bytes, registry_packages


def vendor(
    *,
    cargo_c_archive: Path,
    cargo_registry_dir: Path,
    output_dir: Path,
    repo_root: Path = DEFAULT_REPO_ROOT,
    cargo_bin: Path | None = None,
) -> Path:
    """Generate the authority-bound source and deterministic vendor archive directory."""

    output = Path(output_dir).absolute()
    if os.path.lexists(output):
        raise FileExistsError(f"output already exists: {output}")
    cargo_c = _load_authority(Path(repo_root).absolute())
    source_archive = Path(cargo_c_archive).absolute()
    lock_bytes, packages = _validate_source_archive(source_archive, cargo_c)

    output.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f".{output.name}.", dir=output.parent))
    output_created = False
    try:
        with tempfile.TemporaryDirectory(prefix="cloud-agents-cargo-c-vendor-") as temporary:
            temporary_root = Path(temporary)
            source_parent = temporary_root / "work"
            source_parent.mkdir()
            with tarfile.open(source_archive, "r:*") as archive:
                archive.extractall(source_parent, filter="data")
            source_root = source_parent / cargo_c["source"]["root"]
            if not source_root.is_dir() or source_root.is_symlink():
                raise ValueError("extracted cargo-c source root is invalid")
            vendor_tree = temporary_root / cargo_c["vendor"]["root"]
            _VENDOR.run_cargo_vendor(
                source_root=source_root,
                lock_bytes=lock_bytes,
                registry_dir=cargo_registry_dir,
                registry_url=cargo_c["registry"],
                packages=packages,
                vendor_tree=vendor_tree,
                cargo_bin=cargo_bin,
                archive_api=_PREPARE,
                temporary_prefix="cloud-agents-cargo-c-home-",
            )

            candidate = staging / "candidate"
            candidate.mkdir()
            copied_source = candidate / cargo_c["source"]["file"]
            shutil.copy2(source_archive, copied_source)
            _PREPARE._verified(
                copied_source, "copied cargo-c source archive", cargo_c["source"]["sha256"]
            )
            vendor_archive = candidate / cargo_c["vendor"]["file"]
            _VENDOR.write_vendor_archive(vendor_tree, vendor_archive)
            _PREPARE._verified(
                vendor_archive, "generated cargo-c vendor archive", cargo_c["vendor"]["sha256"]
            )

        if os.path.lexists(output):
            raise FileExistsError(f"output already exists: {output}")
        output.mkdir()
        output_created = True
        for candidate_file in sorted((staging / "candidate").iterdir()):
            os.link(candidate_file, output / candidate_file.name)
        shutil.rmtree(staging / "candidate")
        staging.rmdir()
        return output
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        if output_created:
            shutil.rmtree(output, ignore_errors=True)
        raise


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cargo-c-archive", required=True, type=Path)
    parser.add_argument("--cargo-registry-dir", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--repo-root", type=Path, default=DEFAULT_REPO_ROOT)
    parser.add_argument("--cargo-bin", type=Path)
    arguments = parser.parse_args()
    vendor(
        cargo_c_archive=arguments.cargo_c_archive,
        cargo_registry_dir=arguments.cargo_registry_dir,
        output_dir=arguments.output_dir,
        repo_root=arguments.repo_root,
        cargo_bin=arguments.cargo_bin,
    )


if __name__ == "__main__":
    main()
