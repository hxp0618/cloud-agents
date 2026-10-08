"""Shared verified Cargo vendoring for native source producers."""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import tarfile
import tempfile
import tomllib
from typing import Any


FILTERED_GIT_NAMES = {".gitignore", ".gitattributes"}


def archive_inventory(
    path: Path, label: str, archive_api: Any
) -> dict[str, tuple[str, int]]:
    inspection = archive_api._INSPECTOR.inspect_tar(str(path), set())
    if inspection["symlinks"] or inspection["hardlinks"]:
        raise ValueError(f"{label} must contain only regular files and directories")
    inventory: dict[str, tuple[str, int]] = {}
    with tarfile.open(path, "r:*") as archive:
        for member in archive.getmembers():
            if member.isdir():
                continue
            if not member.isreg():
                raise ValueError(f"{label} contains a non-regular member: {member.name}")
            if member.mode & 0o7000:
                raise ValueError(f"{label} member has unsupported special mode: {member.name}")
            stream = archive.extractfile(member)
            if stream is None:
                raise ValueError(f"{label} member is unreadable: {member.name}")
            with stream:
                digest = archive_api._INSPECTOR.sha256_stream(stream)[0]
                inventory[member.name] = (digest, member.mode & 0o777)
    return inventory


def member_toml(path: Path, member: str, label: str, archive_api: Any) -> dict[str, Any]:
    try:
        value = tomllib.loads(
            archive_api._archive_member_bytes(path, member, label).decode("utf-8")
        )
    except (UnicodeDecodeError, tomllib.TOMLDecodeError) as exc:
        raise ValueError(f"{label} is invalid: {exc}") from exc
    if not isinstance(value, dict):
        raise ValueError(f"{label} must be a TOML table")
    return value


def _is_optional_filtered(relative: str) -> bool:
    path = PurePosixPath(relative)
    return (
        relative == ".git"
        or relative.startswith(".git/")
        or path.name in FILTERED_GIT_NAMES
        or relative == ".cargo-ok"
        or path.name.endswith(".orig")
        or path.name.endswith(".rej")
    )


def find_and_validate_crates(
    registry_dir: Path,
    packages: list[dict[str, str]],
    archive_api: Any,
) -> dict[tuple[str, str], dict[str, tuple[str, int]]]:
    cache = registry_dir / "cache"
    archive_api._verified_directory(cache, "Cargo registry cache")
    result: dict[tuple[str, str], dict[str, tuple[str, int]]] = {}
    for package in packages:
        name, version, checksum = package["name"], package["version"], package["checksum"]
        filename = f"{name}-{version}.crate"
        matches = [path for path in cache.glob(f"*/{filename}") if path.name == filename]
        if len(matches) != 1:
            raise ValueError(f"Cargo registry cache must contain exactly one {filename}")
        archive = archive_api._verified(matches[0], f"Cargo crate {filename}", checksum)
        root = filename.removesuffix(".crate")
        manifest_member = f"{root}/Cargo.toml"
        archive_api._archive_root(
            archive,
            root,
            f"Cargo crate {filename}",
            {manifest_member},
            allow_implicit_root=True,
        )
        inventory = archive_inventory(archive, f"Cargo crate {filename}", archive_api)
        manifest = member_toml(
            archive, manifest_member, f"Cargo crate {filename} Cargo.toml", archive_api
        )
        metadata = manifest.get("package")
        if (
            not isinstance(metadata, dict)
            or metadata.get("name") != name
            or metadata.get("version") != version
        ):
            raise ValueError(f"Cargo crate {filename} metadata does not match Cargo.lock")
        prefix = f"{root}/"
        relative_inventory: dict[str, tuple[str, int]] = {}
        for member, record in inventory.items():
            if not member.startswith(prefix) or member == root:
                raise ValueError(f"Cargo crate {filename} has an invalid root member")
            relative_inventory[member.removeprefix(prefix)] = record
        result[(name, version)] = relative_inventory
    return result


def copy_regular_tree(source: Path, destination: Path, label: str, archive_api: Any) -> None:
    archive_api._verified_directory(source, label)
    destination.mkdir(parents=True)
    for root, directories, files in os.walk(source, topdown=True, followlinks=False):
        root_path = Path(root)
        directories.sort()
        files.sort()
        for directory in directories:
            item = root_path / directory
            if not stat.S_ISDIR(item.lstat().st_mode):
                raise ValueError(f"{label} contains a symlink or special entry: {item}")
            target = destination / item.relative_to(source)
            target.mkdir(mode=0o755)
        for filename in files:
            item = root_path / filename
            if not stat.S_ISREG(item.lstat().st_mode):
                raise ValueError(f"{label} contains a symlink or special entry: {item}")
            target = destination / item.relative_to(source)
            shutil.copy2(item, target)


