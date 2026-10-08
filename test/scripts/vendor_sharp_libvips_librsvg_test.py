#!/usr/bin/env python3
"""Focused tests for the reproducible librsvg Cargo vendor generator."""

from __future__ import annotations

import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[2] / "scripts/vendor-sharp-libvips-librsvg.py"
REGISTRY_SOURCE = "registry+https://github.com/rust-lang/crates.io-index"
REGISTRY = "sparse+https://rsproxy.cn/index/"
VENDOR_FILE = "librsvg-2.63.2-vendor.tar.xz"
VENDOR_ROOT = VENDOR_FILE.removesuffix(".tar.xz")


def digest_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def digest_file(path: Path) -> str:
    return digest_bytes(path.read_bytes())


def load_module():
    spec = importlib.util.spec_from_file_location("vendor_sharp_libvips_librsvg", SCRIPT)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {SCRIPT}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def tar_bytes(root: str, files: dict[str, tuple[bytes, int]]) -> bytes:
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w:gz", format=tarfile.PAX_FORMAT) as archive:
        for relative, (content, mode) in sorted(files.items()):
            member = tarfile.TarInfo(f"{root}/{relative}")
            member.size = len(content)
            member.mode = mode
            member.mtime = 0
            archive.addfile(member, io.BytesIO(content))
    return stream.getvalue()


def write_deterministic_vendor(path: Path, tree: Path) -> None:
    with tarfile.open(path, "w:xz", format=tarfile.PAX_FORMAT, preset=3) as archive:
        for source in [tree, *sorted(tree.rglob("*"))]:
            relative = source.relative_to(tree.parent).as_posix()
            info = archive.gettarinfo(str(source), arcname=relative)
            info.uid = info.gid = info.mtime = 0
            info.uname = info.gname = ""
            if source.is_file():
                with source.open("rb") as stream:
                    archive.addfile(info, stream)
            else:
                archive.addfile(info)


