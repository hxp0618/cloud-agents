#!/usr/bin/env python3
"""Reproduce the authority-bound librsvg Cargo vendor archive from local inputs."""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import tempfile
import tomllib
from typing import Any


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


_PREPARE = _load_module("prepare_sharp_libvips_successor_for_librsvg_vendor", PREPARE_SCRIPT)
_VENDOR = _load_module("native_cargo_vendor_for_librsvg", VENDOR_HELPER)


def _load_authority(repo_root: Path) -> dict[str, Any]:
    source_path = repo_root / SOURCE_RELATIVE_PATH
    _PREPARE._verified(source_path, "source.json")
    try:
        document = json.loads(source_path.read_text(encoding="utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError(f"source.json is invalid: {exc}") from exc
    if not isinstance(document, dict):
        raise ValueError("source.json must be an object")
    librsvg = document.get("librsvg")
    if not isinstance(librsvg, dict):
        raise ValueError("librsvg must be an object")
    cargo_lock = document.get("cargoLock")
    lock_authority = _PREPARE._validate_repo_artifact(cargo_lock, "Cargo.lock")

    vendor = _PREPARE._librsvg_vendor(librsvg)
    return {
        "librsvg": librsvg,
        "cargoLock": cargo_lock,
        "lock": lock_authority,
        "registry": librsvg["registry"],
        "vendor": vendor,
    }


def _read_toml(path: Path, label: str) -> dict[str, Any]:
    _PREPARE._verified(path, label)
    try:
        value = tomllib.loads(path.read_text(encoding="utf-8"))
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as exc:
        raise ValueError(f"{label} is invalid: {exc}") from exc
    if not isinstance(value, dict):
        raise ValueError(f"{label} must be a TOML table")
    return value


def _package_identity(package: Any, workspace_version: Any, label: str) -> tuple[str, str]:
    if not isinstance(package, dict):
        raise ValueError(f"{label} must contain a package table")
    name = package.get("name")
    version_value = package.get("version", "0.0.0")
    if isinstance(version_value, dict):
        if version_value != {"workspace": True}:
            raise ValueError(f"{label} package version inheritance is invalid")
        version_value = workspace_version
    if (
        not isinstance(name, str)
        or not PACKAGE_COMPONENT.fullmatch(name)
        or not isinstance(version_value, str)
        or not PACKAGE_COMPONENT.fullmatch(version_value)
    ):
        raise ValueError(f"{label} package identity is unsafe")
    return name, version_value


def _validate_manifest_paths(
    value: Any, manifest_directory: Path, source_root: Path, label: str
) -> None:
    if isinstance(value, list):
        for item in value:
            _validate_manifest_paths(item, manifest_directory, source_root, label)
        return
    if not isinstance(value, dict):
        return
    for key, item in value.items():
        if key == "path":
            if (
                not isinstance(item, str)
                or not item
                or "\\" in item
                or "\x00" in item
                or PurePosixPath(item).is_absolute()
            ):
                raise ValueError(f"{label} contains an unsafe path")
            target = manifest_directory.joinpath(*PurePosixPath(item).parts)
            if not target.exists():
                raise ValueError(f"{label} path is missing: {item}")
            try:
                target.resolve().relative_to(source_root.resolve())
            except ValueError as exc:
                raise ValueError(f"{label} path escapes source root: {item}") from exc
        _validate_manifest_paths(item, manifest_directory, source_root, label)


def _workspace_identities(source_root: Path) -> set[tuple[str, str]]:
    manifest = _read_toml(source_root / "Cargo.toml", "librsvg workspace Cargo.toml")
    _validate_manifest_paths(manifest, source_root, source_root, "librsvg workspace Cargo.toml")
    workspace = manifest.get("workspace")
    if not isinstance(workspace, dict):
        raise ValueError("librsvg Cargo.toml must contain a workspace table")
    members = workspace.get("members")
    if not isinstance(members, list) or not members:
        raise ValueError("librsvg workspace members must be a non-empty list")
    workspace_package = manifest.get("workspace", {}).get("package")
    # TOML [workspace.package] is represented inside the workspace table.
    if not isinstance(workspace_package, dict):
        workspace_package = {}
    workspace_version = workspace_package.get("version")

    identities: set[tuple[str, str]] = set()
    member_paths: set[str] = set()
    source_resolved = source_root.resolve()
    for index, member_value in enumerate(members):
        member = _PREPARE._safe_path(member_value, f"librsvg workspace member {index}")
        if any(character in member for character in "*?["):
            raise ValueError(f"librsvg workspace member {index} must not be a glob")
        if member in member_paths:
            raise ValueError(f"duplicate librsvg workspace member: {member}")
        member_paths.add(member)
        member_dir = source_root / PurePosixPath(member)
        _PREPARE._verified_directory(member_dir, f"librsvg workspace member {member}")
        try:
            member_dir.resolve().relative_to(source_resolved)
        except ValueError as exc:
            raise ValueError(f"librsvg workspace member escapes source root: {member}") from exc
        member_manifest = _read_toml(
            member_dir / "Cargo.toml", f"librsvg workspace member {member} Cargo.toml"
        )
        _validate_manifest_paths(
            member_manifest,
            member_dir,
            source_root,
            f"librsvg workspace member {member} Cargo.toml",
        )
        identity = _package_identity(
            member_manifest.get("package"), workspace_version, f"librsvg workspace member {member}"
        )
        if identity in identities:
            raise ValueError(f"duplicate librsvg workspace package: {identity[0]} {identity[1]}")
        identities.add(identity)
    return identities


def _validate_lock(
    source_root: Path, lock_authority: dict[str, str]
) -> tuple[bytes, list[dict[str, str]]]:
    lock_path = _PREPARE._verified(
        source_root / "Cargo.lock", "prepared librsvg Cargo.lock", lock_authority["sha256"]
    )
    lock_bytes = lock_path.read_bytes()
    try:
        lock = tomllib.loads(lock_bytes.decode("utf-8"))
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as exc:
        raise ValueError(f"librsvg Cargo.lock is invalid: {exc}") from exc
    package_records = lock.get("package")
    if not isinstance(package_records, list) or not package_records:
        raise ValueError("librsvg Cargo.lock must contain packages")

    local_identities: set[tuple[str, str]] = set()
    all_identities: set[tuple[str, str]] = set()
    registry_packages: list[dict[str, str]] = []
    for index, package in enumerate(package_records):
        if not isinstance(package, dict):
            raise ValueError(f"librsvg Cargo.lock package {index} must be an object")
        name, version = package.get("name"), package.get("version")
        if (
            not isinstance(name, str)
            or not PACKAGE_COMPONENT.fullmatch(name)
            or not isinstance(version, str)
            or not PACKAGE_COMPONENT.fullmatch(version)
        ):
            raise ValueError(f"librsvg Cargo.lock package {index} has an unsafe identity")
        identity = (name, version)
        if identity in all_identities:
            raise ValueError(f"duplicate librsvg Cargo.lock package: {name} {version}")
        all_identities.add(identity)
        source = package.get("source")
        if source is None:
            if "checksum" in package:
                raise ValueError(f"local librsvg Cargo.lock package has a checksum: {name} {version}")
            local_identities.add(identity)
            continue
        if source != CRATES_IO_SOURCE:
            raise ValueError(f"librsvg Cargo.lock package {name} {version} is not crates.io")
        checksum = _PREPARE._canonical_sha256(
            package.get("checksum"), f"librsvg Cargo.lock package {name} {version}"
        )
        registry_packages.append({"name": name, "version": version, "checksum": checksum})

    workspace_identities = _workspace_identities(source_root)
    if local_identities != workspace_identities:
        raise ValueError("librsvg Cargo.lock local packages do not match the prepared workspace")
    if not registry_packages:
        raise ValueError("librsvg Cargo.lock has no registry packages")
    return lock_bytes, registry_packages


def _reject_source_config(source_root: Path) -> None:
    cargo_directory = source_root / ".cargo"
    if not os.path.lexists(cargo_directory):
        return
    _PREPARE._verified_directory(cargo_directory, "prepared librsvg .cargo directory")
    for name in ("config", "config.toml"):
        if os.path.lexists(cargo_directory / name):
            raise ValueError("prepared librsvg source must not contain Cargo config")


def vendor(
    *,
    librsvg_archive: Path,
    librsvg_security_patch: Path,
    cargo_registry_dir: Path,
    output_dir: Path,
    repo_root: Path = DEFAULT_REPO_ROOT,
    cargo_bin: Path | None = None,
) -> Path:
    """Generate the authority-bound deterministic librsvg vendor archive."""

    output = Path(output_dir).absolute()
    if os.path.lexists(output):
        raise FileExistsError(f"output already exists: {output}")
    repo = Path(repo_root).absolute()
    authority = _load_authority(repo)

    output.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f".{output.name}.", dir=output.parent))
    output_created = False
    try:
        with tempfile.TemporaryDirectory(prefix="cloud-agents-librsvg-vendor-") as temporary:
            temporary_root = Path(temporary)
            source_root = _PREPARE.prepare_librsvg_source(
                librsvg=authority["librsvg"],
                cargo_lock=authority["cargoLock"],
                repo_root=repo,
                librsvg_archive=Path(librsvg_archive).absolute(),
                librsvg_security_patch=Path(librsvg_security_patch).absolute(),
                output_dir=temporary_root / "source",
            )
            _reject_source_config(source_root)
            lock_bytes, packages = _validate_lock(source_root, authority["lock"])
            vendor_tree = temporary_root / authority["vendor"]["root"]
            _VENDOR.run_cargo_vendor(
                source_root=source_root,
                lock_bytes=lock_bytes,
                registry_dir=cargo_registry_dir,
                registry_url=authority["registry"],
                packages=packages,
                vendor_tree=vendor_tree,
                cargo_bin=cargo_bin,
                archive_api=_PREPARE,
                temporary_prefix="cloud-agents-librsvg-home-",
            )
            candidate = staging / authority["vendor"]["file"]
            _VENDOR.write_vendor_archive(vendor_tree, candidate)
            _PREPARE._verified(
                candidate,
                "generated librsvg vendor archive",
                authority["vendor"]["sha256"],
            )

        if os.path.lexists(output):
            raise FileExistsError(f"output already exists: {output}")
        output.mkdir()
        output_created = True
        os.link(staging / authority["vendor"]["file"], output / authority["vendor"]["file"])
        (staging / authority["vendor"]["file"]).unlink()
        staging.rmdir()
        return output
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        if output_created:
            shutil.rmtree(output, ignore_errors=True)
        raise


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--librsvg-archive", required=True, type=Path)
    parser.add_argument("--librsvg-security-patch", required=True, type=Path)
    parser.add_argument("--cargo-registry-dir", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--repo-root", type=Path, default=DEFAULT_REPO_ROOT)
    parser.add_argument("--cargo-bin", type=Path)
    arguments = parser.parse_args()
    vendor(
        librsvg_archive=arguments.librsvg_archive,
        librsvg_security_patch=arguments.librsvg_security_patch,
        cargo_registry_dir=arguments.cargo_registry_dir,
        output_dir=arguments.output_dir,
        repo_root=arguments.repo_root,
        cargo_bin=arguments.cargo_bin,
    )


if __name__ == "__main__":
    main()
