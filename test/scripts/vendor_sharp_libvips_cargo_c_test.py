#!/usr/bin/env python3
"""Focused tests for the reproducible cargo-c vendor generator."""

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
import tomllib
import unittest
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[2] / "scripts/vendor-sharp-libvips-cargo-c.py"
CARGO_C_VERSION = "0.10.10+cargo-0.86.0"
REGISTRY_SOURCE = "registry+https://github.com/rust-lang/crates.io-index"
REGISTRY = "sparse+https://rsproxy.cn/index/"
VENDOR_ROOT = "cargo-c-0.10.10-vendor"
FILTERED = {".gitignore", ".gitattributes", ".git", ".cargo-ok"}


def digest_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def digest_file(path: Path) -> str:
    return digest_bytes(path.read_bytes())


def load_module():
    spec = importlib.util.spec_from_file_location("vendor_sharp_libvips_cargo_c", SCRIPT)
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
    with tarfile.open(
        path, "w:xz", format=tarfile.PAX_FORMAT, preset=3
    ) as archive:
        paths = [tree, *sorted(tree.rglob("*"))]
        for source in paths:
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
        self.source_archive = root / f"cargo-c-{CARGO_C_VERSION}.crate"
        self.cargo_bin = root / "cargo"
        self.calls: list[tuple[list[str], Path, dict[str, str]]] = []
        self.vendor_mutation = "none"
        self.crates: dict[tuple[str, str], tuple[Path, dict[str, tuple[bytes, int]], str]] = {}

        (self.repo / "tools/sharp-libvips-successor/v1").mkdir(parents=True)
        cache = self.registry / "cache/rsproxy.cn-fixture"
        index = self.registry / "index/rsproxy.cn-fixture"
        cache.mkdir(parents=True)
        index.mkdir(parents=True)
        (index / "config.json").write_text("{}\n", encoding="utf-8")
        self.cargo_bin.write_text("#!/bin/sh\nexit 99\n", encoding="utf-8")
        self.cargo_bin.chmod(0o755)

        dependencies = {
            ("dep-one", "1.0.0"): {
                "Cargo.toml": (b'[package]\nname = "dep-one"\nversion = "1.0.0"\n', 0o644),
                "src/lib.rs": (b"pub fn one() {}\n", 0o640),
                "src.rs": (b"pub fn sibling() {}\n", 0o644),
                ".gitignore": (b"target\n", 0o644),
            },
            ("dep-two", "2.0.0"): {
                "Cargo.toml": (b'[package]\nname = "dep-two"\nversion = "2.0.0"\n', 0o644),
                "build.rs": (b"fn main() {}\n", 0o755),
                "README.md": (b"fixture\n", 0o664),
            },
        }
        lock_packages: list[dict[str, str]] = []
        for (name, version), files in dependencies.items():
            archive = cache / f"{name}-{version}.crate"
            archive.write_bytes(tar_bytes(f"{name}-{version}", files))
            checksum = digest_file(archive)
            self.crates[(name, version)] = (archive, files, checksum)
            lock_packages.append({"name": name, "version": version, "checksum": checksum})

        lock = [
            "version = 4",
            "",
            "[[package]]",
            'name = "cargo-c"',
            f'version = "{CARGO_C_VERSION}"',
        ]
        for package in lock_packages:
            lock.extend(
                [
                    "",
                    "[[package]]",
                    f'name = "{package["name"]}"',
                    f'version = "{package["version"]}"',
                    f'source = "{REGISTRY_SOURCE}"',
                    f'checksum = "{package["checksum"]}"',
                ]
            )
        self.lock = ("\n".join(lock) + "\n").encode()
        source_files = {
            "Cargo.toml": (
                f'[package]\nname = "cargo-c"\nversion = "{CARGO_C_VERSION}"\n'.encode(),
                0o644,
            ),
            "Cargo.lock": (self.lock, 0o644),
            "src/main.rs": (b"fn main() {}\n", 0o644),
        }
        self.source_archive.write_bytes(
            tar_bytes(f"cargo-c-{CARGO_C_VERSION}", source_files)
        )

        expected_tree = root / "expected" / VENDOR_ROOT
        self.populate_vendor(expected_tree, "none")
        expected_archive = root / "expected-vendor.tar.xz"
        write_deterministic_vendor(expected_archive, expected_tree)
        self.expected_vendor = expected_archive.read_bytes()
        recipe = {
            "cargoC": {
                "version": CARGO_C_VERSION,
                "source": {
                    "url": f"https://rsproxy.cn/api/v1/crates/cargo-c/{CARGO_C_VERSION}/download",
                    "file": self.source_archive.name,
                    "sha256": digest_file(self.source_archive),
                },
                "vendor": {
                    "file": "cargo-c-0.10.10-vendor.tar.xz",
                    "sha256": digest_bytes(self.expected_vendor),
                },
                "registry": REGISTRY,
            }
        }
        (self.repo / "tools/sharp-libvips-successor/v1/source.json").write_text(
            json.dumps(recipe), encoding="utf-8"
        )

    def populate_vendor(self, destination: Path, mutation: str) -> None:
        destination.mkdir(parents=True, exist_ok=True)
        for (name, version), (_, files, package_checksum) in self.crates.items():
            package = destination / f"{name}-{version}"
            package.mkdir(mode=0o755)
            retained: dict[str, str] = {}
            for relative, (content, mode) in files.items():
                if Path(relative).name in FILTERED:
                    continue
                target = package / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(content)
                target.chmod(mode & ~0o022)
                retained[relative] = digest_bytes(content)
            if mutation == "missing" and name == "dep-one":
                (package / "src/lib.rs").unlink()
                retained.pop("src/lib.rs")
            elif mutation == "extra" and name == "dep-one":
                (package / "unexpected.txt").write_text("extra", encoding="utf-8")
                retained["unexpected.txt"] = digest_bytes(b"extra")
            elif mutation == "combined" and name == "dep-one":
                changed = b"pub fn changed() {}\n"
                (package / "src/lib.rs").write_bytes(changed)
                retained["src/lib.rs"] = digest_bytes(changed)
            elif mutation == "link" and name == "dep-one":
                (package / "src/lib.rs").unlink()
                (package / "src/lib.rs").symlink_to("../Cargo.toml")
                retained["src/lib.rs"] = digest_file(package / "Cargo.toml")
            checksum = {
                "$comment": "generated by fixture cargo vendor",
                "files": dict(sorted(retained.items())),
                "package": package_checksum,
            }
            (package / ".cargo-checksum.json").write_text(
                json.dumps(checksum, separators=(",", ":")), encoding="utf-8"
            )

    def fake_cargo(self, command, *, cwd, env, check, capture_output, text, umask):
        command = [str(value) for value in command]
        cwd = Path(cwd)
        env = dict(env)
        self.calls.append((command, cwd, env))
        self.assert_cargo_call(command, cwd, env, check, capture_output, text)
        if umask != 0o022:
            raise AssertionError("cargo invocation must set child umask 0022")
        destination = Path(command[-1])
        if list(destination.iterdir()):
            raise AssertionError("vendor destination was not empty")
        self.populate_vendor(destination, self.vendor_mutation)
        return subprocess.CompletedProcess(command, 0, "", "")

    def assert_cargo_call(self, command, cwd, env, check, capture_output, text) -> None:
        expected = [
            str(self.cargo_bin),
            "vendor",
            "--frozen",
            "--respect-source-config",
            "--versioned-dirs",
        ]
        if command[:-1] != expected:
            raise AssertionError(f"unexpected cargo command: {command!r}")
        if check is not False or not capture_output or not text:
            raise AssertionError("cargo invocation must capture output and preserve exit status")
        if self.repo.resolve() in (cwd.resolve(), *cwd.resolve().parents):
            raise AssertionError("cargo cwd must not be inside the repository")
        cargo_home = Path(env["CARGO_HOME"])
        if cargo_home.parent != Path(env["HOME"]):
            raise AssertionError("Cargo home must be under the isolated HOME")
        if any(name.startswith("CARGO_") and name != "CARGO_HOME" for name in env):
            raise AssertionError("ambient CARGO_* variable survived")
        if env.get("RUSTUP_HOME") != "/fixture/rustup":
            raise AssertionError("RUSTUP_HOME was not retained")
        registry = cargo_home / "registry"
        if not (registry / "cache/rsproxy.cn-fixture").is_dir():
            raise AssertionError("registry cache was not copied")
        if not (registry / "index/rsproxy.cn-fixture").is_dir():
            raise AssertionError("registry index was not copied")
        if any((cargo_home / name).exists() for name in ("config", "config.toml", "credentials", "credentials.toml")):
            raise AssertionError("ambient Cargo config or credentials were copied")
        if (registry / "src").exists():
            raise AssertionError("ambient extracted sources were copied")
        config = (cwd / ".cargo/config.toml").read_text(encoding="utf-8")
        if REGISTRY not in config or 'replace-with = "cloud-agents-registry"' not in config:
            raise AssertionError("isolated source replacement is absent")


