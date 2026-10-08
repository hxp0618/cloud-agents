#!/usr/bin/env python3
"""Prepare a bounded sharp/libvips source context without building it."""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
import posixpath
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import tomllib
from datetime import date
from pathlib import Path, PurePosixPath
from typing import Any
from urllib.parse import urlsplit


REPO_ROOT = Path(__file__).resolve().parents[1]
SOURCE_RELATIVE_PATH = PurePosixPath("tools/sharp-libvips-successor/v1/source.json")
COMPLETION_NOTE = "Source prep only; does not prove full native closure."
ARCHIVE_INSPECTOR = Path(__file__).with_name("lib") / "inspect-generator-supply-archive.py"
_MISSING = object()
_BUILD_INPUT_FILE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+-]*$")
_OCI_REPOSITORY = re.compile(
    r"^[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]+)?"
    r"(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)+$"
)
_OCI_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
_BUILDER_PLATFORMS = {
    "linux-arm64v8": "linux/arm64",
    "linux-x64": "linux/amd64",
}
_PYTHON_VERSION = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
_NINJA_ARCH_FOR_PLATFORM = {
    "linux/arm64": "aarch64",
    "linux/amd64": "x86_64",
}
_RUST_TARGET_FOR_PLATFORM = {
    "linux/arm64": "aarch64-unknown-linux-gnu",
    "linux/amd64": "x86_64-unknown-linux-gnu",
}
_RUST_COMPONENTS = ("rustc", "cargo", "rust-std")
_RPM_ARCH_FOR_PLATFORM = {
    "linux-arm64v8": "aarch64",
    "linux-x64": "x86_64",
}
_GIT_COMMIT = re.compile(r"^[0-9a-f]{40}$")
_CARGO_C_VERSION = re.compile(
    r"^[0-9]+\.[0-9]+\.[0-9]+\+cargo-[0-9]+\.[0-9]+\.[0-9]+$"
)


def _load_inspector() -> Any:
    spec = importlib.util.spec_from_file_location("inspect_generator_supply_archive", ARCHIVE_INSPECTOR)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load archive inspector: {ARCHIVE_INSPECTOR}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


_INSPECTOR = _load_inspector()


def _verified(path: Path, label: str, expected: Any = _MISSING) -> Path:
    try:
        mode = path.lstat().st_mode
    except FileNotFoundError as exc:
        raise ValueError(f"{label} is missing: {path}") from exc
    if not stat.S_ISREG(mode):
        raise ValueError(f"{label} must be a regular file: {path}")
    if expected is _MISSING:
        return path
    expected = _canonical_sha256(expected, label)
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        while chunk := stream.read(1024 * 1024):
            digest.update(chunk)
    actual = digest.hexdigest()
    if actual != expected:
        raise ValueError(f"{label} digest mismatch: expected {expected}, got {actual}")
    return path


def _verified_directory(path: Path, label: str) -> Path:
    try:
        mode = path.lstat().st_mode
    except FileNotFoundError as exc:
        raise ValueError(f"{label} is missing: {path}") from exc
    if not stat.S_ISDIR(mode):
        raise ValueError(f"{label} must be a directory: {path}")
    return path


def _safe_path(value: Any, label: str, *, root: bool = False) -> str:
    if not isinstance(value, str) or not value or "\\" in value or "\x00" in value:
        raise ValueError(f"{label} must be a safe relative path")
    path = PurePosixPath(value)
    if path.is_absolute() or ".." in path.parts or posixpath.normpath(value) != value:
        raise ValueError(f"{label} must be a safe relative path")
    if root and (len(path.parts) != 1 or value in (".", "..")):
        raise ValueError(f"{label} must name one archive root directory")
    return value


def _canonical_sha256(value: Any, label: str) -> str:
    if not isinstance(value, str) or len(value) != 64 or any(
        character not in "0123456789abcdef" for character in value
    ):
        raise ValueError(f"{label} SHA-256 is not canonical")
    return value


def _build_input_url(value: Any, label: str) -> str:
    if not isinstance(value, str) or not value or any(
        ord(character) < 32 or ord(character) == 127 for character in value
    ):
        raise ValueError(f"{label}.url must be a safe HTTPS URL")
    parsed = urlsplit(value)
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.username is not None
        or parsed.password is not None
    ):
        raise ValueError(f"{label}.url must be a credential-free HTTPS URL")
    return value


def _validate_artifact(value: Any, label: str) -> dict[str, str]:
    if not isinstance(value, dict):
        raise ValueError(f"{label} must be an object")
    url = _build_input_url(value.get("url"), label)
    file = value.get("file")
    if not isinstance(file, str) or not _BUILD_INPUT_FILE.fullmatch(file):
        raise ValueError(f"{label}.file must be one safe basename")
    return {
        "url": url,
        "file": file,
        "sha256": _canonical_sha256(value.get("sha256"), label),
    }


def _validate_repo_artifact(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"{label} must be an object")
    validated = dict(value)
    validated["path"] = _safe_path(value.get("path"), f"{label}.path")
    validated["sha256"] = _canonical_sha256(value.get("sha256"), label)
    return validated


def _validate_build_inputs(value: Any) -> list[dict[str, str]]:
    if not isinstance(value, list) or not value:
        raise ValueError("buildInputs must be a non-empty array")
    records: list[dict[str, str]] = []
    urls: set[str] = set()
    files: set[str] = set()
    for index, value_record in enumerate(value):
        label = f"buildInputs[{index}]"
        record = _validate_artifact(value_record, label)
        if record["url"] in urls:
            raise ValueError(f"duplicate buildInputs URL: {record['url']}")
        if record["file"] in files:
            raise ValueError(f"duplicate buildInputs file: {record['file']}")
        urls.add(record["url"])
        files.add(record["file"])
        records.append(record)
    return records


def _python_version(value: Any, label: str) -> str:
    if not isinstance(value, str) or not _PYTHON_VERSION.fullmatch(value):
        raise ValueError(f"{label}.version must use numeric x.y.z form")
    return value