def write_cargo_config(source_root: Path, registry: str) -> None:
    cargo_directory = source_root / ".cargo"
    cargo_directory.mkdir(exist_ok=True)
    (cargo_directory / "config.toml").write_text(
        "\n".join(
            [
                "[source.crates-io]",
                'replace-with = "cloud-agents-registry"',
                "[source.cloud-agents-registry]",
                f"registry = {json.dumps(registry)}",
                "",
            ]
        ),
        encoding="utf-8",
    )


def _normalized_mode(mode: int) -> int:
    if mode & 0o7000:
        raise ValueError("Cargo file mode contains unsupported special bits")
    return (mode & 0o777) & ~0o022


def _sha256(path: Path, archive_api: Any) -> str:
    with path.open("rb") as stream:
        return archive_api._INSPECTOR.sha256_stream(stream)[0]


def validate_vendor(
    vendor_dir: Path,
    packages: list[dict[str, str]],
    crate_inventories: dict[tuple[str, str], dict[str, tuple[str, int]]],
    archive_api: Any,
) -> None:
    expected_directories = {f"{package['name']}-{package['version']}" for package in packages}
    actual_directories: set[str] = set()
    for entry in vendor_dir.iterdir():
        mode = entry.lstat().st_mode
        if not stat.S_ISDIR(mode):
            raise ValueError(f"cargo vendor produced a non-directory package entry: {entry.name}")
        actual_directories.add(entry.name)
    if actual_directories != expected_directories:
        raise ValueError("cargo vendor package directory set does not match Cargo.lock")

    for package in packages:
        name, version, package_checksum = (
            package["name"],
            package["version"],
            package["checksum"],
        )
        package_dir = vendor_dir / f"{name}-{version}"
        source_inventory = crate_inventories[(name, version)]
        actual_inventory: dict[str, tuple[str, int]] = {}
        for root, directories, files in os.walk(package_dir, topdown=True, followlinks=False):
            root_path = Path(root)
            directories.sort()
            files.sort()
            if stat.S_IMODE(root_path.lstat().st_mode) != 0o755:
                raise ValueError(f"cargo vendor directory mode is not 0755: {root_path}")
            for directory in directories:
                item = root_path / directory
                if not stat.S_ISDIR(item.lstat().st_mode):
                    raise ValueError(f"cargo vendor produced a link or special entry: {item}")
            for filename in files:
                item = root_path / filename
                mode = item.lstat().st_mode
                if not stat.S_ISREG(mode):
                    raise ValueError(f"cargo vendor produced a link or special entry: {item}")
                relative = item.relative_to(package_dir).as_posix()
                actual_inventory[relative] = (_sha256(item, archive_api), stat.S_IMODE(mode))

        checksum_record = actual_inventory.pop(".cargo-checksum.json", None)
        checksum_path = package_dir / ".cargo-checksum.json"
        if checksum_record is None or checksum_record[1] != 0o644:
            raise ValueError(f"cargo vendor checksum metadata is missing or has wrong mode: {name}")
        required = {
            relative for relative in source_inventory if not _is_optional_filtered(relative)
        }
        if not required.issubset(actual_inventory) or not set(actual_inventory).issubset(
            source_inventory
        ):
            raise ValueError(f"cargo vendor member set differs from crate: {name} {version}")
        for relative, (actual_digest, actual_mode) in actual_inventory.items():
            expected_digest, source_mode = source_inventory[relative]
            if actual_digest != expected_digest:
                raise ValueError(f"cargo vendor byte differs from crate: {name} {relative}")
            if actual_mode != _normalized_mode(source_mode):
                raise ValueError(
                    f"cargo vendor mode differs from Cargo normalization: {name} {relative}"
                )

        try:
            checksum = json.loads(checksum_path.read_text(encoding="utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise ValueError(f"cargo vendor checksum metadata is invalid: {name}: {exc}") from exc
        if not isinstance(checksum, dict) or set(checksum) != {"$comment", "files", "package"}:
            raise ValueError(f"cargo vendor checksum metadata shape is invalid: {name}")
        files = checksum.get("files")
        expected_files = {
            relative: record[0] for relative, record in sorted(actual_inventory.items())
        }
        if files != expected_files or checksum.get("package") != package_checksum:
            raise ValueError(f"cargo vendor checksum metadata does not match crate: {name}")


def write_vendor_archive(source: Path, destination: Path) -> None:
    paths = [source, *sorted(source.rglob("*"))]
    with tarfile.open(destination, "w:xz", format=tarfile.PAX_FORMAT, preset=3) as archive:
        for path in paths:
            relative = path.relative_to(source.parent).as_posix()
            path_mode = path.lstat().st_mode
            if not (stat.S_ISDIR(path_mode) or stat.S_ISREG(path_mode)):
                raise ValueError(f"vendor tree contains a link or special entry: {relative}")
            info = archive.gettarinfo(str(path), arcname=relative)
            info.uid = info.gid = info.mtime = 0
            info.uname = info.gname = ""
            if stat.S_ISREG(path_mode):
                with path.open("rb") as stream:
                    archive.addfile(info, stream)
            else:
                archive.addfile(info)


def cargo_executable(value: Path | None) -> Path:
    if value is None:
        located = shutil.which("cargo")
        if located is None:
            raise ValueError("cargo executable is unavailable")
        path = Path(located).absolute()
    else:
        path = Path(value).absolute()
    try:
        mode = path.stat().st_mode
    except FileNotFoundError as exc:
        raise ValueError(f"cargo executable is missing: {path}") from exc
    if not stat.S_ISREG(mode) or not os.access(path, os.X_OK):
        raise ValueError(f"cargo executable is not an executable file: {path}")
    return path


def cargo_environment(home: Path) -> dict[str, str]:
    environment = {
        key: value for key, value in os.environ.items() if not key.startswith("CARGO_")
    }
    if not environment.get("RUSTUP_HOME"):
        rustup_home = Path.home() / ".rustup"
        if rustup_home.is_dir():
            environment["RUSTUP_HOME"] = str(rustup_home)
    environment["HOME"] = str(home)
    environment["CARGO_HOME"] = str(home / "cargo-home")
    return environment


def run_cargo_vendor(
    *,
    source_root: Path,
    lock_bytes: bytes,
    registry_dir: Path,
    registry_url: str,
    packages: list[dict[str, str]],
    vendor_tree: Path,
    cargo_bin: Path | None,
    archive_api: Any,
    temporary_prefix: str,
) -> None:
    """Validate cached crates, run isolated Cargo, and verify its complete output."""

    registry = Path(registry_dir).absolute()
    archive_api._verified_directory(registry, "Cargo registry directory")
    archive_api._verified_directory(registry / "index", "Cargo registry index")
    crate_inventories = find_and_validate_crates(registry, packages, archive_api)
    executable = cargo_executable(cargo_bin)
    lock_digest = hashlib.sha256(lock_bytes).hexdigest()
    if hashlib.sha256((source_root / "Cargo.lock").read_bytes()).hexdigest() != lock_digest:
        raise ValueError("prepared Cargo.lock differs from validated bytes")

    with tempfile.TemporaryDirectory(prefix=temporary_prefix) as temporary:
        home = Path(temporary) / "home"
        cargo_home = home / "cargo-home"
        (cargo_home / "registry").mkdir(parents=True)
        copy_regular_tree(
            registry / "cache", cargo_home / "registry/cache", "Cargo registry cache", archive_api
        )
        copy_regular_tree(
            registry / "index", cargo_home / "registry/index", "Cargo registry index", archive_api
        )
        write_cargo_config(source_root, registry_url)
        vendor_tree.mkdir(mode=0o755)
        vendor_tree.chmod(0o755)
        result = subprocess.run(
            [
                str(executable),
                "vendor",
                "--frozen",
                "--respect-source-config",
                "--versioned-dirs",
                str(vendor_tree),
            ],
            cwd=source_root,
            env=cargo_environment(home),
            check=False,
            capture_output=True,
            text=True,
            umask=0o022,
        )
        if result.returncode:
            raise ValueError(f"cargo vendor failed with exit status {result.returncode}")
        if hashlib.sha256((source_root / "Cargo.lock").read_bytes()).hexdigest() != lock_digest:
            raise ValueError("cargo vendor modified Cargo.lock")
        validate_vendor(vendor_tree, packages, crate_inventories, archive_api)