class Fixture:
    def __init__(self, root: Path) -> None:
        self.root = root
        self.repo = root / "repo"
        self.registry = root / "registry"
        self.librsvg_archive = root / "librsvg-2.63.2.tar.xz"
        self.security_patch = root / "security.patch"
        self.cargo_bin = root / "cargo"
        self.prepared = root / "prepared-fixture"
        self.prepare_calls: list[dict[str, object]] = []
        self.cargo_calls: list[tuple[list[str], Path, dict[str, str]]] = []
        self.vendor_mutation = "none"

        authority_dir = self.repo / "tools/sharp-libvips-successor/v1"
        authority_dir.mkdir(parents=True)
        self.librsvg_archive.write_bytes(b"bound librsvg archive fixture\n")
        self.security_patch.write_bytes(b"bound security patch fixture\n")
        self.cargo_bin.write_text("#!/bin/sh\nexit 99\n", encoding="utf-8")
        self.cargo_bin.chmod(0o755)

        cache = self.registry / "cache/rsproxy.cn-fixture"
        index = self.registry / "index/rsproxy.cn-fixture"
        cache.mkdir(parents=True)
        index.mkdir(parents=True)
        (index / "config.json").write_text("{}\n", encoding="utf-8")
        crate_files = {
            "Cargo.toml": (b'[package]\nname = "dep-one"\nversion = "1.0.0"\n', 0o644),
            "src/lib.rs": (b"pub fn one() {}\n", 0o640),
            ".gitignore": (b"target\n", 0o644),
        }
        self.crate = cache / "dep-one-1.0.0.crate"
        self.crate.write_bytes(tar_bytes("dep-one-1.0.0", crate_files))
        self.crate_files = crate_files
        self.crate_checksum = digest_file(self.crate)

        self.prepared.mkdir()
        (self.prepared / "crates/core/src").mkdir(parents=True)
        (self.prepared / "tools/cli/src").mkdir(parents=True)
        (self.prepared / "Cargo.toml").write_text(
            "[workspace]\nresolver = \"2\"\nmembers = [\"crates/core\", \"tools/cli\"]\n"
            "[workspace.package]\nversion = \"2.63.2\"\n",
            encoding="utf-8",
        )
        (self.prepared / "crates/core/Cargo.toml").write_text(
            '[package]\nname = "librsvg"\nversion.workspace = true\n', encoding="utf-8"
        )
        (self.prepared / "tools/cli/Cargo.toml").write_text(
            '[package]\nname = "rsvg_convert"\nversion = "2.63.2"\n', encoding="utf-8"
        )
        (self.prepared / "crates/core/src/lib.rs").write_text("pub fn core() {}\n", encoding="utf-8")
        (self.prepared / "tools/cli/src/main.rs").write_text("fn main() {}\n", encoding="utf-8")
        self.write_lock()

        expected_tree = root / "expected" / VENDOR_ROOT
        self.populate_vendor(expected_tree, "none")
        expected_archive = root / "expected-vendor.tar.xz"
        write_deterministic_vendor(expected_archive, expected_tree)
        self.expected_vendor = expected_archive.read_bytes()
        self.write_authority(digest_bytes(self.expected_vendor))

    def write_lock(self, *, local_version: str = "2.63.2", source: str = REGISTRY_SOURCE) -> None:
        lock = (
            "version = 4\n\n"
            "[[package]]\nname = \"librsvg\"\n"
            f"version = \"{local_version}\"\n\n"
            "[[package]]\nname = \"rsvg_convert\"\nversion = \"2.63.2\"\n\n"
            "[[package]]\nname = \"dep-one\"\nversion = \"1.0.0\"\n"
            f"source = \"{source}\"\nchecksum = \"{self.crate_checksum}\"\n"
        )
        (self.prepared / "Cargo.lock").write_text(lock, encoding="utf-8")

    def write_authority(self, vendor_sha256: str) -> None:
        feature_patch = self.repo / "tools/sharp-libvips-successor/v1/librsvg-features.patch"
        feature_patch.write_text("fixture\n", encoding="utf-8")
        lock = self.repo / "tools/sharp-libvips-successor/v1/librsvg-Cargo.lock"
        shutil.copy2(self.prepared / "Cargo.lock", lock)
        authority = {
            "librsvg": {
                "url": "https://download.gnome.org/librsvg.tar.xz",
                "sha256": digest_file(self.librsvg_archive),
                "root": "librsvg-2.63.2",
                "securityPatch": {
                    "url": "https://example.invalid/security.patch",
                    "sha256": digest_file(self.security_patch),
                },
                "featurePatch": {
                    "path": "tools/sharp-libvips-successor/v1/librsvg-features.patch",
                    "sha256": digest_file(feature_patch),
                },
                "registry": REGISTRY,
                "vendor": {"file": VENDOR_FILE, "sha256": vendor_sha256},
            },
            "cargoLock": {
                "path": "tools/sharp-libvips-successor/v1/librsvg-Cargo.lock",
                "sha256": digest_file(lock),
            },
        }
        (self.repo / "tools/sharp-libvips-successor/v1/source.json").write_text(
            json.dumps(authority), encoding="utf-8"
        )

    def fake_prepare(self, **arguments):
        self.prepare_calls.append(dict(arguments))
        destination = Path(arguments["output_dir"])
        shutil.copytree(self.prepared, destination)
        return destination

    def populate_vendor(self, destination: Path, mutation: str) -> None:
        destination.mkdir(parents=True, exist_ok=True)
        package = destination / "dep-one-1.0.0"
        package.mkdir(mode=0o755)
        retained: dict[str, str] = {}
        for relative, (content, mode) in self.crate_files.items():
            if Path(relative).name in {".gitignore", ".gitattributes", ".cargo-ok"}:
                continue
            target = package / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            target.chmod(mode & ~0o022)
            retained[relative] = digest_bytes(content)
        if mutation == "combined":
            changed = b"pub fn changed() {}\n"
            (package / "src/lib.rs").write_bytes(changed)
            retained["src/lib.rs"] = digest_bytes(changed)
        checksum = {
            "$comment": "generated by fixture cargo vendor",
            "files": dict(sorted(retained.items())),
            "package": self.crate_checksum,
        }
        (package / ".cargo-checksum.json").write_text(
            json.dumps(checksum, separators=(",", ":")), encoding="utf-8"
        )

    def fake_cargo(self, command, *, cwd, env, check, capture_output, text, umask):
        command = [str(value) for value in command]
        cwd = Path(cwd)
        env = dict(env)
        self.cargo_calls.append((command, cwd, env))
        self.assert_cargo_call(command, cwd, env, check, capture_output, text, umask)
        destination = Path(command[-1])
        self.populate_vendor(destination, self.vendor_mutation)
        return subprocess.CompletedProcess(command, 0, "", "")

    def assert_cargo_call(self, command, cwd, env, check, capture_output, text, umask) -> None:
        expected = [
            str(self.cargo_bin), "vendor", "--frozen", "--respect-source-config",
            "--versioned-dirs",
        ]
        if command[:-1] != expected:
            raise AssertionError(f"unexpected cargo command: {command!r}")
        if check is not False or not capture_output or not text or umask != 0o022:
            raise AssertionError("cargo invocation did not preserve the frozen isolated contract")
        cargo_home = Path(env["CARGO_HOME"])
        if cargo_home.parent != Path(env["HOME"]):
            raise AssertionError("Cargo home must be under the isolated HOME")
        if any(name.startswith("CARGO_") and name != "CARGO_HOME" for name in env):
            raise AssertionError("ambient CARGO_* variable survived")
        if (cargo_home / "registry/src").exists():
            raise AssertionError("ambient extracted sources were copied")
        if any((cargo_home / name).exists() for name in ("config", "config.toml", "credentials")):
            raise AssertionError("ambient Cargo config or credentials were copied")
        config = (cwd / ".cargo/config.toml").read_text(encoding="utf-8")
        if REGISTRY not in config or 'replace-with = "cloud-agents-registry"' not in config:
            raise AssertionError("isolated source replacement is absent")


class LibrsvgVendorTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.fixture = Fixture(self.root)
        self.module = load_module()

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def run_vendor(self, output: Path, mutation: str = "none") -> Path:
        self.fixture.vendor_mutation = mutation
        with (
            mock.patch.dict(
                os.environ,
                {"CARGO_HOME": "/ambient/cargo", "CARGO_HTTP_TOKEN": "secret"},
                clear=True,
            ),
            mock.patch.object(
                self.module._PREPARE, "prepare_librsvg_source", self.fixture.fake_prepare
            ),
            mock.patch.object(self.module._VENDOR.subprocess, "run", self.fixture.fake_cargo),
        ):
            self.module.vendor(
                librsvg_archive=self.fixture.librsvg_archive,
                librsvg_security_patch=self.fixture.security_patch,
                cargo_registry_dir=self.fixture.registry,
                output_dir=output,
                repo_root=self.fixture.repo,
                cargo_bin=self.fixture.cargo_bin,
            )
        return output

    def test_reproduces_only_bound_archive_from_prepared_workspace(self) -> None:
        first = self.run_vendor(self.root / "first")
        second = self.run_vendor(self.root / "second")
        self.assertEqual([path.name for path in first.iterdir()], [VENDOR_FILE])
        self.assertEqual((first / VENDOR_FILE).read_bytes(), self.fixture.expected_vendor)
        self.assertEqual((first / VENDOR_FILE).read_bytes(), (second / VENDOR_FILE).read_bytes())
        self.assertEqual(len(self.fixture.prepare_calls), 2)
        call = self.fixture.prepare_calls[0]
        self.assertEqual(call["librsvg_archive"], self.fixture.librsvg_archive)
        self.assertEqual(call["librsvg_security_patch"], self.fixture.security_patch)
        with tarfile.open(first / VENDOR_FILE, "r:xz") as archive:
            members = archive.getmembers()
            self.assertEqual(members[0].name, VENDOR_ROOT)
            self.assertTrue(all(member.uid == member.gid == member.mtime == 0 for member in members))
            self.assertTrue(all(not member.pax_headers for member in members))

    def test_rejects_workspace_lock_drift_and_unknown_sources_before_cargo(self) -> None:
        cases = (("workspace", "9.9.9", REGISTRY_SOURCE), ("source", "2.63.2", "git+https://example.invalid/repo"))
        for label, version, source in cases:
            with self.subTest(label=label):
                self.fixture.write_lock(local_version=version, source=source)
                self.fixture.write_authority(digest_bytes(self.fixture.expected_vendor))
                output = self.root / f"bad-{label}"
                with self.assertRaisesRegex(ValueError, "workspace|source|registry|crates.io"):
                    self.run_vendor(output)
                self.assertFalse(output.exists())
                self.assertEqual(self.fixture.cargo_calls, [])

    def test_rejects_workspace_or_manifest_path_escape_before_cargo(self) -> None:
        root_manifest = self.fixture.prepared / "Cargo.toml"
        member_manifest = self.fixture.prepared / "crates/core/Cargo.toml"
        original_root = root_manifest.read_text(encoding="utf-8")
        original_member = member_manifest.read_text(encoding="utf-8")
        cases = {
            "workspace": ('[workspace]\nmembers = ["../../escape"]\n', original_member),
            "manifest": (
                original_root,
                original_member + '\n[dependencies]\nescape = { path = "../../../outside" }\n',
            ),
        }
        for label, (root_content, member_content) in cases.items():
            with self.subTest(label=label):
                root_manifest.write_text(root_content, encoding="utf-8")
                member_manifest.write_text(member_content, encoding="utf-8")
                output = self.root / f"escape-{label}"
                with self.assertRaisesRegex(ValueError, "workspace|path|escape|unsafe"):
                    self.run_vendor(output)
                self.assertFalse(output.exists())
                self.assertEqual(self.fixture.cargo_calls, [])
        root_manifest.write_text(original_root, encoding="utf-8")
        member_manifest.write_text(original_member, encoding="utf-8")

    def test_rejects_crate_and_generated_vendor_tampering_without_output(self) -> None:
        original = self.fixture.crate.read_bytes()
        self.fixture.crate.write_bytes(original + b"tampered")
        with self.assertRaisesRegex(ValueError, "digest"):
            self.run_vendor(self.root / "crate-tamper")
        self.assertFalse((self.root / "crate-tamper").exists())
        self.assertEqual(self.fixture.cargo_calls, [])
        self.fixture.crate.write_bytes(original)

        with self.assertRaisesRegex(ValueError, "byte|checksum|vendor"):
            self.run_vendor(self.root / "vendor-tamper", "combined")
        self.assertFalse((self.root / "vendor-tamper").exists())

        self.fixture.write_authority("0" * 64)
        with self.assertRaisesRegex(ValueError, "digest"):
            self.run_vendor(self.root / "authority-drift")
        self.assertFalse((self.root / "authority-drift").exists())

    def test_never_overwrites_and_cli_requires_all_inputs(self) -> None:
        output = self.root / "existing"
        output.write_text("keep", encoding="utf-8")
        with self.assertRaisesRegex(FileExistsError, "output"):
            self.run_vendor(output)
        self.assertEqual(output.read_text(encoding="utf-8"), "keep")

        result = subprocess.run(
            ["python3", "-B", str(SCRIPT), "--output-dir", str(self.root / "cli")],
            check=False, capture_output=True, text=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--librsvg-archive", result.stderr)
        self.assertIn("--librsvg-security-patch", result.stderr)
        self.assertIn("--cargo-registry-dir", result.stderr)


if __name__ == "__main__":
    unittest.main()