class CargoCVendorTests(unittest.TestCase):
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
                {
                    "CARGO_HOME": "/ambient/cargo",
                    "CARGO_NET_OFFLINE": "false",
                    "CARGO_HTTP_TOKEN": "secret",
                    "RUSTUP_HOME": "/fixture/rustup",
                },
                clear=True,
            ),
            mock.patch.object(self.module._VENDOR.subprocess, "run", self.fixture.fake_cargo),
        ):
            self.module.vendor(
                cargo_c_archive=self.fixture.source_archive,
                cargo_registry_dir=self.fixture.registry,
                output_dir=output,
                repo_root=self.fixture.repo,
                cargo_bin=self.fixture.cargo_bin,
            )
        return output

    def test_reproduces_bound_archive_and_isolates_frozen_cargo(self) -> None:
        first = self.run_vendor(self.root / "first")
        second = self.run_vendor(self.root / "second")
        source_name = self.fixture.source_archive.name
        vendor_name = "cargo-c-0.10.10-vendor.tar.xz"
        self.assertEqual((first / source_name).read_bytes(), self.fixture.source_archive.read_bytes())
        self.assertEqual((first / vendor_name).read_bytes(), self.fixture.expected_vendor)
        self.assertEqual((first / vendor_name).read_bytes(), (second / vendor_name).read_bytes())
        self.assertEqual(len(self.fixture.calls), 2)
        with tarfile.open(first / vendor_name, "r:xz") as archive:
            members = archive.getmembers()
            self.assertEqual(members[0].name, VENDOR_ROOT)
            self.assertTrue(all(member.uid == member.gid == member.mtime == 0 for member in members))
            self.assertTrue(all(not member.pax_headers for member in members))
            mode = archive.getmember(f"{VENDOR_ROOT}/dep-one-1.0.0/src/lib.rs").mode
            self.assertEqual(mode, 0o640)
            normalized = archive.getmember(f"{VENDOR_ROOT}/dep-two-2.0.0/README.md").mode
            self.assertEqual(normalized, 0o644)
            names = [member.name for member in members]
            self.assertLess(
                names.index(f"{VENDOR_ROOT}/dep-one-1.0.0/src/lib.rs"),
                names.index(f"{VENDOR_ROOT}/dep-one-1.0.0/src.rs"),
            )

    def test_rejects_tampered_or_unsafe_registry_crates_before_cargo(self) -> None:
        cache = next((self.fixture.registry / "cache").glob("*"))
        target = cache / "dep-one-1.0.0.crate"
        original = target.read_bytes()
        cases = {
            "tampered": original + b"tampered",
            "unsafe": tar_bytes("../escape", {"Cargo.toml": (b"bad", 0o644)}),
        }
        for label, content in cases.items():
            with self.subTest(label=label):
                target.write_bytes(content)
                with self.assertRaisesRegex(ValueError, "digest|unsafe|escaping|root"):
                    self.run_vendor(self.root / f"out-{label}")
                self.assertFalse((self.root / f"out-{label}").exists())
                self.assertEqual(self.fixture.calls, [])
                target.write_bytes(original)

    def test_rejects_registry_cache_or_index_links_before_cargo(self) -> None:
        link = self.fixture.registry / "index/rsproxy.cn-fixture/linked"
        link.symlink_to(self.root / "outside")
        output = self.root / "linked-registry"
        with self.assertRaisesRegex(ValueError, "symlink|special"):
            self.run_vendor(output)
        self.assertFalse(output.exists())
        self.assertEqual(self.fixture.calls, [])

    def test_rejects_vendor_missing_extra_modified_or_linked_files(self) -> None:
        for mutation in ("missing", "extra", "combined", "link"):
            with self.subTest(mutation=mutation):
                output = self.root / f"out-{mutation}"
                with self.assertRaisesRegex(ValueError, "vendor|member|byte|link|checksum"):
                    self.run_vendor(output, mutation)
                self.assertFalse(output.exists())

    def test_rejects_source_metadata_or_embedded_cargo_config(self) -> None:
        original = self.fixture.source_archive.read_bytes()
        cases = {
            "wrong-version": {
                "Cargo.toml": (b'[package]\nname="cargo-c"\nversion="9.9.9"\n', 0o644),
                "Cargo.lock": (self.fixture.lock, 0o644),
            },
            "cargo-config": {
                "Cargo.toml": (
                    f'[package]\nname="cargo-c"\nversion="{CARGO_C_VERSION}"\n'.encode(),
                    0o644,
                ),
                "Cargo.lock": (self.fixture.lock, 0o644),
                ".cargo/config.toml": (b"[source.crates-io]\n", 0o644),
            },
        }
        recipe_path = self.fixture.repo / "tools/sharp-libvips-successor/v1/source.json"
        for label, files in cases.items():
            with self.subTest(label=label):
                self.fixture.source_archive.write_bytes(
                    tar_bytes(f"cargo-c-{CARGO_C_VERSION}", files)
                )
                recipe = json.loads(recipe_path.read_text(encoding="utf-8"))
                recipe["cargoC"]["source"]["sha256"] = digest_file(self.fixture.source_archive)
                recipe_path.write_text(json.dumps(recipe), encoding="utf-8")
                output = self.root / f"source-{label}"
                with self.assertRaisesRegex(ValueError, "version|config"):
                    self.run_vendor(output)
                self.assertFalse(output.exists())
                self.fixture.source_archive.write_bytes(original)
                recipe["cargoC"]["source"]["sha256"] = digest_file(self.fixture.source_archive)
                recipe_path.write_text(json.dumps(recipe), encoding="utf-8")

    def test_never_overwrites_existing_output_path(self) -> None:
        cases = ("directory", "file", "symlink")
        for label in cases:
            with self.subTest(label=label):
                output = self.root / f"existing-{label}"
                if label == "directory":
                    output.mkdir()
                elif label == "file":
                    output.write_text("keep", encoding="utf-8")
                else:
                    output.symlink_to(self.root / "absent-target")
                before = output.lstat()
                with self.assertRaisesRegex(FileExistsError, "output"):
                    self.run_vendor(output)
                self.assertEqual(output.lstat().st_mode, before.st_mode)

    def test_cli_requires_all_offline_inputs(self) -> None:
        result = subprocess.run(
            ["python3", "-B", str(SCRIPT), "--output-dir", str(self.root / "output")],
            check=False,
            capture_output=True,
            text=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--cargo-c-archive", result.stderr)
        self.assertIn("--cargo-registry-dir", result.stderr)


if __name__ == "__main__":
    unittest.main()
