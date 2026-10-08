#!/usr/bin/env python3
"""Focused tests for the sharp/libvips source preparation boundary."""

from __future__ import annotations

import hashlib
import importlib.util
import io
import json
import os
import subprocess
import tarfile
import tempfile
import unittest
from contextlib import redirect_stderr
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[2] / "scripts/prepare-sharp-libvips-successor.py"
SPEC = importlib.util.spec_from_file_location("prepare_sharp_libvips_successor", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)

RUST_DATE = "2026-10-07"
RUST_TARGETS = {
    "linux-arm64v8": "aarch64-unknown-linux-gnu",
    "linux-x64": "x86_64-unknown-linux-gnu",
}
RUST_COMPONENTS = {
    "rustc": ("1.92.0-nightly", "a" * 40),
    "cargo": ("0.102.0-nightly (ccccccccc 2026-10-01)", "b" * 40),
    "rust-std": ("1.92.0-nightly", None),
}
CARGO_C_VERSION = "0.10.10+cargo-0.86.0"


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_tar(
    path: Path,
    root: str,
    files: dict[str, bytes],
    symlinks: dict[str, str] | None = None,
    include_root: bool = True,
) -> None:
    with tarfile.open(path, "w:xz") as archive:
        if include_root:
            directory = tarfile.TarInfo(root)
            directory.type = tarfile.DIRTYPE
            directory.mode = 0o755
            directory.mtime = 0
            archive.addfile(directory)
        for name, body in files.items():
            member = tarfile.TarInfo(f"{root}/{name}")
            member.mode = 0o644
            member.mtime = 0
            member.size = len(body)
            archive.addfile(member, io.BytesIO(body))
        for name, target in (symlinks or {}).items():
            member = tarfile.TarInfo(f"{root}/{name}")
            member.type = tarfile.SYMTYPE
            member.linkname = target
            member.mode = 0o777
            member.mtime = 0
            archive.addfile(member)