def _validate_python_tools(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != {"meson", "ninja"}:
        raise ValueError("pythonTools must contain exactly meson and ninja")
    meson_value = value["meson"]
    ninja_value = value["ninja"]
    if not isinstance(meson_value, dict) or not isinstance(ninja_value, dict):
        raise ValueError("pythonTools entries must be objects")

    meson_version = _python_version(meson_value.get("version"), "pythonTools.meson")
    meson_wheel = _validate_artifact(meson_value.get("wheel"), "pythonTools.meson.wheel")
    expected_meson_file = f"meson-{meson_version}-py3-none-any.whl"
    if meson_wheel["file"] != expected_meson_file:
        raise ValueError(f"pythonTools.meson.wheel.file must be {expected_meson_file}")

    ninja_version = _python_version(ninja_value.get("version"), "pythonTools.ninja")
    ninja_values = ninja_value.get("wheels")
    if not isinstance(ninja_values, dict) or set(ninja_values) != set(_BUILDER_PLATFORMS):
        raise ValueError("pythonTools.ninja.wheels must contain exactly the supported targets")
    ninja_wheels: dict[str, dict[str, str]] = {}
    for target, platform in _BUILDER_PLATFORMS.items():
        architecture = _NINJA_ARCH_FOR_PLATFORM[platform]
        wheel = _validate_artifact(
            ninja_values[target], f"pythonTools.ninja.wheels.{target}"
        )
        expected_file = (
            f"ninja-{ninja_version}-py3-none-manylinux2014_{architecture}."
            f"manylinux_2_17_{architecture}.whl"
        )
        if wheel["file"] != expected_file:
            raise ValueError(f"pythonTools.ninja.wheels.{target}.file must be {expected_file}")
        ninja_wheels[target] = wheel

    artifacts = [meson_wheel, *ninja_wheels.values()]
    if len({artifact["url"] for artifact in artifacts}) != len(artifacts):
        raise ValueError("pythonTools wheel URLs must be unique")
    if len({artifact["file"] for artifact in artifacts}) != len(artifacts):
        raise ValueError("pythonTools wheel files must be unique")
    return {
        "meson": {"version": meson_version, "wheel": meson_wheel},
        "ninja": {"version": ninja_version, "wheels": ninja_wheels},
    }


def _validate_rust_toolchain(value: Any) -> dict[str, dict[str, str]]:
    if not isinstance(value, dict) or set(value) != {"manifest"}:
        raise ValueError("rustToolchain must contain only manifest")
    manifest = _validate_artifact(value["manifest"], "rustToolchain.manifest")
    if manifest["file"] != "channel-rust-nightly.toml":
        raise ValueError(
            "rustToolchain.manifest.file must be channel-rust-nightly.toml"
        )
    return {"manifest": manifest}


def _validate_cargo_c(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("cargoC must be an object")
    version = value.get("version")
    if not isinstance(version, str) or not _CARGO_C_VERSION.fullmatch(version):
        raise ValueError("cargoC.version must be an exact cargo-c+cargo version")
    source_value = value.get("source")
    if not isinstance(source_value, dict):
        raise ValueError("cargoC.source must be an object")
    source_file = f"cargo-c-{version}.crate"
    if source_value.get("file") != source_file:
        raise ValueError(f"cargoC.source.file must be {source_file}")
    source = {
        "url": _build_input_url(source_value.get("url"), "cargoC.source"),
        "file": source_file,
        "sha256": _canonical_sha256(source_value.get("sha256"), "cargoC.source"),
        "root": source_file.removesuffix(".crate"),
    }

    vendor_value = value.get("vendor")
    if not isinstance(vendor_value, dict):
        raise ValueError("cargoC.vendor must be an object")
    package_version = version.split("+", 1)[0]
    vendor_file = f"cargo-c-{package_version}-vendor.tar.xz"
    if vendor_value.get("file") != vendor_file:
        raise ValueError(f"cargoC.vendor.file must be {vendor_file}")
    vendor = {
        "file": vendor_file,
        "sha256": _canonical_sha256(vendor_value.get("sha256"), "cargoC.vendor"),
        "root": vendor_file.removesuffix(".tar.xz"),
    }
    return {"version": version, "source": source, "vendor": vendor}


def _rust_commit(value: Any, label: str) -> str:
    if not isinstance(value, str) or not _GIT_COMMIT.fullmatch(value):
        raise ValueError(f"{label} must be a canonical Git commit hash")
    return value


def _parse_rust_manifest(
    path: Path, authority: dict[str, str]
) -> dict[str, Any]:
    try:
        manifest = tomllib.loads(path.read_text(encoding="utf-8"))
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as exc:
        raise ValueError(f"Rust manifest is invalid: {exc}") from exc
    if manifest.get("manifest-version") != "2":
        raise ValueError("Rust manifest version must be 2")
    manifest_date = manifest.get("date")
    if not isinstance(manifest_date, str):
        raise ValueError("Rust manifest date is required")
    try:
        if date.fromisoformat(manifest_date).isoformat() != manifest_date:
            raise ValueError
    except ValueError as exc:
        raise ValueError("Rust manifest date must use a valid YYYY-MM-DD date") from exc
    authority_path = urlsplit(authority["url"]).path
    if (
        PurePosixPath(authority_path).name != authority["file"]
        or f"/{manifest_date}/" not in authority_path
    ):
        raise ValueError("Rust manifest URL must contain its date and exact filename")

    packages = manifest.get("pkg")
    if not isinstance(packages, dict):
        raise ValueError("Rust manifest pkg table is required")
    platforms: dict[str, list[dict[str, str]]] = {
        target: [] for target in _BUILDER_PLATFORMS
    }
    commits: dict[str, str] = {}
    for component in _RUST_COMPONENTS:
        package = packages.get(component)
        if not isinstance(package, dict):
            raise ValueError(f"Rust manifest package {component} is required")
        version = package.get("version")
        if not isinstance(version, str) or "-nightly" not in version:
            raise ValueError(f"Rust manifest package {component} must be nightly")
        if component == "rustc":
            commits[component] = _rust_commit(
                package.get("git_commit_hash"),
                f"Rust manifest package {component} git_commit_hash",
            )
        targets = package.get("target")
        if not isinstance(targets, dict):
            raise ValueError(f"Rust manifest package {component} targets are required")
        for target, platform in _BUILDER_PLATFORMS.items():
            rust_target = _RUST_TARGET_FOR_PLATFORM[platform]
            target_record = targets.get(rust_target)
            if not isinstance(target_record, dict):
                raise ValueError(
                    f"Rust manifest package {component} target {rust_target} is required"
                )
            if target_record.get("available") is not True:
                raise ValueError(
                    f"Rust manifest package {component} target {rust_target} is unavailable"
                )
            label = f"Rust manifest package {component} target {rust_target}"
            url = _build_input_url(target_record.get("xz_url"), label)
            filename = f"{component}-nightly-{rust_target}.tar.xz"
            url_path = urlsplit(url).path
            if (
                PurePosixPath(url_path).name != filename
                or f"/{manifest_date}/" not in url_path
            ):
                raise ValueError(f"{label} xz URL must contain its date and exact filename")
            sha256 = _canonical_sha256(target_record.get("xz_hash"), label)
            platforms[target].append(
                {
                    "url": url,
                    "file": filename,
                    "sha256": sha256,
                    "root": filename.removesuffix(".tar.xz"),
                    "rustTarget": rust_target,
                    "component": component,
                }
            )
    return {
        "date": manifest_date,
        "commits": commits,
        "platforms": platforms,
    }


def _validate_builder_base(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("builderBase must be an object")
    repository = value.get("repository")
    if not isinstance(repository, str) or not _OCI_REPOSITORY.fullmatch(repository):
        raise ValueError("builderBase.repository must be an untagged registry/repository")
    index_digest = value.get("indexDigest")
    if not isinstance(index_digest, str) or not _OCI_DIGEST.fullmatch(index_digest):
        raise ValueError("builderBase.indexDigest must be a canonical SHA-256 digest")
    platforms = value.get("platforms")
    if not isinstance(platforms, dict) or set(platforms) != set(_BUILDER_PLATFORMS):
        raise ValueError("builderBase.platforms must contain exactly the supported targets")
    validated_platforms: dict[str, dict[str, str]] = {}
    for target, expected_platform in _BUILDER_PLATFORMS.items():
        record = platforms[target]
        if not isinstance(record, dict) or record.get("platform") != expected_platform:
            raise ValueError(f"builderBase.platforms.{target}.platform must be {expected_platform}")
        manifest_digest = record.get("manifestDigest")
        if not isinstance(manifest_digest, str) or not _OCI_DIGEST.fullmatch(manifest_digest):
            raise ValueError(
                f"builderBase.platforms.{target}.manifestDigest must be a canonical SHA-256 digest"
            )
        validated_platforms[target] = {
            "platform": expected_platform,
            "manifestDigest": manifest_digest,
        }
    return {
        "repository": repository,
        "indexDigest": index_digest,
        "platforms": validated_platforms,
    }


def _validate_builder_rpms(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != {"keys", "platforms"}:
        raise ValueError("builderRpms must contain exactly keys and platforms")
    keys_value = value["keys"]
    if not isinstance(keys_value, dict) or not keys_value:
        raise ValueError("builderRpms.keys must be a non-empty object")
    keys: dict[str, dict[str, str]] = {}
    key_urls: set[str] = set()
    key_files: set[str] = set()
    for key_id, key_value in keys_value.items():
        if not isinstance(key_id, str) or not _BUILD_INPUT_FILE.fullmatch(key_id):
            raise ValueError("builderRpms key names must be safe identifiers")
        if not isinstance(key_value, dict) or set(key_value) != {"url", "file", "sha256"}:
            raise ValueError(f"builderRpms.keys.{key_id} has unexpected fields")
        key = _validate_artifact(key_value, f"builderRpms.keys.{key_id}")
        key_url = urlsplit(key["url"])
        if (
            key_url.query
            or key_url.fragment
            or PurePosixPath(key_url.path).name != key["file"]
        ):
            raise ValueError(
                f"builderRpms.keys.{key_id}.url basename must equal file without query or fragment"
            )
        if key["url"] in key_urls or key["file"] in key_files:
            raise ValueError("duplicate builderRpms signing key URL or file")
        key_urls.add(key["url"])
        key_files.add(key["file"])
        keys[key_id] = key

    platforms_value = value["platforms"]
    if not isinstance(platforms_value, dict) or set(platforms_value) != set(
        _BUILDER_PLATFORMS
    ):
        raise ValueError("builderRpms.platforms must contain exactly the supported targets")
    used_keys: set[str] = set()
    platforms: dict[str, dict[str, list[dict[str, str]]]] = {}
    for target in _BUILDER_PLATFORMS:
        platform_value = platforms_value[target]
        if not isinstance(platform_value, dict) or set(platform_value) != {"packages"}:
            raise ValueError(f"builderRpms.platforms.{target} must contain only packages")
        packages_value = platform_value["packages"]
        if not isinstance(packages_value, list) or not packages_value:
            raise ValueError(f"builderRpms.platforms.{target}.packages must be non-empty")
        packages: list[dict[str, str]] = []
        files: set[str] = set()
        urls: set[str] = set()
        nevras: set[str] = set()
        allowed_arches = {_RPM_ARCH_FOR_PLATFORM[target], "noarch"}
        for index, package_value in enumerate(packages_value):
            label = f"builderRpms.platforms.{target}.packages[{index}]"
            if not isinstance(package_value, dict) or set(package_value) != {
                "url",
                "file",
                "sha256",
                "nevra",
                "signingKey",
            }:
                raise ValueError(f"{label} has unexpected fields")
            artifact = _validate_artifact(package_value, label)
            package_url = urlsplit(artifact["url"])
            if (
                package_url.query
                or package_url.fragment
                or PurePosixPath(package_url.path).name != artifact["file"]
            ):
                raise ValueError(
                    f"{label}.url basename must equal file without query or fragment"
                )
            if not artifact["file"].endswith(".rpm"):
                raise ValueError(f"{label}.file must be an RPM basename")
            nevra = package_value["nevra"]
            if (
                not isinstance(nevra, str)
                or not nevra
                or any(character.isspace() or ord(character) < 32 for character in nevra)
                or "/" in nevra
                or "\\" in nevra
            ):
                raise ValueError(f"{label}.nevra must be one safe NEVRA line")
            rpm_arch = nevra.rsplit(".", 1)[-1]
            file_arch = artifact["file"].removesuffix(".rpm").rsplit(".", 1)[-1]
            if rpm_arch not in allowed_arches or file_arch not in allowed_arches:
                raise ValueError(f"{label} has the wrong RPM architecture for {target}")
            signing_key = package_value["signingKey"]
            if not isinstance(signing_key, str) or signing_key not in keys:
                raise ValueError(f"{label}.signingKey does not reference builderRpms.keys")
            if artifact["file"] in files or artifact["url"] in urls or nevra in nevras:
                raise ValueError(f"duplicate builderRpms package file, URL or NEVRA for {target}")
            files.add(artifact["file"])
            urls.add(artifact["url"])
            nevras.add(nevra)
            used_keys.add(signing_key)
            packages.append(
                {
                    **artifact,
                    "nevra": nevra,
                    "signingKey": signing_key,
                }
            )
        platforms[target] = {"packages": packages}
    if used_keys != set(keys):
        raise ValueError("builderRpms contains an unused signing key")
    return {"keys": keys, "platforms": platforms}


def _validate_recipe(recipe: Any) -> dict[str, Any]:
    if not isinstance(recipe, dict) or recipe.get("schemaVersion") != 1:
        raise ValueError("source.json schemaVersion must be 1")
    sharp = recipe.get("sharp")
    if not isinstance(sharp, dict) or not isinstance(sharp.get("url"), str) or not sharp["url"]:
        raise ValueError("sharp.url is required")
    _safe_path(sharp.get("root"), "sharp.root", root=True)
    validated = dict(recipe)
    validated["recipePatch"] = _validate_repo_artifact(
        recipe.get("recipePatch"), "recipe patch"
    )
    validated["builderBase"] = _validate_builder_base(recipe.get("builderBase"))
    validated["builderRpms"] = _validate_builder_rpms(recipe.get("builderRpms"))
    validated["pythonTools"] = _validate_python_tools(recipe.get("pythonTools"))
    validated["rustToolchain"] = _validate_rust_toolchain(recipe.get("rustToolchain"))
    validated["cargoC"] = _validate_cargo_c(recipe.get("cargoC"))
    validated["buildInputs"] = _validate_build_inputs(recipe.get("buildInputs"))
    return validated


def _render_builder_base(builder_base: dict[str, Any]) -> str:
    repository = builder_base["repository"]
    lines = [
        "#!/usr/bin/env bash",
        "unset CLOUD_AGENTS_BUILDER_PLATFORM CLOUD_AGENTS_BUILDER_BASE_IMAGE",
        'case "${PLATFORM:-}" in',
    ]
    for target in _BUILDER_PLATFORMS:
        record = builder_base["platforms"][target]
        image = f"{repository}@{record['manifestDigest']}"
        lines.extend(
            [
                f"  {target})",
                f"    CLOUD_AGENTS_BUILDER_PLATFORM={shlex.quote(record['platform'])}",
                f"    CLOUD_AGENTS_BUILDER_BASE_IMAGE={shlex.quote(image)}",
                "    ;;",
            ]
        )
    lines.extend(
        [
            "  *)",
            '    printf \'%s\\n\' "builder-base: unsupported PLATFORM" >&2',
            "    return 1",
            "    ;;",
            "esac",
            "",
        ]
    )
    return "\n".join(lines)


def _render_read_source(build_inputs: list[dict[str, str]]) -> str:
    lines = [
        "#!/usr/bin/env bash",
        "read_source() {",
        '  if [[ "$#" -ne 1 ]]; then',
        '    printf \'%s\\n\' "read_source: expected exactly one URL" >&2',
        "    return 2",
        "  fi",
        '  if [[ -z "${PACKAGE:-}" ]]; then',
        '    printf \'%s\\n\' "read_source: PACKAGE is required" >&2',
        "    return 1",
        "  fi",
        "  local source_file source_sha256 actual_sha256",
        '  case "$1" in',
    ]
    for record in build_inputs:
        lines.extend(
            [
                f"    {shlex.quote(record['url'])})",
                f'      source_file="${{PACKAGE}}/cloud-agents/sources/{record["file"]}"',
                f"      source_sha256={shlex.quote(record['sha256'])}",
                "      ;;",
            ]
        )
    lines.extend(
        [
            "    *)",
            '      printf \'%s\\n\' "read_source: URL is not bound" >&2',
            "      return 1",
            "      ;;",
            "  esac",
            '  if [[ ! -f "$source_file" || -L "$source_file" ]]; then',
            '    printf \'%s\\n\' "read_source: bound source is not a regular file" >&2',
            "    return 1",
            "  fi",
            '  if ! actual_sha256=$(sha256sum -- "$source_file"); then',
            "    return 1",
            "  fi",
            '  actual_sha256=${actual_sha256%% *}',
            '  if [[ "$actual_sha256" != "$source_sha256" ]]; then',
            '    printf \'%s\\n\' "read_source: bound source digest mismatch" >&2',
            "    return 1",
            "  fi",
            '  cat -- "$source_file"',
            "}",
            "",
        ]
    )
    return "\n".join(lines)


def _render_python_requirements(python_tools: dict[str, Any], target: str) -> str:
    meson = python_tools["meson"]
    ninja = python_tools["ninja"]
    return (
        f"meson=={meson['version']} --hash=sha256:{meson['wheel']['sha256']}\n"
        f"ninja=={ninja['version']} --hash=sha256:{ninja['wheels'][target]['sha256']}\n"
    )


def _render_rust_sums(
    manifest: dict[str, str], archives: list[dict[str, str]]
) -> str:
    records = [manifest, *archives]
    return "".join(f"{record['sha256']}  {record['file']}\n" for record in records)


def _render_rust_install(rust_manifest: dict[str, Any], target: str) -> str:
    archives = rust_manifest["platforms"][target]
    rust_target = archives[0]["rustTarget"]
    host = shlex.quote(f"host: {rust_target}")
    rustc_commit = shlex.quote(f"commit-hash: {rust_manifest['commits']['rustc']}")
    cargo_archive = next(record for record in archives if record["component"] == "cargo")
    lines = [
        "#!/bin/sh",
        "set -eu",
        'cd "$(dirname "$0")"',
        "sha256sum -c SHA256SUMS",
    ]
    for archive in archives:
        lines.extend(
            [
                f"tar -xJf {archive['file']}",
                f"./{archive['root']}/install.sh --prefix=/usr/local --disable-ldconfig",
            ]
        )
    lines.extend(
        [
            f"cmp -- ./{cargo_archive['root']}/cargo/bin/cargo /usr/local/bin/cargo",
            "rustc_version=$(/usr/local/bin/rustc -vV)",
            f"printf '%s\\n' \"$rustc_version\" | grep -Fx {host} >/dev/null",
            f"printf '%s\\n' \"$rustc_version\" | grep -Fx {rustc_commit} >/dev/null",
            "cargo_version=$(/usr/local/bin/cargo -vV)",
            f"printf '%s\\n' \"$cargo_version\" | grep -Fx {host} >/dev/null",
            "",
        ]
    )
    return "\n".join(lines)


def _render_cargo_c_install(cargo_c: dict[str, Any]) -> str:
    source = cargo_c["source"]
    vendor = cargo_c["vendor"]
    lock_sha256 = cargo_c["lockSha256"]
    return "\n".join(
        [
            "#!/bin/sh",
            "set -eu",
            'script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)',
            'cd "$script_directory"',
            "sha256sum -c SHA256SUMS",
            f"tar -xf {source['file']}",
            f"tar -xJf {vendor['file']}",
            f'source_directory="$script_directory/{source["root"]}"',
            f'vendor_directory="$script_directory/{vendor["root"]}"',
            'mkdir -p "$source_directory/.cargo"',
            "{",
            "  printf '%s\\n' '[source.crates-io]' 'replace-with = \"vendored-sources\"'",
            "  printf '%s\\n' '[source.vendored-sources]'",
            "  printf 'directory = \"%s\"\\n' \"$vendor_directory\"",
            '} >"$source_directory/.cargo/config.toml"',
            f"printf '%s  %s\\n' {lock_sha256} Cargo.lock | (cd \"$source_directory\" && sha256sum -c -)",
            'cd "$source_directory"',
            "/usr/local/bin/cargo install --path . --locked --offline --root /usr/local/cargo",
            f"printf '%s  %s\\n' {lock_sha256} Cargo.lock | sha256sum -c -",
            "",
        ]
    )


def _archive_root(
    path: Path,
    expected_root: str,
    label: str,
    required_members: set[str] | None = None,
    allow_implicit_root: bool = False,
) -> None:
    required = required_members or set()
    inspection = _INSPECTOR.inspect_tar(str(path), required)
    selected = {record["path"] for record in inspection["selectedMembers"]}
    if selected != required:
        missing = sorted(required - selected)
        raise ValueError(f"{label} required regular members are missing: {missing!r}")
    with tarfile.open(path, "r:*") as archive:
        roots: set[str] = set()
        found_directory = False
        for member in archive.getmembers():
            name = member.name[:-1] if member.isdir() and member.name.endswith("/") else member.name
            if not name:
                continue
            roots.add(name.split("/", 1)[0])
            found_directory |= name == expected_root and member.isdir()
        if roots != {expected_root} or (not allow_implicit_root and not found_directory):
            raise ValueError(f"{label} must contain exactly root directory {expected_root!r}")


def _archive_member_bytes(path: Path, member_name: str, label: str) -> bytes:
    with tarfile.open(path, "r:*") as archive:
        try:
            member = archive.getmember(member_name)
        except KeyError as exc:
            raise ValueError(f"{label} is missing: {member_name}") from exc
        if not member.isreg():
            raise ValueError(f"{label} must be a regular file: {member_name}")
        stream = archive.extractfile(member)
        if stream is None:
            raise ValueError(f"{label} is unreadable: {member_name}")
        with stream:
            return stream.read()


def _librsvg_vendor(librsvg: Any) -> dict[str, str]:
    if not isinstance(librsvg, dict):
        raise ValueError("librsvg must be an object")
    source_root = _safe_path(librsvg.get("root"), "librsvg.root", root=True)
    registry = librsvg.get("registry")
    if not isinstance(registry, str) or not registry.startswith("sparse+"):
        raise ValueError("librsvg.registry must be a sparse credential-free HTTPS URL")
    registry_url = registry.removeprefix("sparse+")
    _build_input_url(registry_url, "librsvg.registry")
    parsed_registry = urlsplit(registry_url)
    if parsed_registry.query or parsed_registry.fragment or not parsed_registry.path.endswith("/"):
        raise ValueError("librsvg.registry must end in / without a query or fragment")
    vendor = librsvg.get("vendor")
    if not isinstance(vendor, dict) or set(vendor) != {"file", "sha256"}:
        raise ValueError("librsvg.vendor must contain exactly file and sha256 fields")
    file = vendor.get("file")
    expected_file = f"{source_root}-vendor.tar.xz"
    if file != expected_file or not _BUILD_INPUT_FILE.fullmatch(expected_file):
        raise ValueError(f"librsvg.vendor.file must be {expected_file}")
    root = _safe_path(file.removesuffix(".tar.xz"), "librsvg vendor root", root=True)
    return {
        "file": file,
        "root": root,
        "sha256": _canonical_sha256(vendor.get("sha256"), "librsvg vendor"),
    }


def _install_librsvg_vendor(source: Path, archive: Path, root: str) -> None:
    vendor = source / "vendor"
    cargo = source / ".cargo"
    for conflict in (vendor, cargo / "config", cargo / "config.toml"):
        if os.path.lexists(conflict):
            raise ValueError(f"librsvg vendor destination already exists: {conflict}")
    if os.path.lexists(cargo) and (not cargo.is_dir() or cargo.is_symlink()):
        raise ValueError(f"librsvg Cargo configuration directory is not a directory: {cargo}")

    staging = Path(tempfile.mkdtemp(prefix=".librsvg-vendor.", dir=source.parent))
    try:
        with tarfile.open(archive, "r:*") as bundled:
            bundled.extractall(staging, filter="data")
        extracted = staging / root
        if not extracted.is_dir() or extracted.is_symlink():
            raise ValueError(f"extracted librsvg vendor root is not a directory: {root}")
        extracted.replace(vendor)
    finally:
        shutil.rmtree(staging, ignore_errors=True)

    cargo.mkdir(exist_ok=True)
    (cargo / "config.toml").write_text(
        '[source.crates-io]\n'
        'replace-with = "cloud-agents-vendor"\n\n'
        '[source.cloud-agents-vendor]\n'
        'directory = "vendor"\n\n'
        '[net]\n'
        'offline = true\n',
        encoding="utf-8",
    )


def _builder_rpm_inputs(
    directory: Path, authority: dict[str, Any]
) -> tuple[dict[str, Path], dict[str, dict[str, Path]]]:
    root = _verified_directory(Path(directory), "builder RPM directory")
    expected_root = {"keys", *_BUILDER_PLATFORMS}
    actual_root = {entry.name for entry in root.iterdir()}
    if actual_root != expected_root:
        raise ValueError("builder RPM directory has missing or unexpected entries")

    key_directory = _verified_directory(root / "keys", "builder RPM key directory")
    expected_keys = {record["file"] for record in authority["keys"].values()}
    if {entry.name for entry in key_directory.iterdir()} != expected_keys:
        raise ValueError("builder RPM key directory has missing or unexpected entries")
    key_inputs = {
        key_id: _verified(
            key_directory / record["file"],
            f"builder RPM signing key {key_id}",
            record["sha256"],
        )
        for key_id, record in authority["keys"].items()
    }

    package_inputs: dict[str, dict[str, Path]] = {}
    for target in _BUILDER_PLATFORMS:
        target_directory = _verified_directory(
            root / target, f"builder RPM directory {target}"
        )
        packages = authority["platforms"][target]["packages"]
        expected_packages = {record["file"] for record in packages}
        if {entry.name for entry in target_directory.iterdir()} != expected_packages:
            raise ValueError(
                f"builder RPM directory {target} has missing or unexpected packages"
            )
        package_inputs[target] = {
            record["file"]: _verified(
                target_directory / record["file"],
                f"builder RPM package {target}/{record['file']}",
                record["sha256"],
            )
            for record in packages
        }
    return key_inputs, package_inputs


def _render_rpm_install(keys: list[tuple[str, str]]) -> str:
    lines = ["""#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
sha256sum -c SHA256SUMS

declare -A key_databases=()
cleanup() {
  rm -rf -- "${key_databases[@]}"
}
trap cleanup EXIT
"""]
    for key_id, key_file in keys:
        lines.append(
            f"""rpm_db="$(mktemp -d)"
key_databases[{shlex.quote(key_id)}]="${{rpm_db}}"
rpm --dbpath "${{rpm_db}}" --initdb
rpm --dbpath "${{rpm_db}}" --import {shlex.quote(f'keys/{key_file}')}
"""
        )
    lines.append("""
packages=()
while IFS=$'\t' read -r rpm_file nevra signing_key key_file; do
  actual_nevra="$(rpm -qp --qf '%{NEVRA}\\n' "packages/${rpm_file}")"
  [[ "${actual_nevra}" == "${nevra}" ]]
  rpm_db="${key_databases[$signing_key]:-}"
  [[ -n "${rpm_db}" ]]
  signature_output="$(rpmkeys --dbpath "${rpm_db}" --checksig --verbose "packages/${rpm_file}" 2>&1)"
  case "${signature_output}" in
    *Signature*": OK"*) ;;
    *) printf '%s\\n' "RPM signature verification failed" >&2; exit 1 ;;
  esac
  packages+=("packages/${rpm_file}")
done < manifest.tsv

for key_file in keys/*; do
  rpm --import "${key_file}"
done
dnf --disablerepo='*' --setopt=localpkg_gpgcheck=1 --setopt=install_weak_deps=False --setopt=tsflags=nodocs -y install "${packages[@]}"
while IFS=$'\t' read -r rpm_file nevra signing_key key_file; do
  installed_nevra="$(rpm -q --qf '%{NEVRA}\\n' "${nevra}")"
  [[ "${installed_nevra}" == "${nevra}" ]]
done < manifest.tsv
""")
    return "".join(lines)


def _repo_path(repo_root: Path, value: Any, label: str) -> Path:
    relative = _safe_path(value, f"{label}.path")
    path = repo_root / Path(*PurePosixPath(relative).parts)
    try:
        path.resolve().relative_to(repo_root.resolve())
    except ValueError as exc:
        raise ValueError(f"{label} escapes repository root") from exc
    return path


def _apply_patch(context: Path, patch_file: Path, label: str = "recipe patch") -> None:
    patch_command = shutil.which("patch")
    if patch_command is None:
        raise ValueError("patch executable is unavailable")
    result = subprocess.run(
        [patch_command, "--batch", "--forward", "--fuzz=0", "-V", "none", "-p1", "-i", str(patch_file)],
        cwd=context,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode:
        detail = (result.stderr or result.stdout).strip()
        raise ValueError(f"{label} failed: {detail}")


def prepare_librsvg_source(
    *,
    librsvg: dict[str, Any],
    cargo_lock: dict[str, Any],
    repo_root: Path,
    librsvg_archive: Path,
    librsvg_security_patch: Path,
    output_dir: Path,
) -> Path:
    """Prepare librsvg sources with the bound feature patch, lock and security patch."""

    output = Path(output_dir).absolute()
    if os.path.lexists(output):
        raise ValueError(f"output already exists: {output}")
    if not isinstance(librsvg, dict):
        raise ValueError("librsvg must be an object")
    if not isinstance(librsvg.get("url"), str) or not librsvg["url"]:
        raise ValueError("librsvg.url is required")
    repo = Path(repo_root).absolute()
    feature_authority = _validate_repo_artifact(
        librsvg.get("featurePatch"), "librsvg feature patch"
    )
    lock_authority = _validate_repo_artifact(cargo_lock, "Cargo.lock")
    security_authority = librsvg.get("securityPatch")
    if not isinstance(security_authority, dict):
        raise ValueError("librsvg security patch must be an object")
    if not isinstance(security_authority.get("url"), str):
        raise ValueError("librsvg.securityPatch.url is required")
    security_sha256 = _canonical_sha256(
        security_authority.get("sha256"), "librsvg security patch"
    )

    archive = _verified(
        Path(librsvg_archive), "librsvg archive", librsvg.get("sha256")
    )
    root = _safe_path(librsvg.get("root"), "librsvg.root", root=True)
    _archive_root(
        archive,
        root,
        "librsvg archive",
        {f"{root}/Cargo.lock"},
    )
    feature_patch = _repo_path(
        repo, feature_authority["path"], "librsvg feature patch"
    )
    _verified(
        feature_patch,
        "librsvg feature patch",
        feature_authority["sha256"],
    )
    lock = _repo_path(repo, lock_authority["path"], "Cargo.lock")
    _verified(lock, "Cargo.lock", lock_authority["sha256"])
    security_patch = _verified(
        Path(librsvg_security_patch),
        "librsvg security patch",
        security_sha256,
    )

    output.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f".{output.name}.", dir=output.parent))
    output_created = False
    try:
        with tarfile.open(archive, "r:*") as source:
            source.extractall(staging, filter="data")
        context = staging / root
        if not context.is_dir() or context.is_symlink():
            raise ValueError(f"extracted librsvg root is not a directory: {root}")
        _apply_patch(context, feature_patch, "librsvg feature patch")
        copied_lock = context / "Cargo.lock"
        shutil.copy2(lock, copied_lock)
        _verified(copied_lock, "prepared librsvg Cargo.lock", lock_authority["sha256"])
        _apply_patch(context, security_patch, "librsvg security patch")
        _verified(copied_lock, "prepared librsvg Cargo.lock", lock_authority["sha256"])

        output.mkdir()
        output_created = True
        for child in context.iterdir():
            child.replace(output / child.name)
        shutil.rmtree(staging)
        return output
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        if output_created:
            shutil.rmtree(output, ignore_errors=True)
        raise


def prepare(
    recipe: dict[str, Any],
    repo_root: Path,
    sharp_archive: Path,
    librsvg_archive: Path,
    librsvg_security_patch: Path,
    librsvg_vendor_dir: Path,
    builder_rpm_dir: Path,
    sources_dir: Path,
    python_wheelhouse: Path,
    rust_dist_dir: Path,
    cargo_c_dir: Path,
    output_dir: Path,
) -> Path:
    """Validate inputs, prepare a patched source context, and return its root."""

    output = Path(output_dir).absolute()
    if os.path.lexists(output):
        raise ValueError(f"output already exists: {output}")
    repo = Path(repo_root).absolute()
    declared_recipe = recipe
    source_path = repo / Path(*SOURCE_RELATIVE_PATH.parts)
    source_bytes = _verified(source_path, "source.json").read_bytes()
    try:
        source_recipe = json.loads(source_bytes)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError(f"source.json is invalid: {exc}") from exc
    recipe = _validate_recipe(recipe)
    sharp, librsvg = recipe["sharp"], recipe.get("librsvg")
    builder_rpms = recipe["builderRpms"]
    rpm_key_inputs, rpm_package_inputs = _builder_rpm_inputs(
        Path(builder_rpm_dir), builder_rpms
    )
    librsvg_vendor = _librsvg_vendor(librsvg)
    vendor_directory = _verified_directory(
        Path(librsvg_vendor_dir), "librsvg vendor directory"
    )
    vendor_archive = _verified(
        vendor_directory / librsvg_vendor["file"],
        "librsvg vendor archive",
        librsvg_vendor["sha256"],
    )
    _archive_root(
        vendor_archive,
        librsvg_vendor["root"],
        "librsvg vendor archive",
    )
    vendor_inspection = _INSPECTOR.inspect_tar(str(vendor_archive), set())
    if vendor_inspection["symlinks"] or vendor_inspection["hardlinks"]:
        raise ValueError("librsvg vendor archive must not contain links")
    cargo_lock, recipe_patch = recipe.get("cargoLock"), recipe["recipePatch"]
    sources = _verified_directory(Path(sources_dir), "sources directory")
    source_inputs = [
        (
            record,
            _verified(
                sources / record["file"],
                f"build input {record['file']}",
                record["sha256"],
            ),
        )
        for record in recipe["buildInputs"]
    ]
    wheelhouse = _verified_directory(Path(python_wheelhouse), "Python wheelhouse")
    python_tools = recipe["pythonTools"]
    wheel_records = [
        python_tools["meson"]["wheel"],
        *python_tools["ninja"]["wheels"].values(),
    ]
    wheel_inputs = {
        record["file"]: _verified(
            wheelhouse / record["file"],
            f"Python wheel {record['file']}",
            record["sha256"],
        )
        for record in wheel_records
    }
    rust_dist = _verified_directory(Path(rust_dist_dir), "Rust dist directory")
    rust_authority = recipe["rustToolchain"]["manifest"]
    rust_manifest_path = _verified(
        rust_dist / rust_authority["file"],
        "Rust manifest",
        rust_authority["sha256"],
    )
    rust_manifest = _parse_rust_manifest(rust_manifest_path, rust_authority)
    rust_inputs: dict[str, Path] = {}
    for archives in rust_manifest["platforms"].values():
        for record in archives:
            archive = _verified(
                rust_dist / record["file"],
                f"Rust archive {record['file']}",
                record["sha256"],
            )
            required_members = {f"{record['root']}/install.sh"}
            if record["component"] == "cargo":
                required_members.add(f"{record['root']}/cargo/bin/cargo")
            _archive_root(
                archive,
                record["root"],
                f"Rust archive {record['file']}",
                required_members,
            )
            rust_inputs[record["file"]] = archive
    cargo_c_directory = _verified_directory(Path(cargo_c_dir), "cargo-c directory")
    cargo_c = recipe["cargoC"]
    cargo_c_source = _verified(
        cargo_c_directory / cargo_c["source"]["file"],
        "cargo-c source archive",
        cargo_c["source"]["sha256"],
    )
    cargo_c_source_members = {
        f"{cargo_c['source']['root']}/Cargo.toml",
        f"{cargo_c['source']['root']}/Cargo.lock",
    }
    _archive_root(
        cargo_c_source,
        cargo_c["source"]["root"],
        "cargo-c source archive",
        cargo_c_source_members,
        allow_implicit_root=True,
    )
    cargo_toml_path = f"{cargo_c['source']['root']}/Cargo.toml"
    cargo_lock_path = f"{cargo_c['source']['root']}/Cargo.lock"
    try:
        cargo_toml = tomllib.loads(
            _archive_member_bytes(
                cargo_c_source, cargo_toml_path, "cargo-c Cargo.toml"
            ).decode("utf-8")
        )
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as exc:
        raise ValueError(f"cargo-c Cargo.toml is invalid: {exc}") from exc
    package = cargo_toml.get("package")
    if not isinstance(package, dict) or package.get("name") != "cargo-c":
        raise ValueError("cargo-c Cargo.toml package name must be cargo-c")
    if package.get("version") != cargo_c["version"]:
        raise ValueError("cargo-c Cargo.toml package version does not match authority")
    cargo_c_lock = _archive_member_bytes(
        cargo_c_source, cargo_lock_path, "cargo-c Cargo.lock"
    )
    cargo_c["lockSha256"] = hashlib.sha256(cargo_c_lock).hexdigest()

    cargo_c_vendor = _verified(
        cargo_c_directory / cargo_c["vendor"]["file"],
        "cargo-c vendor archive",
        cargo_c["vendor"]["sha256"],
    )
    _archive_root(
        cargo_c_vendor,
        cargo_c["vendor"]["root"],
        "cargo-c vendor archive",
    )
    sharp_path = _verified(Path(sharp_archive), "sharp archive", sharp.get("sha256"))
    recipe_patch_path = _repo_path(repo, recipe_patch.get("path"), "recipe patch")
    _verified(recipe_patch_path, "recipe patch", recipe_patch.get("sha256"))
    feature_patch = _validate_repo_artifact(
        librsvg.get("featurePatch"), "librsvg feature patch"
    )
    cargo_lock = _validate_repo_artifact(cargo_lock, "Cargo.lock")
    provenance_inputs = (
        (recipe_patch, recipe_patch_path, "recipe patch"),
        (
            feature_patch,
            _repo_path(repo, feature_patch["path"], "librsvg feature patch"),
            "librsvg feature patch",
        ),
        (
            cargo_lock,
            _repo_path(repo, cargo_lock["path"], "Cargo.lock"),
            "Cargo.lock",
        ),
    )
    for authority, path, label in provenance_inputs:
        _verified(path, label, authority["sha256"])
    _archive_root(sharp_path, sharp["root"], "sharp archive")

    output.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=f".{output.name}.", dir=output.parent))
    output_created = False
    try:
        with tarfile.open(sharp_path, "r:*") as archive:
            archive.extractall(staging, filter="data")
        context = staging / sharp["root"]
        if not context.is_dir() or context.is_symlink():
            raise ValueError(f"extracted sharp root is not a directory: {sharp['root']}")
        bound = context / "cloud-agents"
        bound.mkdir()
        librsvg_source = prepare_librsvg_source(
            librsvg=librsvg,
            cargo_lock=cargo_lock,
            repo_root=repo,
            librsvg_archive=librsvg_archive,
            librsvg_security_patch=librsvg_security_patch,
            output_dir=bound / "librsvg-source",
        )
        _install_librsvg_vendor(
            librsvg_source, vendor_archive, librsvg_vendor["root"]
        )
        bound_sources = bound / "sources"
        bound_sources.mkdir()
        for record, source_path in source_inputs:
            copied_source = bound_sources / record["file"]
            shutil.copy2(source_path, copied_source)
            _verified(copied_source, f"copied build input {record['file']}", record["sha256"])
        read_source = bound / "read-source.sh"
        read_source.write_text(_render_read_source(recipe["buildInputs"]), encoding="utf-8")
        read_source.chmod(0o755)
        builder_base = bound / "builder-base.sh"
        builder_base.write_text(_render_builder_base(recipe["builderBase"]), encoding="utf-8")
        builder_base.chmod(0o755)
        for target in _BUILDER_PLATFORMS:
            target_rpms = context / "platforms" / target / "cloud-agents-rpms"
            package_directory = target_rpms / "packages"
            key_directory = target_rpms / "keys"
            package_directory.mkdir(parents=True)
            key_directory.mkdir()
            packages = builder_rpms["platforms"][target]["packages"]
            used_key_ids = sorted({record["signingKey"] for record in packages})
            for key_id in used_key_ids:
                record = builder_rpms["keys"][key_id]
                copied_key = key_directory / record["file"]
                shutil.copy2(rpm_key_inputs[key_id], copied_key)
                _verified(copied_key, f"copied RPM signing key {key_id}", record["sha256"])
            for record in packages:
                copied_package = package_directory / record["file"]
                shutil.copy2(rpm_package_inputs[target][record["file"]], copied_package)
                _verified(
                    copied_package,
                    f"copied RPM package {target}/{record['file']}",
                    record["sha256"],
                )
            (target_rpms / "SHA256SUMS").write_text(
                "".join(
                    [
                        f"{builder_rpms['keys'][key_id]['sha256']}  keys/{builder_rpms['keys'][key_id]['file']}\n"
                        for key_id in used_key_ids
                    ]
                    + [
                        f"{record['sha256']}  packages/{record['file']}\n"
                        for record in packages
                    ]
                ),
                encoding="utf-8",
            )
            (target_rpms / "manifest.tsv").write_text(
                "".join(
                    "\t".join(
                        (
                            record["file"],
                            record["nevra"],
                            record["signingKey"],
                            builder_rpms["keys"][record["signingKey"]]["file"],
                        )
                    )
                    + "\n"
                    for record in packages
                ),
                encoding="utf-8",
            )
            rpm_install = target_rpms / "install.sh"
            rpm_install.write_text(
                _render_rpm_install(
                    [
                        (key_id, builder_rpms["keys"][key_id]["file"])
                        for key_id in used_key_ids
                    ]
                ),
                encoding="utf-8",
            )
            rpm_install.chmod(0o755)
            target_tools = context / "platforms" / target / "cloud-agents-python-tools"
            target_tools.mkdir(parents=True)
            target_wheels = [
                python_tools["meson"]["wheel"],
                python_tools["ninja"]["wheels"][target],
            ]
            for record in target_wheels:
                copied_wheel = target_tools / record["file"]
                shutil.copy2(wheel_inputs[record["file"]], copied_wheel)
                _verified(copied_wheel, f"copied Python wheel {record['file']}", record["sha256"])
            (target_tools / "requirements.txt").write_text(
                _render_python_requirements(python_tools, target), encoding="utf-8"
            )
            target_rust = context / "platforms" / target / "cloud-agents-rust"
            target_rust.mkdir()
            copied_manifest = target_rust / rust_authority["file"]
            shutil.copy2(rust_manifest_path, copied_manifest)
            _verified(copied_manifest, "copied Rust manifest", rust_authority["sha256"])
            target_archives = rust_manifest["platforms"][target]
            for record in target_archives:
                copied_archive = target_rust / record["file"]
                shutil.copy2(rust_inputs[record["file"]], copied_archive)
                _verified(
                    copied_archive,
                    f"copied Rust archive {record['file']}",
                    record["sha256"],
                )
            (target_rust / "SHA256SUMS").write_text(
                _render_rust_sums(rust_authority, target_archives), encoding="utf-8"
            )
            rust_install = target_rust / "install.sh"
            rust_install.write_text(
                _render_rust_install(rust_manifest, target), encoding="utf-8"
            )
            rust_install.chmod(0o755)
            target_cargo_c = context / "platforms" / target / "cloud-agents-cargo-c"
            target_cargo_c.mkdir()
            for record, source_path in (
                (cargo_c["source"], cargo_c_source),
                (cargo_c["vendor"], cargo_c_vendor),
            ):
                copied = target_cargo_c / record["file"]
                shutil.copy2(source_path, copied)
                _verified(copied, f"copied cargo-c input {record['file']}", record["sha256"])
            (target_cargo_c / "SHA256SUMS").write_text(
                "".join(
                    f"{cargo_c[key]['sha256']}  {cargo_c[key]['file']}\n"
                    for key in ("source", "vendor")
                ),
                encoding="utf-8",
            )
            cargo_c_install = target_cargo_c / "install.sh"
            cargo_c_install.write_text(_render_cargo_c_install(cargo_c), encoding="utf-8")
            cargo_c_install.chmod(0o755)
        _apply_patch(context, recipe_patch_path)
        if json.dumps(source_recipe, sort_keys=True, separators=(",", ":")) != json.dumps(
            declared_recipe, sort_keys=True, separators=(",", ":")
        ):
            raise ValueError("source.json does not match the supplied recipe")
        provenance = bound / "source-provenance"
        copied_source = provenance / Path(*SOURCE_RELATIVE_PATH.parts)
        copied_source.parent.mkdir(parents=True)
        copied_source.write_bytes(source_bytes)
        for authority, path, label in provenance_inputs:
            copied = provenance / Path(*PurePosixPath(authority["path"]).parts)
            copied.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(path, copied)
            _verified(copied, f"copied {label}", authority["sha256"])
        output.mkdir()
        output_created = True
        for child in staging.iterdir():
            child.replace(output / child.name)
        staging.rmdir()
        return output / sharp["root"]
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        if output_created:
            shutil.rmtree(output, ignore_errors=True)
        raise


def _load_recipe(repo_root: Path) -> dict[str, Any]:
    source_path = repo_root / Path(*SOURCE_RELATIVE_PATH.parts)
    _verified(source_path, "source.json")
    try:
        return json.loads(source_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise ValueError(f"source.json is invalid: {exc}") from exc


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sharp-archive", required=True, type=Path)
    parser.add_argument("--librsvg-archive", required=True, type=Path)
    parser.add_argument("--librsvg-security-patch", required=True, type=Path)
    parser.add_argument("--librsvg-vendor-dir", required=True, type=Path)
    parser.add_argument("--builder-rpm-dir", required=True, type=Path)
    parser.add_argument("--sources-dir", required=True, type=Path)
    parser.add_argument("--python-wheelhouse", required=True, type=Path)
    parser.add_argument("--rust-dist-dir", required=True, type=Path)
    parser.add_argument("--cargo-c-dir", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    args = parser.parse_args(argv)
    try:
        repo_root = REPO_ROOT
        context = prepare(
            _load_recipe(repo_root),
            repo_root,
            args.sharp_archive,
            args.librsvg_archive,
            args.librsvg_security_patch,
            args.librsvg_vendor_dir,
            args.builder_rpm_dir,
            args.sources_dir,
            args.python_wheelhouse,
            args.rust_dist_dir,
            args.cargo_c_dir,
            args.output_dir,
        )
    except (OSError, ValueError, tarfile.TarError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    print(context)
    print(COMPLETION_NOTE, file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