class PrepareSharpLibvipsSuccessorTest(unittest.TestCase):
    def test_artifact_basename_allows_literal_plus_and_rejects_unsafe_characters(self) -> None:
        accepted = MODULE._validate_artifact(
            {
                "url": "https://example.test/module+Tabs+Wrap-1.0-1.x86_64.rpm",
                "file": "module+Tabs+Wrap-1.0-1.x86_64.rpm",
                "sha256": "0" * 64,
            },
            "RPM fixture",
        )
        self.assertEqual(accepted["file"], "module+Tabs+Wrap-1.0-1.x86_64.rpm")
        for unsafe in (
            "nested/package.rpm",
            "package name.rpm",
            "package?.rpm",
            "package#fragment.rpm",
            ".hidden.rpm",
        ):
            with self.subTest(unsafe=unsafe), self.assertRaisesRegex(
                ValueError, "safe basename"
            ):
                MODULE._validate_artifact(
                    {
                        "url": "https://example.test/package.rpm",
                        "file": unsafe,
                        "sha256": "0" * 64,
                    },
                    "RPM fixture",
                )

    def fixture(self) -> tuple[Path, dict, Path, Path, Path, Path, Path, Path, Path, Path]:
        temporary_directory = Path(
            tempfile.mkdtemp(prefix="fixture-", dir=self.temporary_directory.name)
        )
        repo = temporary_directory / "repo"
        repo.mkdir()
        lock = repo / "tools/sharp-libvips-successor/v1/librsvg-Cargo.lock"
        patch = repo / "tools/sharp-libvips-successor/v1/patches/locked-inputs.patch"
        feature_patch = (
            repo / "tools/sharp-libvips-successor/v1/patches/librsvg-features.patch"
        )
        lock.parent.mkdir(parents=True)
        patch.parent.mkdir()
        lock.write_bytes(b"fixed cargo lock\n")
        patch.write_text(
            "--- a/recipe.txt\n"
            "+++ b/recipe.txt\n"
            "@@ -1 +1 @@\n"
            "-before\n"
            "+after\n",
            encoding="utf-8",
        )
        feature_patch.write_text(
            "--- a/rsvg/Cargo.toml\n"
            "+++ b/rsvg/Cargo.toml\n"
            "@@ -1,2 +1,2 @@\n"
            "-image = { features = [\"jpeg\", \"png\", \"gif\", \"webp\"] }\n"
            "+image = { features = [\"jpeg\", \"png\"] }\n"
            "-cairo-rs = { features = [\"png\", \"pdf\", \"ps\", \"svg\"] }\n"
            "+cairo-rs = { features = [\"png\", \"svg\"] }\n"
            "--- a/librsvg-c/Cargo.toml\n"
            "+++ b/librsvg-c/Cargo.toml\n"
            "@@ -1 +1 @@\n"
            "-cairo-rs = { features = [\"png\", \"pdf\", \"ps\", \"svg\"] }\n"
            "+cairo-rs = { features = [\"png\", \"svg\"] }\n"
            "--- a/meson.build\n"
            "+++ b/meson.build\n"
            "@@ -1 +1 @@\n"
            "-if host_system in ['windows', 'linux']\n"
            "+if host_system in ['windows']\n",
            encoding="utf-8",
        )

        sharp = temporary_directory / "sharp.tar.xz"
        librsvg = temporary_directory / "librsvg.tar.xz"
        security_patch = temporary_directory / "librsvg-security.patch"
        librsvg_vendor_dir = temporary_directory / "librsvg-vendor"
        librsvg_vendor_dir.mkdir()
        librsvg_root = "librsvg-root"
        librsvg_vendor = librsvg_vendor_dir / f"{librsvg_root}-vendor.tar.xz"
        write_tar(
            librsvg_vendor,
            f"{librsvg_root}-vendor",
            {
                "dependency-1.0.0/Cargo.toml": b"[package]\nname = \"dependency\"\n",
                "dependency-1.0.0/.cargo-checksum.json": b"{}\n",
                "dependency-1.0.0/src/lib.rs": b"pub fn dependency() {}\n",
            },
        )
        sources = temporary_directory / "sources"
        sources.mkdir()
        source_input = sources / "native-source.tar.gz"
        source_input.write_bytes(b"bound native source\n")
        wheelhouse = temporary_directory / "wheelhouse"
        wheelhouse.mkdir()
        meson_wheel = wheelhouse / "meson-1.12.1-py3-none-any.whl"
        ninja_arm64_wheel = (
            wheelhouse
            / "ninja-1.13.2-py3-none-manylinux2014_aarch64.manylinux_2_17_aarch64.whl"
        )
        ninja_x64_wheel = (
            wheelhouse
            / "ninja-1.13.2-py3-none-manylinux2014_x86_64.manylinux_2_17_x86_64.whl"
        )
        meson_wheel.write_bytes(b"meson wheel\n")
        ninja_arm64_wheel.write_bytes(b"ninja arm64 wheel\n")
        ninja_x64_wheel.write_bytes(b"ninja x64 wheel\n")
        (wheelhouse / "unused-cache.whl").write_bytes(b"ignored extra cache\n")
        rust_dist = temporary_directory / "rust-dist"
        rust_dist.mkdir()
        manifest_lines = ['manifest-version = "2"', f'date = "{RUST_DATE}"', ""]
        for component, (version, commit_hash) in RUST_COMPONENTS.items():
            manifest_lines.extend([f"[pkg.{component}]", f'version = "{version}"'])
            if commit_hash is not None:
                manifest_lines.append(f'git_commit_hash = "{commit_hash}"')
            manifest_lines.append("")
            for rust_target in RUST_TARGETS.values():
                archive_name = f"{component}-nightly-{rust_target}.tar.xz"
                archive = rust_dist / archive_name
                files = {
                    "install.sh": f"#!/bin/sh\n# {component} {rust_target}\n".encode()
                }
                if component == "cargo":
                    files["cargo/bin/cargo"] = f"cargo binary for {rust_target}\n".encode()
                write_tar(
                    archive,
                    archive_name.removesuffix(".tar.xz"),
                    files,
                )
                manifest_lines.extend(
                    [
                        f"[pkg.{component}.target.{rust_target}]",
                        "available = true",
                        f'xz_url = "https://static.rust-lang.org/dist/{RUST_DATE}/{archive_name}"',
                        f'xz_hash = "{digest(archive)}"',
                        "",
                    ]
                )
        rust_manifest = rust_dist / "channel-rust-nightly.toml"
        rust_manifest.write_text("\n".join(manifest_lines), encoding="utf-8")
        cargo_c_dir = temporary_directory / "cargo-c"
        cargo_c_dir.mkdir()
        cargo_c_source = cargo_c_dir / f"cargo-c-{CARGO_C_VERSION}.crate"
        cargo_c_source_root = f"cargo-c-{CARGO_C_VERSION}"
        write_tar(
            cargo_c_source,
            cargo_c_source_root,
            {
                "Cargo.toml": (
                    '[package]\nname = "cargo-c"\n'
                    f'version = "{CARGO_C_VERSION}"\n'
                ).encode(),
                "Cargo.lock": b"# fixed cargo-c lock\n",
                "src/main.rs": b"fn main() {}\n",
            },
            include_root=False,
        )
        cargo_c_vendor = cargo_c_dir / "cargo-c-0.10.10-vendor.tar.xz"
        write_tar(
            cargo_c_vendor,
            "cargo-c-0.10.10-vendor",
            {"dependency-1.0.0/.cargo-checksum.json": b"{}\n"},
        )
        builder_rpm_dir = temporary_directory / "builder-rpms"
        (builder_rpm_dir / "keys").mkdir(parents=True)
        rocky_key = builder_rpm_dir / "keys/RPM-GPG-KEY-Rocky"
        rocky_key.write_bytes(b"fixture Rocky signing key\n")
        rpm_files = {
            "linux-arm64v8": "fixture-devel-1.0-1.aarch64.rpm",
            "linux-x64": "fixture-devel-1.0-1.x86_64.rpm",
        }
        for target, rpm_file in rpm_files.items():
            target_dir = builder_rpm_dir / target
            target_dir.mkdir()
            (target_dir / rpm_file).write_bytes(f"fixture {target} RPM\n".encode())
        write_tar(sharp, "sharp-root", {"recipe.txt": b"before\n", "README": b"sharp\n"})
        write_tar(
            librsvg,
            librsvg_root,
            {
                "Cargo.toml": b"[workspace]\n",
                "Cargo.lock": b"upstream cargo lock\n",
                "rsvg/Cargo.toml": (
                    b'image = { features = ["jpeg", "png", "gif", "webp"] }\n'
                    b'cairo-rs = { features = ["png", "pdf", "ps", "svg"] }\n'
                ),
                "librsvg-c/Cargo.toml": (
                    b'cairo-rs = { features = ["png", "pdf", "ps", "svg"] }\n'
                ),
                "meson.build": b"if host_system in ['windows', 'linux']\n",
                "rsvg/src/security.rs": b"const IMAGE_LIMITS: bool = false;\n",
                "LICENSE": b"fixture license\n",
            },
            symlinks={"COPYING": "LICENSE"},
        )
        security_patch.write_text(
            "--- a/rsvg/src/security.rs\n"
            "+++ b/rsvg/src/security.rs\n"
            "@@ -2 +2 @@\n"
            "-const IMAGE_LIMITS: bool = false;\n"
            "+const IMAGE_LIMITS: bool = true;\n",
            encoding="utf-8",
        )
        recipe = {
            "schemaVersion": 1,
            "builderBase": {
                "repository": "quay.io/rockylinux/rockylinux",
                "indexDigest": f"sha256:{'0' * 64}",
                "platforms": {
                    "linux-arm64v8": {
                        "platform": "linux/arm64",
                        "manifestDigest": f"sha256:{'1' * 64}",
                    },
                    "linux-x64": {
                        "platform": "linux/amd64",
                        "manifestDigest": f"sha256:{'2' * 64}",
                    },
                },
            },
            "pythonTools": {
                "meson": {
                    "version": "1.12.1",
                    "wheel": {
                        "url": "https://example.test/meson-1.12.1-py3-none-any.whl",
                        "file": meson_wheel.name,
                        "sha256": digest(meson_wheel),
                    },
                },
                "ninja": {
                    "version": "1.13.2",
                    "wheels": {
                        "linux-arm64v8": {
                            "url": f"https://example.test/{ninja_arm64_wheel.name}",
                            "file": ninja_arm64_wheel.name,
                            "sha256": digest(ninja_arm64_wheel),
                        },
                        "linux-x64": {
                            "url": f"https://example.test/{ninja_x64_wheel.name}",
                            "file": ninja_x64_wheel.name,
                            "sha256": digest(ninja_x64_wheel),
                        },
                    },
                },
            },
            "rustToolchain": {
                "manifest": {
                    "url": f"https://example.test/dist/{RUST_DATE}/{rust_manifest.name}",
                    "file": rust_manifest.name,
                    "sha256": digest(rust_manifest),
                }
            },
            "cargoC": {
                "version": CARGO_C_VERSION,
                "source": {
                    "url": f"https://example.test/{cargo_c_source.name}",
                    "file": cargo_c_source.name,
                    "sha256": digest(cargo_c_source),
                },
                "vendor": {
                    "file": cargo_c_vendor.name,
                    "sha256": digest(cargo_c_vendor),
                },
                "vendorCommand": [
                    "cargo",
                    "vendor",
                    "--locked",
                    "--respect-source-config",
                    "--versioned-dirs",
                ],
            },
            "builderRpms": {
                "keys": {
                    "rocky": {
                        "url": "https://download.rockylinux.org/pub/rocky/RPM-GPG-KEY-Rocky",
                        "file": rocky_key.name,
                        "sha256": digest(rocky_key),
                    }
                },
                "platforms": {
                    target: {
                        "packages": [
                            {
                                "url": f"https://dl.rockylinux.org/{rpm_file}",
                                "file": rpm_file,
                                "sha256": digest(builder_rpm_dir / target / rpm_file),
                                "nevra": rpm_file.removesuffix(".rpm"),
                                "signingKey": "rocky",
                            }
                        ]
                    }
                    for target, rpm_file in rpm_files.items()
                },
            },
            "sharp": {
                "url": "https://example.test/sharp.tar.xz",
                "sha256": digest(sharp),
                "root": "sharp-root",
            },
            "librsvg": {
                "url": "https://example.test/librsvg.tar.xz",
                "sha256": digest(librsvg),
                "root": librsvg_root,
                "registry": "sparse+https://rsproxy.cn/index/",
                "vendor": {
                    "file": librsvg_vendor.name,
                    "sha256": digest(librsvg_vendor),
                },
                "featurePatch": {
                    "path": str(feature_patch.relative_to(repo)),
                    "sha256": digest(feature_patch),
                },
                "securityPatch": {
                    "url": "https://example.test/librsvg-security.patch",
                    "sha256": digest(security_patch),
                },
            },
            "cargoLock": {"path": str(lock.relative_to(repo)), "sha256": digest(lock)},
            "recipePatch": {"path": str(patch.relative_to(repo)), "sha256": digest(patch)},
            "buildInputs": [
                {
                    "url": "https://example.test/native-source.tar.gz",
                    "file": source_input.name,
                    "sha256": digest(source_input),
                }
            ],
        }
        source_json = repo / Path(*MODULE.SOURCE_RELATIVE_PATH.parts)
        source_json.write_bytes(
            (json.dumps(recipe, indent=2, sort_keys=True) + "\n").encode("utf-8")
        )
        return (
            repo,
            recipe,
            sharp,
            librsvg,
            security_patch,
            lock,
            sources,
            wheelhouse,
            rust_dist,
            cargo_c_dir,
        )

    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_applies_recipe_patch_and_copies_bound_inputs(self) -> None:
        repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
        output = Path(self.temporary_directory.name) / "out"

        context = MODULE.prepare(
            recipe=recipe,
            repo_root=repo,
            sharp_archive=sharp,
            librsvg_archive=librsvg,
            librsvg_security_patch=security_patch,
            librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
            builder_rpm_dir=librsvg.parent / "builder-rpms",
            sources_dir=sources,
            python_wheelhouse=wheelhouse,
            rust_dist_dir=rust_dist,
            cargo_c_dir=cargo_c_dir,
            output_dir=output,
        )

        self.assertEqual(context, output / "sharp-root")
        self.assertEqual((context / "recipe.txt").read_bytes(), b"after\n")
        librsvg_source = context / "cloud-agents/librsvg-source"
        self.assertEqual(
            (librsvg_source / "rsvg/Cargo.toml").read_bytes(),
            b'image = { features = ["jpeg", "png"] }\n'
            b'cairo-rs = { features = ["png", "svg"] }\n',
        )
        self.assertEqual(
            (librsvg_source / "librsvg-c/Cargo.toml").read_bytes(),
            b'cairo-rs = { features = ["png", "svg"] }\n',
        )
        self.assertEqual(
            (librsvg_source / "meson.build").read_bytes(),
            b"if host_system in ['windows']\n",
        )
        self.assertEqual(
            (librsvg_source / "rsvg/src/security.rs").read_bytes(),
            b"const IMAGE_LIMITS: bool = true;\n",
        )
        self.assertFalse((librsvg_source / "rsvg/src/security.rs.orig").exists())
        self.assertFalse((librsvg_source / "rsvg/src/security.rs.rej").exists())
        lock = repo / recipe["cargoLock"]["path"]
        self.assertEqual((librsvg_source / "Cargo.lock").read_bytes(), lock.read_bytes())
        self.assertTrue((librsvg_source / "COPYING").is_symlink())
        self.assertEqual(os.readlink(librsvg_source / "COPYING"), "LICENSE")
        self.assertEqual(
            (librsvg_source / "vendor/dependency-1.0.0/src/lib.rs").read_bytes(),
            b"pub fn dependency() {}\n",
        )
        self.assertEqual(
            (librsvg_source / ".cargo/config.toml").read_text(encoding="utf-8"),
            '[source.crates-io]\n'
            'replace-with = "cloud-agents-vendor"\n\n'
            '[source.cloud-agents-vendor]\n'
            'directory = "vendor"\n\n'
            '[net]\n'
            'offline = true\n',
        )
        for relative in (
            "rsvg/Cargo.toml.bak",
            "librsvg-c/Cargo.toml.bak",
            "meson.build.bak",
        ):
            self.assertFalse((librsvg_source / relative).exists())
        self.assertFalse((context / "cloud-agents/librsvg.tar.xz").exists())
        self.assertFalse((context / "cloud-agents/librsvg-security.patch").exists())
        self.assertFalse((context / "cloud-agents/librsvg-Cargo.lock").exists())
        provenance = context / "cloud-agents/source-provenance"
        source_json = repo / Path(*MODULE.SOURCE_RELATIVE_PATH.parts)
        self.assertEqual(
            (provenance / MODULE.SOURCE_RELATIVE_PATH).read_bytes(),
            source_json.read_bytes(),
        )
        for authority in (
            recipe["recipePatch"],
            recipe["librsvg"]["featurePatch"],
            recipe["cargoLock"],
        ):
            relative = Path(authority["path"])
            self.assertEqual(
                (provenance / relative).read_bytes(),
                (repo / relative).read_bytes(),
            )
        source_input = sources / recipe["buildInputs"][0]["file"]
        self.assertEqual(
            (context / "cloud-agents/sources" / source_input.name).read_bytes(),
            source_input.read_bytes(),
        )
        for target in RUST_TARGETS:
            rpm_bundle = context / "platforms" / target / "cloud-agents-rpms"
            package = recipe["builderRpms"]["platforms"][target]["packages"][0]
            key = recipe["builderRpms"]["keys"][package["signingKey"]]
            self.assertEqual(
                (rpm_bundle / "packages" / package["file"]).read_bytes(),
                (librsvg.parent / "builder-rpms" / target / package["file"]).read_bytes(),
            )
            self.assertEqual(
                (rpm_bundle / "keys" / key["file"]).read_bytes(),
                (librsvg.parent / "builder-rpms/keys" / key["file"]).read_bytes(),
            )
            self.assertEqual(
                (rpm_bundle / "manifest.tsv").read_text(encoding="utf-8"),
                "\t".join(
                    (
                        package["file"],
                        package["nevra"],
                        package["signingKey"],
                        key["file"],
                    )
                )
                + "\n",
            )
            install = (rpm_bundle / "install.sh").read_text(encoding="utf-8")
            self.assertIn("declare -A key_databases", install)
            self.assertEqual(install.count("--initdb"), 1)
            self.assertIn("rpmkeys --dbpath", install)
            self.assertIn('*Signature*": OK"*)', install)
            self.assertIn('*) printf \'%s\\n\' "RPM signature verification failed"', install)
            self.assertIn("rpm -qp --qf '%{NEVRA}\\n'", install)
            self.assertNotIn("%{NAME}.%{ARCH}", install)
            self.assertNotIn("expected_sha", install)
            self.assertIn("--disablerepo='*'", install)
            self.assertIn("--setopt=localpkg_gpgcheck=1", install)
            self.assertIn("--setopt=install_weak_deps=False", install)
            self.assertIn("rpm -q --qf", install)
        read_source = context / "cloud-agents/read-source.sh"
        result = subprocess.run(
            [
                "bash",
                "-c",
                'source "$1"; read_source "$2"',
                "read-source-test",
                str(read_source),
                recipe["buildInputs"][0]["url"],
            ],
            check=False,
            capture_output=True,
            env={**os.environ, "PACKAGE": str(context)},
        )
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, source_input.read_bytes())
        self.assertEqual(result.stderr, b"")
        self.assertIn("source prep only", MODULE.COMPLETION_NOTE.lower())

    def test_source_provenance_rejects_missing_link_invalid_or_mismatched_source_json(
        self,
    ) -> None:
        for mode in ("missing", "symlink", "invalid", "mismatch"):
            with self.subTest(mode=mode):
                (
                    repo,
                    recipe,
                    sharp,
                    librsvg,
                    security_patch,
                    _,
                    sources,
                    wheelhouse,
                    rust_dist,
                    cargo_c_dir,
                ) = self.fixture()
                source_json = repo / Path(*MODULE.SOURCE_RELATIVE_PATH.parts)
                if mode == "missing":
                    source_json.unlink()
                elif mode == "symlink":
                    body = source_json.read_bytes()
                    source_json.unlink()
                    target = source_json.with_name("source-target.json")
                    target.write_bytes(body)
                    source_json.symlink_to(target.name)
                elif mode == "invalid":
                    source_json.write_bytes(b"{invalid\n")
                else:
                    authority = json.loads(source_json.read_bytes())
                    authority["sharp"]["url"] = "https://example.test/different.tar.xz"
                    source_json.write_text(json.dumps(authority), encoding="utf-8")
                output = Path(self.temporary_directory.name) / f"source-json-{mode}"

                with self.assertRaisesRegex(
                    ValueError, "source.json|regular|match|invalid"
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_builder_rpm_directory_is_an_exact_regular_file_set(self) -> None:
        for mode in ("missing", "extra", "tampered", "symlink"):
            with self.subTest(mode=mode):
                (
                    repo,
                    recipe,
                    sharp,
                    librsvg,
                    security_patch,
                    _,
                    sources,
                    wheelhouse,
                    rust_dist,
                    cargo_c_dir,
                ) = self.fixture()
                rpm_dir = librsvg.parent / "builder-rpms"
                package = recipe["builderRpms"]["platforms"]["linux-arm64v8"]["packages"][0]
                rpm = rpm_dir / "linux-arm64v8" / package["file"]
                if mode == "missing":
                    rpm.unlink()
                elif mode == "extra":
                    (rpm.parent / "extra.rpm").write_bytes(b"extra\n")
                elif mode == "tampered":
                    rpm.write_bytes(b"tampered\n")
                else:
                    body = rpm.read_bytes()
                    rpm.unlink()
                    target = rpm.parent / "target.rpm"
                    target.write_bytes(body)
                    rpm.symlink_to(target.name)
                output = Path(self.temporary_directory.name) / f"rpm-{mode}"
                with self.assertRaisesRegex(
                    ValueError, "builder RPM|RPM|directory|regular|digest|unexpected"
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=rpm_dir,
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_builder_rpm_authority_rejects_identity_and_key_drift(self) -> None:
        for mode in (
            "unsafe NEVRA",
            "wrong arch",
            "duplicate file",
            "duplicate URL",
            "duplicate NEVRA",
            "unknown key",
            "unused key",
            "key query",
            "key basename",
            "package fragment",
            "package basename",
        ):
            with self.subTest(mode=mode):
                (
                    repo,
                    recipe,
                    sharp,
                    librsvg,
                    security_patch,
                    _,
                    sources,
                    wheelhouse,
                    rust_dist,
                    cargo_c_dir,
                ) = self.fixture()
                authority = recipe["builderRpms"]
                package = authority["platforms"]["linux-arm64v8"]["packages"][0]
                if mode == "unsafe NEVRA":
                    package["nevra"] += "\nunsafe"
                elif mode == "wrong arch":
                    package["nevra"] = package["nevra"].removesuffix("aarch64") + "x86_64"
                elif mode.startswith("duplicate"):
                    duplicate = dict(package)
                    duplicate.update(
                        file="second.aarch64.rpm",
                        url="https://dl.rockylinux.org/second.aarch64.rpm",
                        nevra="second-1.0-1.aarch64",
                    )
                    field = {
                        "duplicate file": "file",
                        "duplicate URL": "url",
                        "duplicate NEVRA": "nevra",
                    }[mode]
                    duplicate[field] = package[field]
                    authority["platforms"]["linux-arm64v8"]["packages"].append(duplicate)
                elif mode == "unknown key":
                    package["signingKey"] = "unknown"
                elif mode == "key query":
                    authority["keys"]["rocky"]["url"] += "?download=1"
                elif mode == "key basename":
                    authority["keys"]["rocky"]["url"] += ".different"
                elif mode == "package fragment":
                    package["url"] += "#download"
                elif mode == "package basename":
                    package["url"] += ".different"
                else:
                    authority["keys"]["unused"] = dict(authority["keys"]["rocky"])
                    authority["keys"]["unused"]["file"] = "unused-key"
                    authority["keys"]["unused"]["url"] += ".unused"
                output = Path(self.temporary_directory.name) / mode.replace(" ", "-")
                with self.assertRaisesRegex(
                    ValueError,
                    "builderRpms|RPM|NEVRA|duplicate|signing|unused|architecture|URL|basename|query|fragment",
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_librsvg_source_helper_rejects_existing_output(self) -> None:
        repo, recipe, _, librsvg, security_patch, _, *_ = self.fixture()
        output = Path(self.temporary_directory.name) / "existing-librsvg"
        output.mkdir()
        marker = output / "keep"
        marker.write_bytes(b"keep\n")

        with self.assertRaisesRegex(ValueError, "output already exists"):
            MODULE.prepare_librsvg_source(
                librsvg=recipe["librsvg"],
                cargo_lock=recipe["cargoLock"],
                repo_root=repo,
                librsvg_archive=librsvg,
                librsvg_security_patch=security_patch,
                output_dir=output,
            )

        self.assertEqual(marker.read_bytes(), b"keep\n")

    def test_librsvg_source_rejects_bound_input_and_context_drift_without_output(self) -> None:
        cases = ("feature-digest", "unsafe-feature-path", "context-drift", "already-applied")
        for mode in cases:
            with self.subTest(mode=mode):
                repo, recipe, _, librsvg, security_patch, _, *_ = self.fixture()
                feature = repo / recipe["librsvg"]["featurePatch"]["path"]
                if mode == "feature-digest":
                    feature.write_bytes(b"tampered\n")
                elif mode == "unsafe-feature-path":
                    recipe["librsvg"]["featurePatch"]["path"] = "../escape.patch"
                else:
                    root = "librsvg-root"
                    rsvg = (
                        b'image = { features = ["jpeg", "png"] }\n'
                        b'cairo-rs = { features = ["png", "svg"] }\n'
                        if mode == "already-applied"
                        else b'image = { features = ["jpeg", "png", "avif"] }\n'
                        b'cairo-rs = { features = ["png", "pdf", "ps", "svg"] }\n'
                    )
                    write_tar(
                        librsvg,
                        root,
                        {
                            "Cargo.toml": b"[workspace]\n",
                            "Cargo.lock": b"upstream cargo lock\n",
                            "rsvg/Cargo.toml": rsvg,
                            "librsvg-c/Cargo.toml": (
                                b'cairo-rs = { features = ["png", "pdf", "ps", "svg"] }\n'
                            ),
                            "meson.build": b"if host_system in ['windows', 'linux']\n",
                            "rsvg/src/security.rs": b"const IMAGE_LIMITS: bool = false;\n",
                        },
                    )
                    recipe["librsvg"]["sha256"] = digest(librsvg)
                output = Path(self.temporary_directory.name) / f"librsvg-{mode}"
                with self.assertRaisesRegex(ValueError, "feature patch|repository|digest"):
                    MODULE.prepare_librsvg_source(
                        librsvg=recipe["librsvg"],
                        cargo_lock=recipe["cargoLock"],
                        repo_root=repo,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_librsvg_source_rejects_unsafe_archive_and_security_failure_without_output(self) -> None:
        for mode in ("unsafe-archive", "lock-symlink", "security-failure"):
            with self.subTest(mode=mode):
                repo, recipe, _, librsvg, security_patch, _, *_ = self.fixture()
                if mode == "unsafe-archive":
                    write_tar(librsvg, "../escape", {"Cargo.toml": b"bad\n"})
                    recipe["librsvg"]["sha256"] = digest(librsvg)
                elif mode == "lock-symlink":
                    write_tar(
                        librsvg,
                        "librsvg-root",
                        {
                            "Cargo.toml": b"[workspace]\n",
                            "rsvg/Cargo.toml": (
                                b'image = { features = ["jpeg", "png", "gif", "webp"] }\n'
                                b'cairo-rs = { features = ["png", "pdf", "ps", "svg"] }\n'
                            ),
                            "librsvg-c/Cargo.toml": (
                                b'cairo-rs = { features = ["png", "pdf", "ps", "svg"] }\n'
                            ),
                            "meson.build": b"if host_system in ['windows', 'linux']\n",
                            "rsvg/src/security.rs": b"const IMAGE_LIMITS: bool = false;\n",
                        },
                        symlinks={"Cargo.lock": "Cargo.toml"},
                    )
                    recipe["librsvg"]["sha256"] = digest(librsvg)
                else:
                    security_patch.write_text(
                        "--- a/rsvg/src/security.rs\n"
                        "+++ b/rsvg/src/security.rs\n"
                        "@@ -1 +1 @@\n"
                        "-const IMAGE_LIMITS: bool = drifted;\n"
                        "+const IMAGE_LIMITS: bool = true;\n",
                        encoding="utf-8",
                    )
                    recipe["librsvg"]["securityPatch"]["sha256"] = digest(security_patch)
                output = Path(self.temporary_directory.name) / f"librsvg-{mode}"
                with self.assertRaisesRegex(
                    ValueError, "archive|security patch|unsafe|escaping|regular"
                ):
                    MODULE.prepare_librsvg_source(
                        librsvg=recipe["librsvg"],
                        cargo_lock=recipe["cargoLock"],
                        repo_root=repo,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_librsvg_vendor_rejects_invalid_bound_archive_without_output(self) -> None:
        for mode in ("missing", "tampered", "unsafe root", "wrong root", "link"):
            with self.subTest(mode=mode):
                (
                    repo,
                    recipe,
                    sharp,
                    librsvg,
                    security_patch,
                    _,
                    sources,
                    wheelhouse,
                    rust_dist,
                    cargo_c_dir,
                ) = self.fixture()
                vendor_dir = librsvg.parent / "librsvg-vendor"
                vendor = vendor_dir / recipe["librsvg"]["vendor"]["file"]
                if mode == "missing":
                    vendor.unlink()
                elif mode == "tampered":
                    vendor.write_bytes(b"tampered vendor\n")
                else:
                    root = (
                        "../escape"
                        if mode == "unsafe root"
                        else "unexpected-root"
                        if mode == "wrong root"
                        else "librsvg-root-vendor"
                    )
                    write_tar(
                        vendor,
                        root,
                        {"dependency-1.0.0/Cargo.toml": b"[package]\n"},
                        {"dependency-1.0.0/link": "Cargo.toml"}
                        if mode == "link"
                        else None,
                    )
                    recipe["librsvg"]["vendor"]["sha256"] = digest(vendor)
                output = Path(self.temporary_directory.name) / f"vendor-{mode}"
                with self.assertRaisesRegex(
                    ValueError, "vendor|digest|archive|root|link|unsafe|escaping"
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=vendor_dir,
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_librsvg_vendor_rejects_authority_drift_without_output(self) -> None:
        mutations = {
            "query": lambda librsvg: librsvg.update(
                registry="sparse+https://rsproxy.cn/index/?mirror=1"
            ),
            "fragment": lambda librsvg: librsvg.update(
                registry="sparse+https://rsproxy.cn/index/#mirror"
            ),
            "missing slash": lambda librsvg: librsvg.update(
                registry="sparse+https://rsproxy.cn/index"
            ),
            "wrong filename": lambda librsvg: librsvg["vendor"].update(
                file="different-vendor.tar.xz"
            ),
            "extra field": lambda librsvg: librsvg["vendor"].update(url="https://example.test/vendor"),
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label):
                (
                    repo,
                    recipe,
                    sharp,
                    librsvg,
                    security_patch,
                    _,
                    sources,
                    wheelhouse,
                    rust_dist,
                    cargo_c_dir,
                ) = self.fixture()
                mutate(recipe["librsvg"])
                output = Path(self.temporary_directory.name) / label.replace(" ", "-")
                with self.assertRaisesRegex(ValueError, "registry|vendor|file|field"):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_librsvg_vendor_rejects_source_conflicts_without_output(self) -> None:
        for conflict in ("vendor/file", ".cargo/config", ".cargo/config.toml"):
            with self.subTest(conflict=conflict):
                (
                    repo,
                    recipe,
                    sharp,
                    librsvg,
                    security_patch,
                    _,
                    sources,
                    wheelhouse,
                    rust_dist,
                    cargo_c_dir,
                ) = self.fixture()
                write_tar(
                    librsvg,
                    "librsvg-root",
                    {
                        "Cargo.toml": b"[workspace]\n",
                        "Cargo.lock": b"upstream cargo lock\n",
                        "rsvg/Cargo.toml": (
                            b'image = { features = ["jpeg", "png", "gif", "webp"] }\n'
                            b'cairo-rs = { features = ["png", "pdf", "ps", "svg"] }\n'
                        ),
                        "librsvg-c/Cargo.toml": (
                            b'cairo-rs = { features = ["png", "pdf", "ps", "svg"] }\n'
                        ),
                        "meson.build": b"if host_system in ['windows', 'linux']\n",
                        "rsvg/src/security.rs": b"const IMAGE_LIMITS: bool = false;\n",
                        conflict: b"must not be overwritten\n",
                    },
                )
                recipe["librsvg"]["sha256"] = digest(librsvg)
                output = Path(self.temporary_directory.name) / conflict.replace("/", "-")
                with self.assertRaisesRegex(ValueError, "vendor|Cargo configuration|exists"):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_generates_exact_builder_base_selection(self) -> None:
        repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
        output = Path(self.temporary_directory.name) / "out"
        context = MODULE.prepare(
            recipe=recipe,
            repo_root=repo,
            sharp_archive=sharp,
            librsvg_archive=librsvg,
            librsvg_security_patch=security_patch,
            librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
            builder_rpm_dir=librsvg.parent / "builder-rpms",
            sources_dir=sources,
            python_wheelhouse=wheelhouse,
            rust_dist_dir=rust_dist,
            cargo_c_dir=cargo_c_dir,
            output_dir=output,
        )
        selector = context / "cloud-agents/builder-base.sh"

        for target, expected in (
            (
                "linux-arm64v8",
                (
                    "linux/arm64",
                    f"quay.io/rockylinux/rockylinux@sha256:{'1' * 64}",
                ),
            ),
            (
                "linux-x64",
                (
                    "linux/amd64",
                    f"quay.io/rockylinux/rockylinux@sha256:{'2' * 64}",
                ),
            ),
        ):
            with self.subTest(target=target):
                result = subprocess.run(
                    [
                        "bash",
                        "-c",
                        'source "$1"; printf \'%s\\n%s\\n\' '
                        '"$CLOUD_AGENTS_BUILDER_PLATFORM" "$CLOUD_AGENTS_BUILDER_BASE_IMAGE"',
                        "builder-base-test",
                        str(selector),
                    ],
                    check=False,
                    capture_output=True,
                    text=True,
                    env={
                        **os.environ,
                        "PLATFORM": target,
                        "CLOUD_AGENTS_BUILDER_PLATFORM": "floating-platform",
                        "CLOUD_AGENTS_BUILDER_BASE_IMAGE": "floating-image:latest",
                    },
                )
                self.assertEqual(result.returncode, 0)
                self.assertEqual(result.stdout.splitlines(), list(expected))
                self.assertEqual(result.stderr, "")

        unknown = subprocess.run(
            ["bash", "-c", 'source "$1"', "builder-base-test", str(selector)],
            check=False,
            capture_output=True,
            text=True,
            env={
                **os.environ,
                "PLATFORM": "linux-unknown",
                "CLOUD_AGENTS_BUILDER_PLATFORM": "floating-platform",
                "CLOUD_AGENTS_BUILDER_BASE_IMAGE": "floating-image:latest",
            },
        )
        self.assertNotEqual(unknown.returncode, 0)
        self.assertEqual(unknown.stdout, "")

    def test_copies_platform_python_tools_and_generates_hashed_requirements(self) -> None:
        repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
        output = Path(self.temporary_directory.name) / "out"
        context = MODULE.prepare(
            recipe=recipe,
            repo_root=repo,
            sharp_archive=sharp,
            librsvg_archive=librsvg,
            librsvg_security_patch=security_patch,
            librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
            builder_rpm_dir=librsvg.parent / "builder-rpms",
            sources_dir=sources,
            python_wheelhouse=wheelhouse,
            rust_dist_dir=rust_dist,
            cargo_c_dir=cargo_c_dir,
            output_dir=output,
        )
        meson = recipe["pythonTools"]["meson"]
        ninja = recipe["pythonTools"]["ninja"]

        for target in ("linux-arm64v8", "linux-x64"):
            with self.subTest(target=target):
                target_directory = (
                    context / "platforms" / target / "cloud-agents-python-tools"
                )
                ninja_wheel = ninja["wheels"][target]
                expected_names = {
                    meson["wheel"]["file"],
                    ninja_wheel["file"],
                    "requirements.txt",
                }
                self.assertEqual(
                    {path.name for path in target_directory.iterdir()}, expected_names
                )
                for wheel in (meson["wheel"], ninja_wheel):
                    self.assertEqual(
                        (target_directory / wheel["file"]).read_bytes(),
                        (wheelhouse / wheel["file"]).read_bytes(),
                    )
                self.assertEqual(
                    (target_directory / "requirements.txt").read_text(encoding="utf-8"),
                    f"meson=={meson['version']} --hash=sha256:{meson['wheel']['sha256']}\n"
                    f"ninja=={ninja['version']} --hash=sha256:{ninja_wheel['sha256']}\n",
                )

    def test_derives_platform_rust_archives_and_offline_install_script(self) -> None:
        repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = (
            self.fixture()
        )
        output = Path(self.temporary_directory.name) / "out"
        context = MODULE.prepare(
            recipe=recipe,
            repo_root=repo,
            sharp_archive=sharp,
            librsvg_archive=librsvg,
            librsvg_security_patch=security_patch,
            librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
            builder_rpm_dir=librsvg.parent / "builder-rpms",
            sources_dir=sources,
            python_wheelhouse=wheelhouse,
            rust_dist_dir=rust_dist,
            cargo_c_dir=cargo_c_dir,
            output_dir=output,
        )
        manifest = recipe["rustToolchain"]["manifest"]

        for target, rust_target in RUST_TARGETS.items():
            with self.subTest(target=target):
                rust_directory = context / "platforms" / target / "cloud-agents-rust"
                archives = [
                    f"{component}-nightly-{rust_target}.tar.xz"
                    for component in RUST_COMPONENTS
                ]
                self.assertEqual(
                    {path.name for path in rust_directory.iterdir()},
                    {manifest["file"], *archives, "SHA256SUMS", "install.sh"},
                )
                for filename in (manifest["file"], *archives):
                    self.assertEqual(
                        (rust_directory / filename).read_bytes(),
                        (rust_dist / filename).read_bytes(),
                    )
                expected_sums = [f"{manifest['sha256']}  {manifest['file']}"]
                expected_sums.extend(
                    f"{digest(rust_dist / filename)}  {filename}" for filename in archives
                )
                self.assertEqual(
                    (rust_directory / "SHA256SUMS").read_text(encoding="utf-8"),
                    "\n".join(expected_sums) + "\n",
                )
                install = (rust_directory / "install.sh").read_text(encoding="utf-8")
                self.assertIn("set -eu", install)
                self.assertIn("sha256sum -c SHA256SUMS", install)
                for filename in archives:
                    root = filename.removesuffix(".tar.xz")
                    self.assertIn(f"tar -xJf {filename}", install)
                    self.assertIn(
                        f"./{root}/install.sh --prefix=/usr/local --disable-ldconfig",
                        install,
                    )
                self.assertIn("/usr/local/bin/rustc -vV", install)
                self.assertIn("/usr/local/bin/cargo -vV", install)
                self.assertIn(RUST_COMPONENTS["rustc"][1], install)
                cargo_root = f"cargo-nightly-{rust_target}"
                self.assertIn(
                    f"cmp -- ./{cargo_root}/cargo/bin/cargo /usr/local/bin/cargo",
                    install,
                )
                self.assertNotIn(RUST_COMPONENTS["cargo"][1], install)
                self.assertNotIn("rustup", install)
                self.assertNotIn("curl", install)

    def test_copies_cargo_c_inputs_and_generates_offline_install_script(self) -> None:
        (
            repo,
            recipe,
            sharp,
            librsvg,
            security_patch,
            _,
            sources,
            wheelhouse,
            rust_dist,
            cargo_c_dir,
        ) = self.fixture()
        output = Path(self.temporary_directory.name) / "out"
        context = MODULE.prepare(
            recipe=recipe,
            repo_root=repo,
            sharp_archive=sharp,
            librsvg_archive=librsvg,
            librsvg_security_patch=security_patch,
            librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
            builder_rpm_dir=librsvg.parent / "builder-rpms",
            sources_dir=sources,
            python_wheelhouse=wheelhouse,
            rust_dist_dir=rust_dist,
            cargo_c_dir=cargo_c_dir,
            output_dir=output,
        )
        cargo_c = recipe["cargoC"]

        for target in RUST_TARGETS:
            with self.subTest(target=target):
                directory = context / "platforms" / target / "cloud-agents-cargo-c"
                self.assertEqual(
                    {path.name for path in directory.iterdir()},
                    {
                        cargo_c["source"]["file"],
                        cargo_c["vendor"]["file"],
                        "SHA256SUMS",
                        "install.sh",
                    },
                )
                for key in ("source", "vendor"):
                    artifact = cargo_c[key]
                    self.assertEqual(
                        (directory / artifact["file"]).read_bytes(),
                        (cargo_c_dir / artifact["file"]).read_bytes(),
                    )
                expected_sums = "".join(
                    f"{cargo_c[key]['sha256']}  {cargo_c[key]['file']}\n"
                    for key in ("source", "vendor")
                )
                self.assertEqual(
                    (directory / "SHA256SUMS").read_text(encoding="utf-8"),
                    expected_sums,
                )
                install = (directory / "install.sh").read_text(encoding="utf-8")
                checksum = install.index("sha256sum -c SHA256SUMS")
                extract = install.index(f"tar -xf {cargo_c['source']['file']}")
                consume = install.index(
                    "/usr/local/bin/cargo install --path . --locked --offline "
                    "--root /usr/local/cargo"
                )
                self.assertLess(checksum, extract)
                self.assertLess(extract, consume)
                self.assertIn('[source.crates-io]', install)
                self.assertIn('replace-with = "vendored-sources"', install)
                self.assertIn('[source.vendored-sources]', install)
                self.assertIn("cargo-c-0.10.10-vendor", install)
                self.assertEqual(install.count("Cargo.lock"), 2)
                self.assertNotIn("--bin", install)
                self.assertNotIn("--no-default-features", install)

    def test_invalid_cargo_c_archives_do_not_create_output(self) -> None:
        for mode in ("missing", "tampered", "symlink", "unsafe", "wrong root"):
            with self.subTest(mode=mode):
                (
                    repo,
                    recipe,
                    sharp,
                    librsvg,
                    security_patch,
                    _,
                    sources,
                    wheelhouse,
                    rust_dist,
                    cargo_c_dir,
                ) = self.fixture()
                authority = recipe["cargoC"]["vendor" if mode in ("tampered", "unsafe") else "source"]
                archive = cargo_c_dir / authority["file"]
                if mode == "missing":
                    archive.unlink()
                elif mode == "tampered":
                    archive.write_bytes(b"tampered cargo-c archive\n")
                elif mode == "symlink":
                    original = archive.read_bytes()
                    archive.unlink()
                    target = cargo_c_dir / "cargo-c-target.crate"
                    target.write_bytes(original)
                    archive.symlink_to(target)
                elif mode == "unsafe":
                    with tarfile.open(archive, "w:xz") as unsafe:
                        member = tarfile.TarInfo("../escape")
                        member.size = 1
                        unsafe.addfile(member, io.BytesIO(b"x"))
                    authority["sha256"] = digest(archive)
                else:
                    write_tar(
                        archive,
                        "wrong-root",
                        {
                            "Cargo.toml": b'[package]\nname="cargo-c"\nversion="0.1.0"\n',
                            "Cargo.lock": b"lock\n",
                        },
                    )
                    authority["sha256"] = digest(archive)
                output = Path(self.temporary_directory.name) / f"cargo-c-{mode.replace(' ', '-')}"
                with self.assertRaisesRegex(
                    ValueError, "cargo-c|cargoC|archive|digest|regular|unsafe|root"
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_cargo_c_metadata_and_lock_must_match_authority(self) -> None:
        for mode in ("name", "version", "missing lock", "symlink lock"):
            with self.subTest(mode=mode):
                (
                    repo,
                    recipe,
                    sharp,
                    librsvg,
                    security_patch,
                    _,
                    sources,
                    wheelhouse,
                    rust_dist,
                    cargo_c_dir,
                ) = self.fixture()
                source = recipe["cargoC"]["source"]
                archive = cargo_c_dir / source["file"]
                root = source["file"].removesuffix(".crate")
                name = "not-cargo-c" if mode == "name" else "cargo-c"
                version = "0.10.11+cargo-0.86.0" if mode == "version" else CARGO_C_VERSION
                files = {
                    "Cargo.toml": (
                        f'[package]\nname = "{name}"\nversion = "{version}"\n'
                    ).encode()
                }
                symlinks = None
                if mode != "missing lock":
                    if mode == "symlink lock":
                        symlinks = {"Cargo.lock": "Cargo.toml"}
                    else:
                        files["Cargo.lock"] = b"# fixed cargo-c lock\n"
                write_tar(archive, root, files, symlinks)
                source["sha256"] = digest(archive)
                output = Path(self.temporary_directory.name) / f"cargo-c-{mode.replace(' ', '-')}"
                with self.assertRaisesRegex(ValueError, "cargo-c|Cargo.toml|Cargo.lock|regular"):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_cli_requires_builder_rpm_directory(self) -> None:
        arguments = [
            "--sharp-archive",
            "sharp",
            "--librsvg-archive",
            "librsvg",
            "--librsvg-security-patch",
            "patch",
            "--librsvg-vendor-dir",
            "librsvg-vendor",
            "--sources-dir",
            "sources",
            "--python-wheelhouse",
            "wheels",
            "--rust-dist-dir",
            "rust",
            "--cargo-c-dir",
            "cargo-c",
            "--output-dir",
            "out",
        ]
        stderr = io.StringIO()
        with redirect_stderr(stderr), self.assertRaises(SystemExit) as raised:
            MODULE.main(arguments)
        self.assertEqual(raised.exception.code, 2)
        self.assertIn("--builder-rpm-dir", stderr.getvalue())

    def test_missing_or_invalid_rust_archive_does_not_create_output(self) -> None:
        for mode in (
            "missing",
            "tampered",
            "symlink",
            "wrong root",
            "missing cargo binary",
            "symlink cargo binary",
        ):
            with self.subTest(mode=mode):
                repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = (
                    self.fixture()
                )
                filename = "cargo-nightly-aarch64-unknown-linux-gnu.tar.xz"
                archive = rust_dist / filename
                if mode == "missing":
                    archive.unlink()
                elif mode == "tampered":
                    archive.write_bytes(b"tampered archive\n")
                elif mode == "symlink":
                    original = archive.read_bytes()
                    archive.unlink()
                    target = rust_dist / "rust-archive-target.tar.xz"
                    target.write_bytes(original)
                    archive.symlink_to(target)
                elif mode in ("wrong root", "missing cargo binary", "symlink cargo binary"):
                    old_digest = digest(archive)
                    root = (
                        "wrong-root"
                        if mode == "wrong root"
                        else filename.removesuffix(".tar.xz")
                    )
                    files = {"install.sh": b"#!/bin/sh\n"}
                    symlinks = None
                    if mode == "wrong root":
                        files["cargo/bin/cargo"] = b"cargo binary\n"
                    elif mode == "symlink cargo binary":
                        symlinks = {"cargo/bin/cargo": "../../install.sh"}
                    write_tar(archive, root, files, symlinks)
                    manifest = rust_dist / recipe["rustToolchain"]["manifest"]["file"]
                    manifest.write_text(
                        manifest.read_text(encoding="utf-8").replace(
                            f'xz_hash = "{old_digest}"',
                            f'xz_hash = "{digest(archive)}"',
                            1,
                        ),
                        encoding="utf-8",
                    )
                    recipe["rustToolchain"]["manifest"]["sha256"] = digest(manifest)
                else:
                    raise AssertionError(f"unhandled mode: {mode}")
                output = Path(self.temporary_directory.name) / f"rust-{mode.replace(' ', '-')}"
                with self.assertRaisesRegex(
                    ValueError, "Rust|rust|archive|digest|regular|root"
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_rust_manifest_rejects_digest_and_semantic_drift(self) -> None:
        for mode in (
            "digest",
            "manifest filename",
            "manifest version",
            "invalid date",
            "URL date",
            "stable channel",
            "missing target",
            "unavailable",
            "archive filename",
        ):
            with self.subTest(mode=mode):
                repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = (
                    self.fixture()
                )
                authority = recipe["rustToolchain"]["manifest"]
                manifest = rust_dist / authority["file"]
                text = manifest.read_text(encoding="utf-8")
                if mode == "digest":
                    manifest.write_text(text + "# digest drift\n", encoding="utf-8")
                elif mode == "manifest filename":
                    authority["file"] = "channel-rust-floating.toml"
                elif mode == "manifest version":
                    manifest.write_text(
                        text.replace('manifest-version = "2"', 'manifest-version = "1"', 1),
                        encoding="utf-8",
                    )
                    authority["sha256"] = digest(manifest)
                elif mode == "invalid date":
                    manifest.write_text(
                        text.replace(f'date = "{RUST_DATE}"', 'date = "nightly"', 1),
                        encoding="utf-8",
                    )
                    authority["sha256"] = digest(manifest)
                elif mode == "URL date":
                    authority["url"] = authority["url"].replace(RUST_DATE, "2026-10-06")
                elif mode == "stable channel":
                    manifest.write_text(
                        text.replace('version = "1.92.0-nightly"', 'version = "1.92.0"', 1),
                        encoding="utf-8",
                    )
                    authority["sha256"] = digest(manifest)
                elif mode == "missing target":
                    manifest.write_text(
                        text.replace(
                            "[pkg.cargo.target.aarch64-unknown-linux-gnu]",
                            "[pkg.cargo.target.armv7-unknown-linux-gnu]",
                            1,
                        ),
                        encoding="utf-8",
                    )
                    authority["sha256"] = digest(manifest)
                elif mode == "unavailable":
                    manifest.write_text(
                        text.replace("available = true", "available = false", 1),
                        encoding="utf-8",
                    )
                    authority["sha256"] = digest(manifest)
                else:
                    manifest.write_text(
                        text.replace(
                            "rustc-nightly-aarch64-unknown-linux-gnu.tar.xz",
                            "rustc-nightly-armv7-unknown-linux-gnu.tar.xz",
                            1,
                        ),
                        encoding="utf-8",
                    )
                    authority["sha256"] = digest(manifest)
                output = Path(self.temporary_directory.name) / f"manifest-{mode.replace(' ', '-')}"
                with self.assertRaisesRegex(
                    ValueError, "Rust|rust|manifest|date|nightly|target|available|file|digest"
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_missing_or_tampered_python_wheel_does_not_create_output(self) -> None:
        for mode in ("missing", "tampered", "symlink"):
            with self.subTest(mode=mode):
                repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = (
                    self.fixture()
                )
                wheel = wheelhouse / (
                    recipe["pythonTools"]["meson"]["wheel"]["file"]
                    if mode == "missing"
                    else recipe["pythonTools"]["ninja"]["wheels"]["linux-x64"]["file"]
                )
                if mode == "missing":
                    wheel.unlink()
                elif mode == "tampered":
                    wheel.write_bytes(b"tampered wheel\n")
                else:
                    original = wheel.read_bytes()
                    wheel.unlink()
                    target = wheelhouse / "wheel-target.whl"
                    target.write_bytes(original)
                    wheel.symlink_to(target)
                output = Path(self.temporary_directory.name) / f"wheel-{mode}"
                with self.assertRaisesRegex(ValueError, "wheel|Python"):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_python_tool_authority_rejects_invalid_versions_and_architectures(self) -> None:
        mutations = {
            "missing tool": lambda tools: tools.pop("ninja"),
            "extra tool": lambda tools: tools.update({"other": {}}),
            "invalid version": lambda tools: tools["meson"].update(version="1.12"),
            "version mismatch": lambda tools: tools["meson"].update(version="1.12.2"),
            "missing target": lambda tools: tools["ninja"]["wheels"].pop("linux-x64"),
            "wrong architecture": lambda tools: tools["ninja"]["wheels"][
                "linux-arm64v8"
            ].update(
                file="ninja-1.13.2-py3-none-manylinux2014_x86_64."
                "manylinux_2_17_x86_64.whl"
            ),
            "duplicate URL": lambda tools: tools["ninja"]["wheels"]["linux-x64"].update(
                url=tools["meson"]["wheel"]["url"]
            ),
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label):
                repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = (
                    self.fixture()
                )
                mutate(recipe["pythonTools"])
                output = Path(self.temporary_directory.name) / label.replace(" ", "-")
                with self.assertRaisesRegex(
                    ValueError, "pythonTools|version|wheel|target|duplicate"
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_builder_base_authority_rejects_floating_and_invalid_mappings(self) -> None:
        mutations = {
            "tagged repository": lambda builder: builder.update(
                repository="quay.io/rockylinux/rockylinux:9"
            ),
            "floating digest": lambda builder: builder.update(indexDigest="latest"),
            "missing platform": lambda builder: builder["platforms"].pop("linux-x64"),
            "duplicate platform": lambda builder: builder["platforms"]["linux-x64"].update(
                platform="linux/arm64"
            ),
            "wrong platform": lambda builder: builder["platforms"]["linux-arm64v8"].update(
                platform="linux/ppc64le"
            ),
            "extra platform": lambda builder: builder["platforms"].update(
                {
                    "linux-ppc64le": {
                        "platform": "linux/ppc64le",
                        "manifestDigest": f"sha256:{'3' * 64}",
                    }
                }
            ),
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label):
                repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
                mutate(recipe["builderBase"])
                output = Path(self.temporary_directory.name) / label.replace(" ", "-")
                with self.assertRaisesRegex(
                    ValueError, "builderBase|repository|digest|platform"
                ):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_tampered_source_digest_does_not_create_output(self) -> None:
        repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
        security_patch.write_bytes(b"tampered\n")
        output = Path(self.temporary_directory.name) / "out"

        with self.assertRaisesRegex(ValueError, "digest"):
            MODULE.prepare(
                recipe=recipe,
                repo_root=repo,
                sharp_archive=sharp,
                librsvg_archive=librsvg,
                librsvg_security_patch=security_patch,
                librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                builder_rpm_dir=librsvg.parent / "builder-rpms",
                sources_dir=sources,
                python_wheelhouse=wheelhouse,
                rust_dist_dir=rust_dist,
                cargo_c_dir=cargo_c_dir,
                output_dir=output,
            )
        self.assertFalse(output.exists())

    def test_already_applied_recipe_patch_is_rejected_without_output(self) -> None:
        repo, recipe, _, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
        sharp = Path(self.temporary_directory.name) / "already-patched.tar.xz"
        write_tar(sharp, "sharp-root", {"recipe.txt": b"after\n", "README": b"sharp\n"})
        recipe["sharp"]["sha256"] = digest(sharp)
        output = Path(self.temporary_directory.name) / "out"

        with self.assertRaisesRegex(ValueError, "recipe patch failed"):
            MODULE.prepare(
                recipe=recipe,
                repo_root=repo,
                sharp_archive=sharp,
                librsvg_archive=librsvg,
                librsvg_security_patch=security_patch,
                librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                builder_rpm_dir=librsvg.parent / "builder-rpms",
                sources_dir=sources,
                python_wheelhouse=wheelhouse,
                rust_dist_dir=rust_dist,
                cargo_c_dir=cargo_c_dir,
                output_dir=output,
            )
        self.assertFalse(output.exists())

    def test_existing_output_is_rejected_without_touching_it(self) -> None:
        repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
        output = Path(self.temporary_directory.name) / "out"
        output.mkdir()
        sentinel = output / "sentinel"
        sentinel.write_bytes(b"keep")

        with self.assertRaisesRegex(ValueError, "output"):
            MODULE.prepare(
                recipe=recipe,
                repo_root=repo,
                sharp_archive=sharp,
                librsvg_archive=librsvg,
                librsvg_security_patch=security_patch,
                librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                builder_rpm_dir=librsvg.parent / "builder-rpms",
                sources_dir=sources,
                python_wheelhouse=wheelhouse,
                rust_dist_dir=rust_dist,
                cargo_c_dir=cargo_c_dir,
                output_dir=output,
            )
        self.assertEqual(sentinel.read_bytes(), b"keep")

    def test_tampered_repo_lock_or_recipe_patch_is_rejected(self) -> None:
        for tampered_key in ("cargoLock", "recipePatch"):
            with self.subTest(tampered_key=tampered_key):
                repo, recipe, sharp, librsvg, security_patch, lock, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
                target = lock if tampered_key == "cargoLock" else repo / recipe["recipePatch"]["path"]
                target.write_bytes(target.read_bytes() + b"tampered\n")
                output = Path(self.temporary_directory.name) / tampered_key

                with self.assertRaisesRegex(ValueError, "digest"):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_read_source_rejects_unknown_missing_and_tampered_inputs_without_bytes(self) -> None:
        repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
        output = Path(self.temporary_directory.name) / "out"
        context = MODULE.prepare(
            recipe=recipe,
            repo_root=repo,
            sharp_archive=sharp,
            librsvg_archive=librsvg,
            librsvg_security_patch=security_patch,
            librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
            builder_rpm_dir=librsvg.parent / "builder-rpms",
            sources_dir=sources,
            python_wheelhouse=wheelhouse,
            rust_dist_dir=rust_dist,
            cargo_c_dir=cargo_c_dir,
            output_dir=output,
        )
        read_source = context / "cloud-agents/read-source.sh"
        copied_source = context / "cloud-agents/sources" / recipe["buildInputs"][0]["file"]

        def run(*urls: str) -> subprocess.CompletedProcess[bytes]:
            return subprocess.run(
                [
                    "bash",
                    "-c",
                    'source "$1"; shift; read_source "$@"',
                    "read-source-test",
                    str(read_source),
                    *urls,
                ],
                check=False,
                capture_output=True,
                env={**os.environ, "PACKAGE": str(context)},
            )

        for wrong_arguments in ((), (recipe["buildInputs"][0]["url"], "extra")):
            with self.subTest(wrong_arguments=wrong_arguments):
                result = run(*wrong_arguments)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, b"")

        unknown = run("https://example.test/unknown.tar.gz")
        self.assertNotEqual(unknown.returncode, 0)
        self.assertEqual(unknown.stdout, b"")

        copied_source.unlink()
        missing = run(recipe["buildInputs"][0]["url"])
        self.assertNotEqual(missing.returncode, 0)
        self.assertEqual(missing.stdout, b"")

        copied_source.write_bytes(b"tampered native source\n")
        tampered = run(recipe["buildInputs"][0]["url"])
        self.assertNotEqual(tampered.returncode, 0)
        self.assertEqual(tampered.stdout, b"")

        copied_source.unlink()
        target = copied_source.with_name("target.tar.gz")
        target.write_bytes(b"bound native source\n")
        copied_source.symlink_to(target)
        symlink = run(recipe["buildInputs"][0]["url"])
        self.assertNotEqual(symlink.returncode, 0)
        self.assertEqual(symlink.stdout, b"")

    def test_missing_or_tampered_build_input_does_not_create_output(self) -> None:
        for mode in ("missing", "tampered", "symlink"):
            with self.subTest(mode=mode):
                repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
                source_input = sources / recipe["buildInputs"][0]["file"]
                if mode == "missing":
                    source_input.unlink()
                elif mode == "tampered":
                    source_input.write_bytes(b"tampered\n")
                else:
                    source_input.unlink()
                    target = sources / "target.tar.gz"
                    target.write_bytes(b"bound native source\n")
                    source_input.symlink_to(target)
                output = Path(self.temporary_directory.name) / f"out-{mode}"
                with self.assertRaisesRegex(ValueError, "build input"):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())

    def test_build_input_authority_rejects_empty_duplicate_and_unsafe_records(self) -> None:
        mutations = {
            "empty": [],
            "duplicate URL": [
                {
                    "url": "https://example.test/native-source.tar.gz",
                    "file": "second-source.tar.gz",
                    "sha256": "0" * 64,
                }
            ],
            "duplicate file": [
                {
                    "url": "https://example.test/second-source.tar.gz",
                    "file": "native-source.tar.gz",
                    "sha256": "0" * 64,
                }
            ],
            "unsafe path": [
                {
                    "url": "https://example.test/unsafe.tar.gz",
                    "file": "nested/source.tar.gz",
                    "sha256": "0" * 64,
                }
            ],
            "insecure URL": [
                {
                    "url": "http://example.test/insecure.tar.gz",
                    "file": "insecure.tar.gz",
                    "sha256": "0" * 64,
                }
            ],
            "credential URL": [
                {
                    "url": "https://user:secret@example.test/credential.tar.gz",
                    "file": "credential.tar.gz",
                    "sha256": "0" * 64,
                }
            ],
        }
        for label, extra in mutations.items():
            with self.subTest(label=label):
                repo, recipe, sharp, librsvg, security_patch, _, sources, wheelhouse, rust_dist, cargo_c_dir = self.fixture()
                recipe["buildInputs"] = (
                    [] if label == "empty" else [*recipe["buildInputs"], *extra]
                )
                output = Path(self.temporary_directory.name) / label.replace(" ", "-")
                with self.assertRaisesRegex(ValueError, "buildInputs|duplicate|file"):
                    MODULE.prepare(
                        recipe=recipe,
                        repo_root=repo,
                        sharp_archive=sharp,
                        librsvg_archive=librsvg,
                        librsvg_security_patch=security_patch,
                        librsvg_vendor_dir=librsvg.parent / "librsvg-vendor",
                        builder_rpm_dir=librsvg.parent / "builder-rpms",
                        sources_dir=sources,
                        python_wheelhouse=wheelhouse,
                        rust_dist_dir=rust_dist,
                        cargo_c_dir=cargo_c_dir,
                        output_dir=output,
                    )
                self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
